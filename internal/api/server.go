package api

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
	"github.com/Ogstra/ogs-swg/internal/sys"
	"github.com/alitto/pond"
	"github.com/dgraph-io/ristretto"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
)

type Server struct {
	store            *core.Store
	config           *core.Config
	executor         core.SystemExecutor
	sampler          *core.StatsSampler
	pool             *pond.WorkerPool
	validate         *validator.Validate
	cache            *ristretto.Cache
	wgPendingRestart bool
	wgQRCache        map[string]qrEntry
	wgQRCacheMutex   sync.RWMutex
	wgMux            sync.RWMutex
	wgSamplerTicker  *time.Ticker
	wgSamplerStop    chan struct{}
	wgSamplerPaused  bool
	wgLast           map[string]core.WGSample
}

func NewServer(store *core.Store, config *core.Config, executor core.SystemExecutor) *Server {
	interval := 60 * time.Second
	if config.WGSamplerIntervalSec > 0 {
		interval = time.Duration(config.WGSamplerIntervalSec) * time.Second
	}
	cache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: 1e6,     // number of keys to track frequency of (1M).
		MaxCost:     1 << 25, // maximum cost of cache (32MB).
		BufferItems: 64,      // number of keys per Get buffer.
	})
	if err != nil {
		log.Fatalf("Failed to initialize ristretto cache: %v", err)
	}

	return &Server{
		store:            store,
		config:           config,
		executor:         executor,
		sampler:          nil,
		pool:             pond.New(100, 1000, pond.IdleTimeout(30*time.Second)),
		validate:         validator.New(),
		cache:            cache,
		wgPendingRestart: false,
		wgQRCache:        make(map[string]qrEntry),
		wgQRCacheMutex:   sync.RWMutex{},
		wgSamplerStop:    make(chan struct{}),
		wgSamplerTicker:  time.NewTicker(interval),
		wgMux:            sync.RWMutex{},
		wgLast:           make(map[string]core.WGSample),
		wgSamplerPaused:  false,
	}
}

type gzipResponseWriter struct {
	io.Writer
	http.ResponseWriter
}

func (w gzipResponseWriter) Write(b []byte) (int, error) {
	return w.Writer.Write(b)
}

func (s *Server) PondMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		done := make(chan struct{})
		s.pool.Submit(func() {
			defer close(done)
			next.ServeHTTP(w, r)
		})
		<-done
	})
}

func (s *Server) GzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		defer gz.Close()
		gzw := &gzipResponseWriter{Writer: gz, ResponseWriter: w}
		next.ServeHTTP(gzw, r)
	})
}

func (s *Server) secure(handler http.HandlerFunc) http.HandlerFunc {
	if s.config.APIKey == "" {
		return handler
	}

	return func(w http.ResponseWriter, r *http.Request) {
		// If authenticated via JWT (AuthMiddleware), allow
		if r.Context().Value(userContextKey) != nil {
			handler(w, r)
			return
		}

		// Otherwise, enforce API Key
		if r.Header.Get("X-API-Key") != s.config.APIKey {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		handler(w, r)
	}
}

// getPermissions extracts PanelUserPermissions from the request context.
// Returns nil if not present (e.g. API-key-only auth, which should not happen with the new middleware).
func getPermissions(r *http.Request) *core.PanelUserPermissions {
	p, _ := r.Context().Value(permissionsContextKey).(*core.PanelUserPermissions)
	return p
}

// requirePerm wraps a handler and returns 403 if the caller lacks the required permission.
func (s *Server) requirePerm(check func(*core.PanelUserPermissions) bool, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := getPermissions(r)
		// nil permissions means API-key auth — grant full access for backward compatibility
		if p != nil && !check(p) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		h(w, r)
	}
}

func (s *Server) requireSingbox(w http.ResponseWriter) bool {
	if !s.config.EnableSingbox {
		http.Error(w, "sing-box disabled", http.StatusServiceUnavailable)
		return false
	}
	return true
}

func (s *Server) requireWireGuard(w http.ResponseWriter) bool {
	if !s.config.EnableWireGuard {
		http.Error(w, "WireGuard disabled", http.StatusServiceUnavailable)
		return false
	}
	return true
}

func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	// Public Login
	mux.HandleFunc("POST /api/login", s.handleLogin)

	// Permission helpers (shorthand)
	canReadUsers := func(p *core.PanelUserPermissions) bool { return p.CanReadUsers }
	canWriteUsers := func(p *core.PanelUserPermissions) bool { return p.CanWriteUsers }
	canReadWG := func(p *core.PanelUserPermissions) bool { return p.CanReadWireguard }
	canWriteWG := func(p *core.PanelUserPermissions) bool { return p.CanWriteWireguard }
	canReadConfig := func(p *core.PanelUserPermissions) bool { return p.CanReadConfig }
	canWriteConfig := func(p *core.PanelUserPermissions) bool { return p.CanWriteConfig }
	canReadSettings := func(p *core.PanelUserPermissions) bool { return p.CanReadSettings }
	canWriteSettings := func(p *core.PanelUserPermissions) bool { return p.CanWriteSettings }
	canReadPanelUsers := func(p *core.PanelUserPermissions) bool { return p.CanReadPanelUsers }
	canWritePanelUsers := func(p *core.PanelUserPermissions) bool { return p.CanWritePanelUsers }
	canReadLogs := func(p *core.PanelUserPermissions) bool { return p.CanReadLogs }

	protected := http.NewServeMux()

	// Auth: any authenticated user can change their own password/username
	protected.HandleFunc("PUT /api/auth/password", s.secure(s.handleUpdatePassword))
	protected.HandleFunc("PUT /api/auth/username", s.secure(s.requirePerm(canWriteSettings, s.handleUpdateUsername)))

	// VPN Users
	protected.HandleFunc("GET /api/users", s.secure(s.requirePerm(canReadUsers, s.handleGetUsers)))
	protected.HandleFunc("POST /api/users", s.secure(s.requirePerm(canWriteUsers, s.handleCreateUser)))
	protected.HandleFunc("PUT /api/users", s.secure(s.requirePerm(canWriteUsers, s.handleUpdateUser)))
	protected.HandleFunc("DELETE /api/users", s.secure(s.requirePerm(canWriteUsers, s.handleDeleteUser)))
	protected.HandleFunc("GET /api/users/{name}/inbounds", s.secure(s.requirePerm(canReadUsers, s.handleGetUserInbounds)))
	protected.HandleFunc("GET /api/users/{name}/vless", s.secure(s.requirePerm(canWriteUsers, s.handleGetUserVLESSLink)))
	protected.HandleFunc("GET /api/users/{name}/link", s.secure(s.requirePerm(canWriteUsers, s.handleGetUserLink)))
	protected.HandleFunc("DELETE /api/users/{name}/inbounds/{tag}", s.secure(s.requirePerm(canWriteUsers, s.handleRemoveUserFromInbound)))
	protected.HandleFunc("PUT /api/users/{name}/inbounds/{tag}", s.secure(s.requirePerm(canWriteUsers, s.handleUpdateUserInInbound)))
	protected.HandleFunc("POST /api/users/bulk", s.secure(s.requirePerm(canWriteUsers, s.handleBulkCreateUsers)))

	// Reports/logs
	protected.HandleFunc("GET /api/report", s.secure(s.requirePerm(canReadUsers, s.handleGetReport)))
	protected.HandleFunc("GET /api/report/summary", s.secure(s.requirePerm(canReadUsers, s.handleGetReportSummary)))
	protected.HandleFunc("GET /api/logs", s.secure(s.requirePerm(canReadLogs, s.handleGetLogs)))
	protected.HandleFunc("GET /api/logs/search", s.secure(s.requirePerm(canReadLogs, s.handleSearchLogs)))
	protected.HandleFunc("GET /api/dashboard", s.secure(s.handleGetDashboardData))
	protected.HandleFunc("GET /api/stats", s.secure(s.handleGetStats))
	protected.HandleFunc("GET /api/status", s.secure(s.handleGetSystemStatus))
	// WireGuard
	protected.HandleFunc("GET /api/wireguard/peers", s.secure(s.requirePerm(canReadWG, s.handleGetWireGuardPeers)))
	protected.HandleFunc("POST /api/wireguard/peers", s.secure(s.requirePerm(canWriteWG, s.handleCreateWireGuardPeer)))
	protected.HandleFunc("DELETE /api/wireguard/peers", s.secure(s.requirePerm(canWriteWG, s.handleDeleteWireGuardPeer)))
	protected.HandleFunc("POST /api/wireguard/peers/restore", s.secure(s.requirePerm(canWriteWG, s.handleRestoreWireGuardPeer)))
	protected.HandleFunc("GET /api/wireguard/interface", s.secure(s.requirePerm(canReadWG, s.handleGetWireGuardInterface)))
	protected.HandleFunc("PUT /api/wireguard/interface", s.secure(s.requirePerm(canWriteWG, s.handleUpdateWireGuardInterface)))
	protected.HandleFunc("PUT /api/wireguard/peer", s.secure(s.requirePerm(canWriteWG, s.handleUpdateWireGuardPeer)))
	protected.HandleFunc("GET /api/wireguard/peer/config", s.secure(s.requirePerm(canReadWG, s.handleGetWireGuardPeerConfig)))
	protected.HandleFunc("GET /api/wireguard/config", s.secure(s.requirePerm(canReadWG, s.handleGetWireGuardConfig)))
	protected.HandleFunc("PUT /api/wireguard/config", s.secure(s.requirePerm(canWriteWG, s.handleUpdateWireGuardConfig)))
	protected.HandleFunc("POST /api/wireguard/config/backup", s.secure(s.requirePerm(canWriteWG, s.handleBackupWireGuardConfig)))
	protected.HandleFunc("POST /api/wireguard/config/restore", s.secure(s.requirePerm(canWriteWG, s.handleRestoreWireGuardConfig)))
	protected.HandleFunc("GET /api/wireguard/traffic", s.secure(s.requirePerm(canReadWG, s.handleGetWireGuardTraffic)))
	protected.HandleFunc("GET /api/wireguard/traffic/series", s.secure(s.requirePerm(canReadWG, s.handleGetWireGuardTrafficSeries)))
	protected.HandleFunc("GET /api/wireguard/interfaces", s.secure(s.requirePerm(canReadWG, s.handleListWireGuardInterfaces)))
	protected.HandleFunc("POST /api/wireguard/interfaces", s.secure(s.requirePerm(canWriteWG, s.handleCreateWireGuardInterface)))
	protected.HandleFunc("GET /api/wireguard/interfaces/status", s.secure(s.requirePerm(canReadWG, s.handleListWireGuardInterfacesStatus)))
	protected.HandleFunc("GET /api/wireguard/interfaces/{iface}/peers", s.secure(s.requirePerm(canReadWG, s.handleGetWireGuardPeersForInterface)))
	protected.HandleFunc("POST /api/wireguard/interfaces/{iface}/peers", s.secure(s.requirePerm(canWriteWG, s.handleCreateWireGuardPeerForInterface)))
	protected.HandleFunc("DELETE /api/wireguard/interfaces/{iface}/peers", s.secure(s.requirePerm(canWriteWG, s.handleDeleteWireGuardPeerForInterface)))
	protected.HandleFunc("POST /api/wireguard/interfaces/{iface}/peers/restore", s.secure(s.requirePerm(canWriteWG, s.handleRestoreWireGuardPeerForInterface)))
	protected.HandleFunc("GET /api/wireguard/interfaces/{iface}/interface", s.secure(s.requirePerm(canReadWG, s.handleGetWireGuardInterfaceForInterface)))
	protected.HandleFunc("PUT /api/wireguard/interfaces/{iface}/interface", s.secure(s.requirePerm(canWriteWG, s.handleUpdateWireGuardInterfaceForInterface)))
	protected.HandleFunc("PUT /api/wireguard/interfaces/{iface}/peer", s.secure(s.requirePerm(canWriteWG, s.handleUpdateWireGuardPeerForInterface)))
	protected.HandleFunc("GET /api/wireguard/interfaces/{iface}/peer/config", s.secure(s.requirePerm(canReadWG, s.handleGetWireGuardPeerConfigForInterface)))
	protected.HandleFunc("GET /api/wireguard/interfaces/{iface}/config", s.secure(s.requirePerm(canReadWG, s.handleGetWireGuardConfigForInterface)))
	protected.HandleFunc("PUT /api/wireguard/interfaces/{iface}/config", s.secure(s.requirePerm(canWriteWG, s.handleUpdateWireGuardConfigForInterface)))
	protected.HandleFunc("GET /api/wireguard/interfaces/{iface}/traffic", s.secure(s.requirePerm(canReadWG, s.handleGetWireGuardTrafficForInterface)))
	protected.HandleFunc("GET /api/wireguard/interfaces/{iface}/traffic/series", s.secure(s.requirePerm(canReadWG, s.handleGetWireGuardTrafficSeriesForInterface)))
	protected.HandleFunc("POST /api/wireguard/interfaces/{iface}/enable", s.secure(s.requirePerm(canWriteWG, s.handleEnableWireGuardInterface)))
	protected.HandleFunc("POST /api/wireguard/interfaces/{iface}/disable", s.secure(s.requirePerm(canWriteWG, s.handleDisableWireGuardInterface)))
	protected.HandleFunc("DELETE /api/wireguard/interfaces/{iface}", s.secure(s.requirePerm(canWriteWG, s.handleDeleteWireGuardInterface)))

	// Config / Sing-box / service control
	protected.HandleFunc("GET /api/config", s.secure(s.requirePerm(canReadConfig, s.handleGetConfig)))
	protected.HandleFunc("PUT /api/config", s.secure(s.requirePerm(canWriteConfig, s.handleUpdateConfig)))
	protected.HandleFunc("GET /api/singbox/config", s.secure(s.requirePerm(canReadConfig, s.handleGetSingboxConfig)))
	protected.HandleFunc("PUT /api/singbox/config", s.secure(s.requirePerm(canWriteConfig, s.handleUpdateSingboxConfig)))
	protected.HandleFunc("GET /api/singbox/inbounds", s.secure(s.requirePerm(canReadConfig, s.handleGetSingboxInbounds)))
	protected.HandleFunc("POST /api/singbox/inbound", s.secure(s.requirePerm(canWriteConfig, s.handleAddSingboxInbound)))
	protected.HandleFunc("PUT /api/singbox/inbound", s.secure(s.requirePerm(canWriteConfig, s.handleUpdateSingboxInbound)))
	protected.HandleFunc("DELETE /api/singbox/inbound", s.secure(s.requirePerm(canWriteConfig, s.handleDeleteSingboxInbound)))
	protected.HandleFunc("POST /api/singbox/apply", s.secure(s.requirePerm(canWriteConfig, s.handleApplySingboxChanges)))
	protected.HandleFunc("POST /api/service/restart", s.secure(s.requirePerm(canWriteConfig, s.handleRestartService)))
	protected.HandleFunc("POST /api/service/start", s.secure(s.requirePerm(canWriteConfig, s.handleStartService)))
	protected.HandleFunc("POST /api/service/stop", s.secure(s.requirePerm(canWriteConfig, s.handleStopService)))
	protected.HandleFunc("POST /api/config/backup", s.secure(s.requirePerm(canWriteConfig, s.handleBackupConfig)))
	protected.HandleFunc("POST /api/config/restore", s.secure(s.requirePerm(canWriteConfig, s.handleRestoreConfig)))
	protected.HandleFunc("GET /api/config/backup/meta", s.secure(s.requirePerm(canReadConfig, s.handleGetBackupMeta)))
	protected.HandleFunc("GET /api/tools/reality-keys", s.secure(s.requirePerm(canWriteConfig, s.handleGenerateRealityKeys)))
	protected.HandleFunc("POST /api/tools/self-signed-cert", s.secure(s.requirePerm(canWriteConfig, s.handleGenerateSelfSignedCert)))
	protected.HandleFunc("GET /api/sysctl", s.secure(s.requirePerm(canReadConfig, s.handleGetSysctl)))
	protected.HandleFunc("POST /api/sysctl", s.secure(s.requirePerm(canWriteConfig, s.handleApplySysctl)))

	// Settings
	protected.HandleFunc("GET /api/settings/features", s.secure(s.requirePerm(canReadSettings, s.handleGetFeatures)))
	protected.HandleFunc("PUT /api/settings/features", s.secure(s.requirePerm(canWriteSettings, s.handleUpdateFeatures)))
	protected.HandleFunc("GET /api/settings/public-ip", s.secure(s.requirePerm(canReadSettings, s.handleGetPublicIP)))
	protected.HandleFunc("PUT /api/settings/public-ip", s.secure(s.requirePerm(canWriteSettings, s.handleUpdatePublicIP)))
	protected.HandleFunc("POST /api/sampler/run", s.secure(s.requirePerm(canWriteSettings, s.handleRunSampler)))
	protected.HandleFunc("GET /api/sampler/history", s.secure(s.requirePerm(canReadSettings, s.handleSamplerHistory)))
	protected.HandleFunc("POST /api/sampler/pause", s.secure(s.requirePerm(canWriteSettings, s.handlePauseSampler)))
	protected.HandleFunc("POST /api/sampler/resume", s.secure(s.requirePerm(canWriteSettings, s.handleResumeSampler)))
	protected.HandleFunc("POST /api/retention/prune", s.secure(s.requirePerm(canWriteSettings, s.handlePruneNow)))

	// Panel user management
	protected.HandleFunc("GET /api/panel-users", s.secure(s.requirePerm(canReadPanelUsers, s.handleGetPanelUsers)))
	protected.HandleFunc("POST /api/panel-users", s.secure(s.requirePerm(canWritePanelUsers, s.handleCreatePanelUser)))
	protected.HandleFunc("PUT /api/panel-users/permissions", s.secure(s.requirePerm(canWritePanelUsers, s.handleUpdatePanelUserPermissions)))
	protected.HandleFunc("PUT /api/panel-users/username", s.secure(s.requirePerm(canWritePanelUsers, s.handleUpdatePanelUserUsername)))
	protected.HandleFunc("PUT /api/panel-users/password", s.secure(s.requirePerm(canWritePanelUsers, s.handleUpdatePanelUserPassword)))
	protected.HandleFunc("DELETE /api/panel-users", s.secure(s.requirePerm(canWritePanelUsers, s.handleDeletePanelUser)))

	// Mount protected routes under /api/
	mux.Handle("/api/", s.AuthMiddleware(s.PondMiddleware(s.GzipMiddleware(protected))))

	return mux
}

// Stop gracefully stops the server and cleans up resources
func (s *Server) Stop() {
	// Stop sing-box sampler if running
	if s.sampler != nil {
		s.sampler.Stop()
	}

	// Stop WireGuard sampler
	s.wgMux.Lock()
	if s.wgSamplerTicker != nil {
		s.wgSamplerTicker.Stop()
	}
	s.wgMux.Unlock()

	// Safely close channel (prevent double close panic)
	select {
	case <-s.wgSamplerStop:
		// Already closed
	default:
		close(s.wgSamplerStop)
	}

	if s.executor != nil {
		s.executor.Close()
	}

	if s.pool != nil {
		s.pool.StopAndWait()
	}

	// Close store connection
	if s.store != nil {
		s.store.Close()
	}
}

func StartServer(cfg *core.Config) *Server {
	cfg.ApplyWireGuardTestModeDefaults()
	cfg.LogSource = detectLogSource(cfg)

	store, err := core.NewStore(cfg.DatabasePath)
	if err != nil {
		panic("StartServer: failed to open database: " + err.Error())
	}

	if err := store.EnsureDefaultPanelUser(); err != nil {
		panic("StartServer: failed to ensure default panel user: " + err.Error())
	}

	var executor core.SystemExecutor
	if cfg.ExecutionMode == "docker_local" {
		log.Printf("Initializing Docker Local Executor (host D-Bus mode)")
		executor = sys.NewDockerLocalExecutor(cfg)
	} else {
		localOpts := []sys.LocalExecutorOption{
			sys.WithWireGuardConfigDir(cfg.WireGuardConfigDir),
			sys.WithWireGuardTestMode(cfg.WireGuardTestMode),
		}
		if cfg.WireGuardTestMode {
			if err := os.MkdirAll(cfg.WireGuardConfigDir, 0755); err != nil {
				panic("StartServer: failed to create WireGuard test dir: " + err.Error())
			}
			log.Printf("Initializing Local Executor (WireGuard test mode enabled, dir=%s)", cfg.WireGuardConfigDir)
		} else {
			log.Printf("Initializing Local Executor")
		}
		executor = sys.NewLocalExecutor(localOpts...)
	}

	// 2a. Set executor on config so it can read remote files if needed
	cfg.SetExecutor(executor)

	// 2b. Sync inbounds from Sing-box config (now uses executor)
	if err := cfg.SyncInboundsFromSingbox(); err != nil {
		// Log but don't fail, as it might be a temporary config issue
		log.Printf("StartServer: warning: failed to sync inbounds: %v", err)
	}

	server := NewServer(store, cfg, executor)

	if cfg.EnableSingbox {
		sbClient := core.NewSingboxClient(cfg.SingboxAPIAddr, executor)
		if cfg.UseStatsSampler {
			sampler := core.NewStatsSampler(sbClient, store, cfg)
			sampler.Start()
			server.sampler = sampler
		} else {
			watcher := core.NewWatcher(cfg.AccessLogPath)
			watcher.Start()
			inboundTags := cfg.StatsInbounds
			if len(inboundTags) == 0 {
				inboundTags = cfg.ManagedInbounds
			}
			calc := core.NewCalculator(watcher, sbClient, store, inboundTags)
			calc.Start()
		}
	} else {
		log.Printf("sing-box disabled via config; skipping watcher/sampler")
	}

	if cfg.EnableWireGuard {
		server.startWireGuardSampler()
	}

	// Start background maintenance (Retention & Vacuum)
	go func() {
		// Run initial check after 1 minute, then daily
		time.Sleep(1 * time.Minute)
		maintenance := func() {
			vacuumNeeded := false

			// Main Stats Retention
			if cfg.RetentionEnabled && cfg.RetentionDays > 0 {
				cutoff := time.Now().Add(-time.Duration(cfg.RetentionDays) * 24 * time.Hour).Unix()
				err := store.PruneOlderThan(cutoff)
				if err != nil {
					log.Printf("Retention prune error: %v", err)
				} else {
					log.Printf("Retention prune: removed samples older than %d", cutoff)
					vacuumNeeded = true
				}
			}

			// WireGuard Stats Retention
			if cfg.WGRetentionDays > 0 {
				cutoff := time.Now().Add(-time.Duration(cfg.WGRetentionDays) * 24 * time.Hour).Unix()
				err := store.PruneWGSamplesOlderThan(cutoff)
				if err != nil {
					log.Printf("WG retention prune error: %v", err)
				} else {
					log.Printf("WG retention prune: removed samples older than %d", cutoff)
					vacuumNeeded = true
				}
			}

			// Aggregation / Rollup
			if cfg.AggregationEnabled && cfg.AggregationDays > 0 {
				aggCutoff := time.Now().Add(-time.Duration(cfg.AggregationDays) * 24 * time.Hour).Unix()
				err := store.CompressOldSamples(aggCutoff)
				if err != nil {
					log.Printf("Aggregation compression error: %v", err)
				} else {
					log.Printf("Aggregation: compressed samples older than %d", aggCutoff)
					vacuumNeeded = true
				}

				err = store.CompressOldWGSamples(aggCutoff)
				if err != nil {
					log.Printf("WG Aggregation compression error: %v", err)
				} else {
					log.Printf("WG Aggregation: compressed samples older than %d", aggCutoff)
					vacuumNeeded = true
				}
			}

			if vacuumNeeded {
				if err := store.Vacuum(); err != nil {
					log.Printf("DB Maintenance: Vacuum failed: %v", err)
				} else {
					log.Printf("DB Maintenance: Vacuum completed")
				}
			}
		}

		// Run once on startup (after delay)
		maintenance()

		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			maintenance()
		}
	}()

	router := server.Routes()

	// Prefer built frontend assets; fallback to legacy path if needed.
	var distDir string
	candidates := []string{
		"./frontend/dist",
		"./frontend",
	}
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(exeDir, "frontend", "dist"),
			filepath.Join(exeDir, "frontend"),
		)
	}
	for _, candidate := range candidates {
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			distDir = candidate
			break
		}
	}
	if distDir == "" {
		log.Printf("Frontend directory not found. Checked: %v", candidates)
	} else {
		log.Printf("Serving static files from %s", distDir)
	}

	fs := http.FileServer(http.Dir(distDir))
	router.Handle("/assets/", http.StripPrefix("/", fs))
	router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		if distDir == "" {
			http.Error(w, "frontend assets not found", http.StatusInternalServerError)
			return
		}
		relPath := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if relPath != "" && relPath != "." {
			fullPath := filepath.Join(distDir, filepath.FromSlash(relPath))
			if st, err := os.Stat(fullPath); err == nil && !st.IsDir() {
				http.ServeFile(w, r, fullPath)
				return
			}
		}
		http.ServeFile(w, r, filepath.Join(distDir, "index.html"))
	})

	// Start server in goroutine so we can return the server instance
	go func() {
		if err := http.ListenAndServe(cfg.ListenAddr, router); err != nil {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	return server
}

type UserStatus struct {
	Name          string   `json:"name"`
	UUID          string   `json:"uuid"`
	Flow          string   `json:"flow"`
	VmessSecurity string   `json:"vmess_security,omitempty"`
	VmessAlterID  int      `json:"vmess_alter_id,omitempty"`
	Uplink        int64    `json:"uplink"`
	Downlink      int64    `json:"downlink"`
	Total         int64    `json:"total"`
	QuotaLimit    int64    `json:"quota_limit"`
	QuotaPeriod   string   `json:"quota_period"`
	ResetDay      int      `json:"reset_day"`
	Enabled       bool     `json:"enabled"`
	LastSeen      int64    `json:"last_seen"`
	InboundTags   []string `json:"inbound_tags"`
}

func (s *Server) handleGetUsers(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}
	// 1. Load active users from Singbox Config
	activeUsers, err := s.config.GetActiveUsers()
	if err != nil {
		http.Error(w, "Failed to load users: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 2. Load all metadata (includes disabled users)
	allMeta, err := s.store.GetAllUserMetadata()
	if err != nil {
		http.Error(w, "Failed to load metadata: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 3. Map for quick lookup
	activeMap := make(map[string]core.UserAccount)
	for _, u := range activeUsers {
		activeMap[u.Name] = u
	}

	metaMap := make(map[string]core.UserMetadata)
	for _, m := range allMeta {
		metaMap[m.Email] = m
	}

	// 4. Merge unique names
	uniqueNames := make(map[string]bool)
	for name := range activeMap {
		uniqueNames[name] = true
	}
	for name := range metaMap {
		uniqueNames[name] = true
	}

	result := []UserStatus{}
	for name := range uniqueNames {
		user, isActive := activeMap[name]
		meta, hasMeta := metaMap[name]

		if !isActive && !hasMeta {
			continue
		}

		// Defaults
		var uuid, flow string
		var vmessSecurity string
		var vmessAlterID int
		var inboundTags []string
		var limit int64
		var period string = "monthly"
		var enabled bool = true

		if isActive {
			uuid = user.UUID
			flow = user.Flow
			vmessSecurity = user.VmessSecurity
			vmessAlterID = user.VmessAlterID
			inboundTags = user.InboundTags
		}

		// If the user is not active but has stored inbound tags in metadata, use them.
		// This ensures disabled users display their real inbound_tags instead of nil/"all".
		if !isActive && hasMeta && len(meta.InboundTags) > 0 {
			inboundTags = meta.InboundTags
		}

		if hasMeta {
			limit = meta.QuotaLimit
			period = meta.QuotaPeriod
			enabled = meta.Enabled
			if vmessSecurity == "" && meta.VmessSecurity != "" {
				vmessSecurity = meta.VmessSecurity
			}
			if vmessAlterID == 0 && meta.VmessAlterID != 0 {
				vmessAlterID = meta.VmessAlterID
			}

			// If active, we trust config for existence.
			// But if config says active, and meta says disabled,
			// it means we haven't applied the disable yet?
			// Actually, GetActiveUsers returns users present in config.
			// If a user is present in config, they are effectively enabled in Singbox.
			// But our metadata says they should be disabled.
			// This mismatch (drift) is possible.
			// We should report what's in metadata for "Enabled" status, unless they are not in config at all (then disabled).

			// Refinement:
			// If in config -> Enabled=true (technically).
			// If NOT in config -> Enabled=? (could be disabled, or just deleted)
			// We want to show the "Target" state from metadata usually.

			// Let's stick to: Enabled flag comes from Metadata if available.
			// If no metadata, default is true (since they are in config).
		} else {
			// user in config but no meta => assumed enabled
			enabled = true
		}

		// If strictly disabled in metadata, but active in config, we still show as enabled=false (logic in handleUpdateUser handles sync)
		// But in UI we verify state.

		// Stats calculation for user table indicators:
		// always show current calendar month usage.
		now := time.Now()
		startOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		samples, err := s.store.GetSamples(name, startOfMonth.Unix(), now.Unix())
		var up, down int64
		lastSeen := int64(0)
		if err == nil {
			for _, smp := range samples {
				up += smp.Uplink
				down += smp.Downlink
				if (smp.Uplink+smp.Downlink) >= s.config.ActiveThresholdBytes && smp.Timestamp > lastSeen {
					lastSeen = smp.Timestamp
				}
			}
		}

		if lastSeen == 0 {
			if ts, err := s.store.GetLastSeenWithThreshold(name, s.config.ActiveThresholdBytes); err == nil && ts > 0 {
				lastSeen = ts
			}
		}

		result = append(result, UserStatus{
			Name:          name,
			UUID:          uuid,
			Flow:          flow,
			VmessSecurity: vmessSecurity,
			VmessAlterID:  vmessAlterID,
			Uplink:        up,
			Downlink:      down,
			Total:         up + down,
			QuotaLimit:    limit,
			QuotaPeriod:   period,
			ResetDay:      1,
			Enabled:       enabled,
			LastSeen:      lastSeen,
			InboundTags:   inboundTags,
		})
	}

	if shouldRedactUsersReadOnly(r) {
		for i := range result {
			if strings.TrimSpace(result[i].UUID) != "" {
				result[i].UUID = maskedValue
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

type CreateUserRequest struct {
	Name          string `json:"name"`
	OriginalName  string `json:"original_name,omitempty"`
	UUID          string `json:"uuid"`
	Flow          string `json:"flow"`
	VmessSecurity string `json:"vmess_security,omitempty"`
	VmessAlterID  int    `json:"vmess_alter_id,omitempty"`
	QuotaLimit    int64  `json:"quota_limit"`
	QuotaPeriod   string `json:"quota_period"`
	ResetDay      int    `json:"reset_day"`
	Enabled       *bool  `json:"enabled,omitempty"`
	InboundTag    string `json:"inbound_tag,omitempty"`
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}
	var req CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}
	if req.InboundTag == "" {
		http.Error(w, "Inbound Tag is required", http.StatusBadRequest)
		return
	}
	if req.UUID == "" {
		req.UUID = uuid.NewString()
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	if enabled {
		if err := s.config.AddUser(req.Name, req.UUID, req.Flow, req.InboundTag, req.VmessSecurity, req.VmessAlterID); err != nil {
			if errors.Is(err, os.ErrInvalid) {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			http.Error(w, "Failed to add user to config: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	meta := core.UserMetadata{
		Email:         req.Name,
		QuotaLimit:    req.QuotaLimit,
		QuotaPeriod:   req.QuotaPeriod,
		ResetDay:      1,
		Enabled:       enabled,
		VmessSecurity: req.VmessSecurity,
		VmessAlterID:  req.VmessAlterID,
	}
	if err := s.store.SaveUserMetadata(meta); err != nil {
		http.Error(w, "Failed to save metadata: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.cache.Del("api:status")
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}
	var req CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}

	originalName := req.OriginalName
	if originalName == "" {
		originalName = req.Name
	}

	// Load existing metadata upfront — needed for both enable and disable paths.
	existingMeta, _ := s.store.GetUserMetadata(originalName)

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	} else {
		if existingMeta != nil {
			enabled = existingMeta.Enabled
		}
	}

	if req.UUID == "" {
		req.UUID = uuid.NewString()
	}

	// inboundTags tracks which inbound tags to persist in metadata.
	var inboundTags []string
	if existingMeta != nil {
		inboundTags = existingMeta.InboundTags
	}

	if enabled {
		nameChanged := originalName != req.Name
		if nameChanged {
			if err := s.config.RenameUser(originalName, req.Name, req.UUID, req.Flow, req.VmessSecurity, req.VmessAlterID); err != nil {
				http.Error(w, "Failed to rename user in config: "+err.Error(), http.StatusInternalServerError)
				return
			}
		} else {
			// Re-enable path: restore user in all previously known inbounds.
			// Determine the list of inbounds to restore.
			tagsToRestore := inboundTags
			if len(tagsToRestore) == 0 && req.InboundTag != "" {
				tagsToRestore = []string{req.InboundTag}
			}

			if len(tagsToRestore) > 0 {
				for _, tag := range tagsToRestore {
					// Try updating in-place first (user already present in inbound).
					if err := s.config.UpdateUserInInbound(req.Name, req.UUID, req.Flow, tag, req.VmessSecurity, req.VmessAlterID); err != nil {
						// User not in this inbound yet — add them.
						if addErr := s.config.AddUser(req.Name, req.UUID, req.Flow, tag, req.VmessSecurity, req.VmessAlterID); addErr != nil {
							// Ignore "already exists" — propagate other errors.
							if !strings.Contains(addErr.Error(), "already exists") {
								http.Error(w, "Failed to restore user in inbound "+tag+": "+addErr.Error(), http.StatusInternalServerError)
								return
							}
						}
					}
				}
			} else {
				// No known inbounds: fall back to UpdateUser across all managed inbounds.
				if err := s.config.UpdateUser(req.Name, req.UUID, req.Flow, req.InboundTag, req.VmessSecurity, req.VmessAlterID); err != nil {
					if err := s.config.AddUser(req.Name, req.UUID, req.Flow, req.InboundTag, req.VmessSecurity, req.VmessAlterID); err != nil {
						http.Error(w, "Failed to update user in config: "+err.Error(), http.StatusInternalServerError)
						return
					}
				}
			}
		}
	} else {
		// Disable path: capture current inbound tags before removing the user.
		if currentInbounds, err := s.config.GetUserInbounds(originalName); err == nil && len(currentInbounds) > 0 {
			tags := make([]string, len(currentInbounds))
			for i, ib := range currentInbounds {
				tags[i] = ib.Tag
			}
			inboundTags = tags
		}
		// If GetUserInbounds failed or returned empty, preserve existing inboundTags from metadata.

		s.config.RemoveUser(originalName)
		if originalName != req.Name {
			s.config.RemoveUser(req.Name)
		}
	}

	meta := core.UserMetadata{
		Email:         req.Name,
		QuotaLimit:    req.QuotaLimit,
		QuotaPeriod:   req.QuotaPeriod,
		ResetDay:      1,
		Enabled:       enabled,
		VmessSecurity: req.VmessSecurity,
		VmessAlterID:  req.VmessAlterID,
		InboundTags:   inboundTags,
	}
	if err := s.store.SaveUserMetadata(meta); err != nil {
		http.Error(w, "Failed to save metadata: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if originalName != req.Name {
		s.store.DeleteUserMetadata(originalName)
	}

	s.cache.Del("api:status")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}

	if err := s.config.RemoveUser(name); err != nil {
		http.Error(w, "Failed to remove user from config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := s.store.DeleteUserMetadata(name); err != nil {
		http.Error(w, "Failed to delete metadata: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.cache.Del("api:status")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleRemoveUserFromInbound(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}

	name := r.PathValue("name")
	tag := r.PathValue("tag")

	if name == "" || tag == "" {
		http.Error(w, "Name and tag are required", http.StatusBadRequest)
		return
	}

	if err := s.config.RemoveUserFromInbound(name, tag); err != nil {
		http.Error(w, "Failed to remove user from inbound: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Note: We don't delete metadata here since user might still exist in other inbounds
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleUpdateUserInInbound(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}

	name := r.PathValue("name")
	tag := r.PathValue("tag")
	if name == "" || tag == "" {
		http.Error(w, "Name and tag are required", http.StatusBadRequest)
		return
	}

	var req struct {
		UUID          string `json:"uuid"`
		Flow          string `json:"flow"`
		VmessSecurity string `json:"vmess_security,omitempty"`
		VmessAlterID  int    `json:"vmess_alter_id,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.UUID == "" {
		http.Error(w, "UUID is required", http.StatusBadRequest)
		return
	}

	if err := s.config.UpdateUserInInbound(name, req.UUID, req.Flow, tag, req.VmessSecurity, req.VmessAlterID); err != nil {
		http.Error(w, "Failed to update user in inbound: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if req.VmessSecurity != "" || req.VmessAlterID != 0 {
		if meta, err := s.store.GetUserMetadata(name); err == nil && meta != nil {
			if req.VmessSecurity != "" {
				meta.VmessSecurity = req.VmessSecurity
			}
			if req.VmessAlterID != 0 {
				meta.VmessAlterID = req.VmessAlterID
			}
			_ = s.store.SaveUserMetadata(*meta)
		}
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleBulkCreateUsers(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}
	var reqs []CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&reqs); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	for _, req := range reqs {
		if req.Name == "" {
			continue
		}
		if req.UUID == "" {
			req.UUID = uuid.NewString()
		}
		// InboundTag might be in req if we update bulk UI, otherwise empty (all managed)
		if err := s.config.AddUser(req.Name, req.UUID, req.Flow, req.InboundTag, req.VmessSecurity, req.VmessAlterID); err != nil {
			log.Printf("Bulk create failed for %s: %v", req.Name, err)
			// Continue with others
		}

		enabled := true
		meta := core.UserMetadata{
			Email:         req.Name,
			QuotaLimit:    req.QuotaLimit,
			QuotaPeriod:   req.QuotaPeriod,
			ResetDay:      1,
			Enabled:       enabled,
			VmessSecurity: req.VmessSecurity,
			VmessAlterID:  req.VmessAlterID,
		}
		s.store.SaveUserMetadata(meta)
	}

	s.cache.Del("api:status")
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) handleGetReport(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}
	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")

	var start, end int64
	if startStr != "" {
		if ts, err := strconv.ParseInt(startStr, 10, 64); err == nil {
			start = ts
		} else if t, err := time.Parse("2006-01-02", startStr); err == nil {
			start = t.Unix()
		}
	}
	if endStr != "" {
		if ts, err := strconv.ParseInt(endStr, 10, 64); err == nil {
			end = ts
		} else if t, err := time.Parse("2006-01-02", endStr); err == nil {
			end = t.Add(24 * time.Hour).Unix()
		}
	}

	if start == 0 {
		start = time.Now().Add(-30 * 24 * time.Hour).Unix()
	}
	if end == 0 {
		end = time.Now().Unix()
	}

	users, err := s.config.GetActiveUsers()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	result := []UserStatus{}
	redactUsers := shouldRedactUsersReadOnly(r)
	for _, user := range users {
		samples, err := s.store.GetCombinedReport(user.Name, start, end)
		if err != nil {
			continue
		}
		var up, down int64
		for _, smp := range samples {
			up += smp.Uplink
			down += smp.Downlink
		}
		uuid := user.UUID
		if redactUsers && strings.TrimSpace(uuid) != "" {
			uuid = maskedValue
		}
		result = append(result, UserStatus{
			Name:     user.Name,
			UUID:     uuid,
			Flow:     user.Flow,
			Uplink:   up,
			Downlink: down,
			Total:    up + down,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (s *Server) handleGetReportSummary(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}
	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")
	limitStr := r.URL.Query().Get("limit_bytes")

	var start, end int64
	if startStr != "" {
		if ts, err := strconv.ParseInt(startStr, 10, 64); err == nil {
			start = ts
		} else if t, err := time.Parse("2006-01-02", startStr); err == nil {
			start = t.Unix()
		}
	}
	if endStr != "" {
		if ts, err := strconv.ParseInt(endStr, 10, 64); err == nil {
			end = ts
		} else if t, err := time.Parse("2006-01-02", endStr); err == nil {
			end = t.Add(24 * time.Hour).Unix()
		}
	}
	if start == 0 || end == 0 {
		end = time.Now().Unix()
		start = time.Now().Add(-24 * time.Hour).Unix()
	}
	var limitBytes int64
	if limitStr != "" {
		if v, err := strconv.ParseInt(limitStr, 10, 64); err == nil && v > 0 {
			limitBytes = v
		}
	}

	users, err := s.config.GetActiveUsers()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	type Row struct {
		Name     string `json:"name"`
		Uplink   int64  `json:"uplink"`
		Downlink int64  `json:"downlink"`
		Total    int64  `json:"total"`
		Exceeded bool   `json:"exceeded"`
	}
	result := []Row{}
	for _, user := range users {
		samples, err := s.store.GetCombinedReport(user.Name, start, end)
		if err != nil {
			continue
		}
		var up, down int64
		for _, smp := range samples {
			up += smp.Uplink
			down += smp.Downlink
		}
		total := up + down
		exceeded := limitBytes > 0 && total > limitBytes
		result = append(result, Row{
			Name:     user.Name,
			Uplink:   up,
			Downlink: down,
			Total:    total,
			Exceeded: exceeded,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}
	content, err := s.config.GetSingboxConfig()
	if err != nil {
		http.Error(w, "Failed to read config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if shouldRedactConfigReadOnly(r) {
		content = redactSingboxJSON(content)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(content))
}

func (s *Server) handleGetLogs(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}
	filterUser := strings.TrimSpace(r.URL.Query().Get("user"))
	limit := 200
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil {
			if v < 20 {
				v = 20
			} else if v > 1000 {
				v = 1000
			}
			limit = v
		}
	}

	var lines []string
	var err error

	if s.config.LogSource == "journal" || s.config.AccessLogPath == "" {
		if s.executor != nil {
			lines, err = s.executor.ReadJournal(r.Context(), "sing-box", limit)
		} else {
			lines, err = readJournalLines("sing-box", limit)
		}
	} else {
		lines, err = tailFileLines(s.config.AccessLogPath, 256*1024, limit)
		if err != nil && s.config.LogSource == "file" {
			// Fallback to journal if file missing or unreadable
			if s.executor != nil {
				if linesJ, jErr := s.executor.ReadJournal(r.Context(), "sing-box", limit); jErr == nil {
					lines = linesJ
					err = nil
				}
			} else if linesJ, jErr := readJournalLines("sing-box", limit); jErr == nil {
				lines = linesJ
				err = nil
			}
		}
	}
	if err != nil {
		log.Printf("handleGetLogs: cannot read logs: %v", err)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"logs": []string{"Failed to read logs: " + err.Error()},
		})
		return
	}
	if filterUser != "" {
		f := strings.ToLower(filterUser)
		filtered := make([]string, 0, len(lines))
		for _, ln := range lines {
			if strings.Contains(strings.ToLower(ln), f) {
				filtered = append(filtered, ln)
			}
		}
		lines = filtered
	}
	if len(lines) == 0 {
		if s.config.LogSource == "journal" {
			lines = []string{"(no log lines found in journal for sing-box)"}
		} else {
			lines = []string{"(no log lines found in " + s.config.AccessLogPath + ")"}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"logs": lines,
	})
}

func (s *Server) handleSearchLogs(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		http.Error(w, "q is required", http.StatusBadRequest)
		return
	}
	pageSize := 200
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 2000 {
			pageSize = v
		}
	}
	if ps := r.URL.Query().Get("page_size"); ps != "" {
		if v, err := strconv.Atoi(ps); err == nil && v > 0 && v <= 2000 {
			pageSize = v
		}
	}
	page := 1
	if p := r.URL.Query().Get("page"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 && v <= 1000 {
			page = v
		}
	}
	effectiveLimit := page * pageSize
	if effectiveLimit > 5000 {
		effectiveLimit = 5000
	}

	var lines []string
	var err error

	if s.config.LogSource == "journal" || s.config.AccessLogPath == "" {
		if s.executor != nil {
			lines, err = s.executor.SearchJournal(r.Context(), "sing-box", q, effectiveLimit)
		} else {
			lines, err = searchJournalLines("sing-box", q, effectiveLimit)
		}
	} else {
		lines, err = searchFileLines(s.config.AccessLogPath, q, effectiveLimit)
		if (err != nil || len(lines) == 0) && s.config.LogSource == "file" {
			// Fallback to journal if file missing/unreadable or no matches
			if s.executor != nil {
				if linesJ, jErr := s.executor.SearchJournal(r.Context(), "sing-box", q, effectiveLimit); jErr == nil {
					lines = linesJ
					err = nil
				}
			} else if linesJ, jErr := searchJournalLines("sing-box", q, effectiveLimit); jErr == nil {
				lines = linesJ
				err = nil
			}
		}
	}
	if err != nil {
		log.Printf("handleSearchLogs: cannot search logs: %v", err)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"logs": []string{"Failed to search logs: " + err.Error()},
		})
		return
	}
	start := (page - 1) * pageSize
	end := start + pageSize
	if start > len(lines) {
		start = len(lines)
	}
	if end > len(lines) {
		end = len(lines)
	}
	paged := lines[start:end]
	hasMore := len(lines) == effectiveLimit && end == len(lines)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"logs":      paged,
		"page":      page,
		"page_size": pageSize,
		"has_more":  hasMore,
	})
}

func tailFileLines(path string, maxBytes int64, maxLines int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	var start int64
	if info.Size() > maxBytes {
		start = info.Size() - maxBytes
	}

	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, err
	}

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(data), "\n")
	if start > 0 && len(lines) > 0 {
		lines = lines[1:]
	}
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines, nil
}

func readJournalLines(unit string, maxLines int) ([]string, error) {
	if _, err := exec.LookPath("journalctl"); err != nil {
		log.Printf("journalctl not found: %v", err)
		return []string{"(journalctl not available on this system)"}, nil
	}
	cmd := exec.Command("journalctl", "-u", unit, "-n", strconv.Itoa(maxLines), "--no-pager")
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" || strings.Contains(strings.ToLower(msg), "no entries") || len(out) == 0 {
			return []string{}, nil
		}
		return nil, err
	}
	data := strings.TrimSpace(string(out))
	if data == "" {
		return []string{}, nil
	}
	return strings.Split(data, "\n"), nil
}

func searchJournalLines(unit, query string, maxLines int) ([]string, error) {
	if _, err := exec.LookPath("journalctl"); err != nil {
		log.Printf("journalctl not found: %v", err)
		return []string{"(journalctl not available on this system)"}, nil
	}
	// journalctl --grep does not match timestamps; fetch full log and filter newest first until limit.
	cmd := exec.Command("journalctl", "-u", unit, "--no-pager")
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" || strings.Contains(strings.ToLower(msg), "no entries") || len(out) == 0 {
			return []string{}, nil
		}
		return nil, err
	}
	data := strings.TrimSpace(string(out))
	if data == "" {
		return []string{}, nil
	}
	lines := strings.Split(data, "\n")
	q := strings.ToLower(query)
	matched := make([]string, 0, maxLines)
	for i := len(lines) - 1; i >= 0 && len(matched) < maxLines; i-- {
		if strings.Contains(strings.ToLower(lines[i]), q) {
			matched = append(matched, lines[i])
		}
	}
	// reverse to keep chronological order
	for i, j := 0, len(matched)-1; i < j; i, j = i+1, j-1 {
		matched[i], matched[j] = matched[j], matched[i]
	}
	return matched, nil
}

func searchFileLines(path, query string, maxLines int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	const chunkSize = 64 * 1024
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := info.Size()
	q := strings.ToLower(query)
	var matched []string
	rem := ""

	for offset := size; offset > 0 && len(matched) < maxLines; {
		readSize := int64(chunkSize)
		if offset < readSize {
			readSize = offset
		}
		offset -= readSize
		buf := make([]byte, readSize)
		if _, err := f.ReadAt(buf, offset); err != nil && err != io.EOF {
			return nil, err
		}
		data := string(buf) + rem
		lines := strings.Split(data, "\n")
		if offset > 0 && len(lines) > 0 {
			rem = lines[0]
			lines = lines[1:]
		} else {
			rem = ""
		}
		for i := len(lines) - 1; i >= 0 && len(matched) < maxLines; i-- {
			if strings.Contains(strings.ToLower(lines[i]), q) {
				matched = append(matched, lines[i])
			}
		}
	}
	// reverse to chronological order
	for i, j := 0, len(matched)-1; i < j; i, j = i+1, j-1 {
		matched[i], matched[j] = matched[j], matched[i]
	}
	return matched, nil
}

func detectLogSource(cfg *core.Config) string {
	source := strings.ToLower(strings.TrimSpace(cfg.LogSource))
	if source == "" {
		source = "journal"
	}
	if source != "journal" && source != "file" {
		log.Printf("Unknown log_source %q, defaulting to journal", cfg.LogSource)
		return "journal"
	}
	return source
}
