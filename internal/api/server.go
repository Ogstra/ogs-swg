package api

import (
	"compress/gzip"
	"context"
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
	sqlcStore "github.com/Ogstra/ogs-swg/internal/core/store"
	"github.com/Ogstra/ogs-swg/internal/sys"
	"github.com/alitto/pond"
	"github.com/dgraph-io/ristretto"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
)

type Server struct {
	store                *core.Store
	config               *core.Config
	executor             core.SystemExecutor
	now                  func() time.Time
	sampler              *core.StatsSampler
	pool                 *pond.WorkerPool
	validate             *validator.Validate
	cache                *ristretto.Cache
	wgPendingRestart     bool
	wgQRCache            map[string]qrEntry
	wgQRCacheMutex       sync.RWMutex
	wgMux                sync.RWMutex
	wgSamplerTicker      *time.Ticker
	wgSamplerStop        chan struct{}
	wgSamplerPaused      bool
	wgLast               map[string]core.WGSample
	loginLimiter         *loginLimiter
	subscriptionLimiter  *subscriptionLimiter
	protectionRules      *protectionRuleCache
	blockedRecordDedup   map[string]time.Time
	blockedRecordDedupMu sync.Mutex
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

	srv := &Server{
		store:               store,
		config:              config,
		executor:            executor,
		now:                 time.Now,
		sampler:             nil,
		pool:                pond.New(100, 1000, pond.IdleTimeout(30*time.Second)),
		validate:            validator.New(),
		cache:               cache,
		wgPendingRestart:    false,
		wgQRCache:           make(map[string]qrEntry),
		wgQRCacheMutex:      sync.RWMutex{},
		wgSamplerStop:       make(chan struct{}),
		wgSamplerTicker:     time.NewTicker(interval),
		wgMux:               sync.RWMutex{},
		wgLast:              make(map[string]core.WGSample),
		wgSamplerPaused:     false,
		loginLimiter:        newLoginLimiter(),
		subscriptionLimiter: newSubscriptionLimiter(),
		protectionRules:     newProtectionRuleCache(),
		blockedRecordDedup:  make(map[string]time.Time),
	}
	srv.reloadProtectionRules(context.Background())
	return srv
}

type gzipResponseWriter struct {
	io.Writer
	http.ResponseWriter
}

func (w gzipResponseWriter) Write(b []byte) (int, error) {
	return w.Writer.Write(b)
}

func (w gzipResponseWriter) Flush() {
	if flusher, ok := w.Writer.(interface{ Flush() error }); ok {
		_ = flusher.Flush()
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
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

	// Public Subscription
	mux.HandleFunc("GET /s/{token}", s.handlePublicSubscription)

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

	protected.HandleFunc("GET /api/settings/subscription-protection", s.secure(s.requirePerm(canReadSettings, s.handleGetSubscriptionProtection)))
	protected.HandleFunc("PUT /api/settings/subscription-protection", s.secure(s.requirePerm(canWriteSettings, s.handleUpdateSubscriptionProtection)))
	protected.HandleFunc("GET /api/settings/protection-rules", s.secure(s.requirePerm(canReadSettings, s.handleGetProtectionRules)))
	protected.HandleFunc("POST /api/settings/protection-rules", s.secure(s.requirePerm(canWriteSettings, s.handleCreateProtectionRule)))
	protected.HandleFunc("DELETE /api/settings/protection-rules/{id}", s.secure(s.requirePerm(canWriteSettings, s.handleDeleteProtectionRule)))
	protected.HandleFunc("GET /api/settings/protection-rules/blocked-log", s.secure(s.requirePerm(canReadSettings, s.handleGetBlockedLog)))

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
	protected.HandleFunc("PUT /api/users/{name}/route-tags", s.secure(s.requirePerm(canWriteUsers, s.handleUpdateUserRouteTags)))
	protected.HandleFunc("POST /api/users/bulk", s.secure(s.requirePerm(canWriteUsers, s.handleBulkCreateUsers)))
	protected.HandleFunc("GET /api/user-route-tags", s.secure(s.requirePerm(canReadUsers, s.handleGetUserRouteTags)))
	protected.HandleFunc("POST /api/user-route-tags", s.secure(s.requirePerm(canWriteUsers, s.handleCreateUserRouteTag)))
	protected.HandleFunc("PUT /api/user-route-tags/{id}", s.secure(s.requirePerm(canWriteUsers, s.handleUpdateUserRouteTag)))
	protected.HandleFunc("DELETE /api/user-route-tags/{id}", s.secure(s.requirePerm(canWriteUsers, s.handleDeleteUserRouteTag)))
	protected.HandleFunc("GET /api/user-route-tags/compatible-rules", s.secure(s.requirePerm(canReadConfig, s.handleGetCompatibleUserRouteRules)))

	// Reports/logs
	protected.HandleFunc("GET /api/report", s.secure(s.requirePerm(canReadUsers, s.handleGetReport)))
	protected.HandleFunc("GET /api/report/summary", s.secure(s.requirePerm(canReadUsers, s.handleGetReportSummary)))
	protected.HandleFunc("GET /api/logs", s.secure(s.requirePerm(canReadLogs, s.handleGetLogs)))
	protected.HandleFunc("GET /api/logs/search", s.secure(s.requirePerm(canReadLogs, s.handleSearchLogs)))
	protected.HandleFunc("GET /api/logs/search/stream", s.secure(s.requirePerm(canReadLogs, s.handleSearchLogsStream)))
	protected.HandleFunc("GET /api/dashboard", s.secure(s.handleGetDashboardData))
	protected.HandleFunc("GET /api/dashboard/consumer-chart", s.secure(s.handleGetDashboardConsumerChart))
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
	protected.HandleFunc("GET /api/wireguard/config/backups", s.secure(s.requirePerm(canReadWG, s.handleListWireGuardConfigBackups)))
	protected.HandleFunc("GET /api/wireguard/config/backup", s.secure(s.requirePerm(canReadWG, s.handleGetWireGuardConfigBackup)))
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
	protected.HandleFunc("POST /api/wireguard/interfaces/{iface}/config/backup", s.secure(s.requirePerm(canWriteWG, s.handleBackupWireGuardConfigForInterface)))
	protected.HandleFunc("GET /api/wireguard/interfaces/{iface}/config/backups", s.secure(s.requirePerm(canReadWG, s.handleListWireGuardConfigBackupsForInterface)))
	protected.HandleFunc("GET /api/wireguard/interfaces/{iface}/config/backup", s.secure(s.requirePerm(canReadWG, s.handleGetWireGuardConfigBackupForInterface)))
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
	protected.HandleFunc("GET /api/singbox/dns", s.secure(s.requirePerm(canReadConfig, s.handleGetSingboxDNS)))
	protected.HandleFunc("PUT /api/singbox/dns", s.secure(s.requirePerm(canWriteConfig, s.handleUpdateSingboxDNS)))
	protected.HandleFunc("GET /api/singbox/outbounds", s.secure(s.requirePerm(canReadConfig, s.handleGetSingboxOutbounds)))
	protected.HandleFunc("PUT /api/singbox/outbounds/domain-strategy", s.secure(s.requirePerm(canWriteConfig, s.handleUpdateSingboxOutboundDomainStrategies)))
	protected.HandleFunc("GET /api/singbox/inbounds", s.secure(s.requirePerm(canReadConfig, s.handleGetSingboxInbounds)))
	protected.HandleFunc("POST /api/singbox/inbound", s.secure(s.requirePerm(canWriteConfig, s.handleAddSingboxInbound)))
	protected.HandleFunc("PUT /api/singbox/inbound", s.secure(s.requirePerm(canWriteConfig, s.handleUpdateSingboxInbound)))
	protected.HandleFunc("DELETE /api/singbox/inbound", s.secure(s.requirePerm(canWriteConfig, s.handleDeleteSingboxInbound)))
	protected.HandleFunc("POST /api/singbox/apply", s.secure(s.requirePerm(canWriteConfig, s.handleApplySingboxChanges)))
	protected.HandleFunc("GET /api/singbox/route/rules", s.secure(s.requirePerm(canReadConfig, s.handleGetSingboxRouteRules)))
	protected.HandleFunc("POST /api/singbox/route/rules/upsert", s.secure(s.requirePerm(canWriteConfig, s.handleUpsertSingboxRouteRules)))
	protected.HandleFunc("POST /api/service/restart", s.secure(s.requirePerm(canWriteConfig, s.handleRestartService)))
	protected.HandleFunc("POST /api/service/start", s.secure(s.requirePerm(canWriteConfig, s.handleStartService)))
	protected.HandleFunc("POST /api/service/stop", s.secure(s.requirePerm(canWriteConfig, s.handleStopService)))
	protected.HandleFunc("POST /api/config/backup", s.secure(s.requirePerm(canWriteConfig, s.handleBackupConfig)))
	protected.HandleFunc("POST /api/config/restore", s.secure(s.requirePerm(canWriteConfig, s.handleRestoreConfig)))
	protected.HandleFunc("GET /api/config/backup/meta", s.secure(s.requirePerm(canReadConfig, s.handleGetBackupMeta)))
	protected.HandleFunc("GET /api/config/backups", s.secure(s.requirePerm(canReadConfig, s.handleListConfigBackups)))
	protected.HandleFunc("GET /api/config/backup", s.secure(s.requirePerm(canReadConfig, s.handleGetConfigBackup)))
	protected.HandleFunc("GET /api/tools/reality-keys", s.secure(s.requirePerm(canWriteConfig, s.handleGenerateRealityKeys)))
	protected.HandleFunc("POST /api/tools/self-signed-cert", s.secure(s.requirePerm(canWriteConfig, s.handleGenerateSelfSignedCert)))
	protected.HandleFunc("POST /api/tools/rand-base64", s.secure(s.handleGenerateRandBase64))
	protected.HandleFunc("GET /api/sysctl", s.secure(s.requirePerm(canReadConfig, s.handleGetSysctl)))
	protected.HandleFunc("POST /api/sysctl", s.secure(s.requirePerm(canWriteConfig, s.handleApplySysctl)))

	// Settings
	protected.HandleFunc("GET /api/settings/features", s.secure(s.requirePerm(canReadSettings, s.handleGetFeatures)))
	protected.HandleFunc("PUT /api/settings/features", s.secure(s.requirePerm(canWriteSettings, s.handleUpdateFeatures)))
	protected.HandleFunc("GET /api/settings/dashboard-preferences", s.secure(s.requirePerm(canReadSettings, s.handleGetDashboardPreferences)))
	protected.HandleFunc("PUT /api/settings/dashboard-preferences", s.secure(s.requirePerm(canWriteSettings, s.handleUpdateDashboardPreferences)))
	protected.HandleFunc("GET /api/settings/public-ip", s.secure(s.requirePerm(canReadSettings, s.handleGetPublicIP)))
	protected.HandleFunc("PUT /api/settings/public-ip", s.secure(s.requirePerm(canWriteSettings, s.handleUpdatePublicIP)))
	protected.HandleFunc("GET /api/settings/subscription-domain", s.secure(s.requirePerm(canReadSettings, s.handleGetSubscriptionDomain)))
	protected.HandleFunc("PUT /api/settings/subscription-domain", s.secure(s.requirePerm(canWriteSettings, s.handleUpdateSubscriptionDomain)))
	protected.HandleFunc("POST /api/sampler/run", s.secure(s.requirePerm(canWriteSettings, s.handleRunSampler)))
	protected.HandleFunc("GET /api/sampler/history", s.secure(s.requirePerm(canReadSettings, s.handleSamplerHistory)))
	protected.HandleFunc("GET /api/subscription-requests/history", s.secure(s.requirePerm(canReadSettings, s.handleSubscriptionRequestHistory)))
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

	// Subscriptions
	protected.HandleFunc("GET /api/subscriptions", s.secure(s.requirePerm(canReadUsers, s.handleGetSubscriptions)))
	protected.HandleFunc("POST /api/subscriptions", s.secure(s.requirePerm(canWriteUsers, s.handleCreateSubscription)))
	protected.HandleFunc("GET /api/subscriptions/defaults", s.secure(s.requirePerm(canReadUsers, s.handleGetSubscriptionDefaults)))
	protected.HandleFunc("GET /api/subscriptions/default-destinations", s.secure(s.requirePerm(canReadUsers, s.handleGetSubscriptionDefaultDestinations)))
	protected.HandleFunc("PUT /api/subscriptions/defaults", s.secure(s.requirePerm(canWriteUsers, s.handleUpdateSubscriptionDefaults)))
	protected.HandleFunc("GET /api/subscriptions/{id}", s.secure(s.requirePerm(canReadUsers, s.handleGetSubscription)))
	protected.HandleFunc("PUT /api/subscriptions/{id}", s.secure(s.requirePerm(canWriteUsers, s.handleUpdateSubscription)))
	protected.HandleFunc("DELETE /api/subscriptions/{id}", s.secure(s.requirePerm(canWriteUsers, s.handleDeleteSubscription)))
	protected.HandleFunc("POST /api/subscriptions/{id}/regenerate", s.secure(s.requirePerm(canWriteUsers, s.handleRegenerateSubscriptionToken)))

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

	if cfg.EnableSingbox && !cfg.DemoMode {
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
	} else if cfg.EnableSingbox && cfg.DemoMode {
		log.Printf("Demo mode: skipping sing-box watcher/sampler; seeded demo data is authoritative")
	} else {
		log.Printf("sing-box disabled via config; skipping watcher/sampler")
	}

	if cfg.EnableWireGuard && !cfg.DemoMode {
		server.startWireGuardSampler()
	} else if cfg.EnableWireGuard && cfg.DemoMode {
		log.Printf("Demo mode: skipping WireGuard sampler; seeded demo data is authoritative")
	}

	// Start background maintenance (Retention & Vacuum)
	if !cfg.DemoMode {
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

					err = store.PruneSubscriptionRequestsOlderThan(cutoff)
					if err != nil {
						log.Printf("Subscription retention prune error: %v", err)
					} else {
						log.Printf("Subscription retention prune: removed requests older than %d", cutoff)
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
	} else {
		log.Printf("Demo mode: skipping DB maintenance; demo seeder manages retention")
	}

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

	registerFrontendRoutes(router, distDir)

	// Start server in goroutine so we can return the server instance
	go func() {
		if err := http.ListenAndServe(cfg.ListenAddr, router); err != nil {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	return server
}

const (
	frontendDocumentCacheControl = "no-store, no-cache, must-revalidate"
	frontendAssetCacheControl    = "public, max-age=31536000, immutable"
)

func registerFrontendRoutes(router *http.ServeMux, distDir string) {
	fs := http.FileServer(http.Dir(distDir))
	assetHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if distDir == "" {
			http.Error(w, "frontend assets not found", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Cache-Control", frontendAssetCacheControl)
		http.StripPrefix("/", fs).ServeHTTP(w, r)
	})

	router.Handle("/assets/", assetHandler)
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
				switch {
				case strings.EqualFold(filepath.Base(fullPath), "sw.js"):
					setFrontendDocumentCacheHeaders(w)
					w.Header().Set("Service-Worker-Allowed", "/")
				case strings.EqualFold(filepath.Ext(fullPath), ".html"):
					setFrontendDocumentCacheHeaders(w)
				case strings.EqualFold(filepath.Ext(fullPath), ".webmanifest"):
					w.Header().Set("Cache-Control", "public, max-age=3600")
				}
				http.ServeFile(w, r, fullPath)
				return
			}
		}

		setFrontendDocumentCacheHeaders(w)
		http.ServeFile(w, r, filepath.Join(distDir, "index.html"))
	})
}

func setFrontendDocumentCacheHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", frontendDocumentCacheControl)
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

// SubQuotaInfo carries subscription-level quota context for a user.
type SubQuotaInfo struct {
	Name       string `json:"name"`
	QuotaLimit int64  `json:"quota_limit"`
	UsedBytes  int64  `json:"used_bytes"`
}

type UserStatus struct {
	Name              string               `json:"name"`
	UUID              string               `json:"uuid"`
	Flow              string               `json:"flow"`
	VmessSecurity     string               `json:"vmess_security,omitempty"`
	VmessAlterID      int                  `json:"vmess_alter_id,omitempty"`
	Uplink            int64                `json:"uplink"`
	Downlink          int64                `json:"downlink"`
	Total             int64                `json:"total"`
	QuotaLimit        int64                `json:"quota_limit"`
	QuotaPeriod       string               `json:"quota_period"`
	ResetDay          int                  `json:"reset_day"`
	Enabled           bool                 `json:"enabled"`
	LastSeen          int64                `json:"last_seen"`
	InboundTags       []string             `json:"inbound_tags"`
	RouteTags         []UserRouteTagStatus `json:"route_tags"`
	SubscriptionQuota *SubQuotaInfo        `json:"subscription_quota,omitempty"`
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
	demoActiveUsers := make(map[string]bool)
	if s.config.DemoMode && s.store != nil {
		if users, err := s.store.GetActiveUsersWithThreshold(5*time.Minute, s.config.ActiveThresholdBytes); err == nil {
			for _, user := range users {
				demoActiveUsers[user] = true
			}
		}
		if len(demoActiveUsers) == 0 {
			if users, err := s.store.GetActiveUsers(5 * time.Minute); err == nil {
				for _, user := range users {
					demoActiveUsers[user] = true
				}
			}
		}
	}
	routeTagDefinitions, err := s.store.ListUserRouteTags()
	if err != nil {
		http.Error(w, "Failed to load route tags: "+err.Error(), http.StatusInternalServerError)
		return
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
			if uuid == "" && meta.Credential != "" {
				uuid = meta.Credential
			}
			if flow == "" && meta.Flow != "" {
				flow = meta.Flow
			}
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

		// Live sing-box state wins over stale metadata for effective enabled status.
		// This prevents users manually restored to an inbound from staying stuck as
		// "Disabled" in the UI until the next sampler reconciliation.
		if isActive {
			enabled = true
			if hasMeta && metadataNeedsActiveBackfill(meta, user) {
				metaToSave := meta
				metaToSave.Enabled = true
				if metaToSave.Credential == "" {
					metaToSave.Credential = user.UUID
				}
				if metaToSave.Flow == "" {
					metaToSave.Flow = user.Flow
				}
				if len(metaToSave.InboundTags) == 0 {
					metaToSave.InboundTags = canonicalInboundTags(user.InboundTags...)
				}
				if metaToSave.VmessSecurity == "" {
					metaToSave.VmessSecurity = user.VmessSecurity
				}
				if metaToSave.VmessAlterID == 0 {
					metaToSave.VmessAlterID = user.VmessAlterID
				}
				_ = s.store.SaveUserMetadata(metaToSave)
			}
		}

		// Stats calculation for user table indicators:
		// always show current calendar month usage.
		now := s.now()
		startOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		combinedSamples, err := s.store.GetCombinedReport(name, startOfMonth.Unix(), now.Unix())
		var up, down int64
		lastSeen := int64(0)
		if err == nil {
			for _, smp := range combinedSamples {
				up += smp.Uplink
				down += smp.Downlink
			}
		}

		// Keep lastSeen based on raw samples/threshold lookup so the activity signal
		// remains tied to recent sample timestamps rather than compressed buckets.
		samples, err := s.store.GetSamples(name, startOfMonth.Unix(), now.Unix())
		if err == nil {
			for _, smp := range samples {
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
		if s.config.DemoMode {
			if demoActiveUsers[name] {
				lastSeen = demoModeLastSeenInRange("singbox-user-active", name, now, 5, 240)
			} else {
				lastSeen = demoModeLastSeenInRange("singbox-user-idle", name, now, 6*60, 9*60*60)
			}
		}
		routeTags, err := s.routeTagStatusesForUser(name, routeTagDefinitions)
		if err != nil {
			http.Error(w, "Failed to resolve route tags: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Subscription quota: check if this user belongs to a subscription with quota_limit > 0.
		var subQuota *SubQuotaInfo
		if subs, subErr := s.store.Queries.GetSubscriptionsForUser(r.Context(), name); subErr == nil {
			for _, sub := range subs {
				if sub.QuotaLimit.Int64 > 0 {
					subUsed, _ := s.store.Queries.GetSubscriptionUsageInRange(r.Context(), sqlcStore.GetSubscriptionUsageInRangeParams{
						SubID: sub.ID,
						Ts:    startOfMonth.Unix(),
						Ts_2:  now.Unix(),
					})
					subQuota = &SubQuotaInfo{
						Name:       sub.Name,
						QuotaLimit: sub.QuotaLimit.Int64,
						UsedBytes:  subUsed,
					}
					break // first sub with quota wins
				}
			}
		}

		result = append(result, UserStatus{
			Name:              name,
			UUID:              uuid,
			Flow:              flow,
			VmessSecurity:     vmessSecurity,
			VmessAlterID:      vmessAlterID,
			Uplink:            up,
			Downlink:          down,
			Total:             up + down,
			QuotaLimit:        limit,
			QuotaPeriod:       period,
			ResetDay:          1,
			Enabled:           enabled,
			LastSeen:          lastSeen,
			InboundTags:       inboundTags,
			RouteTags:         routeTags,
			SubscriptionQuota: subQuota,
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

func canonicalInboundTags(tags ...string) []string {
	for _, tag := range tags {
		trimmed := strings.TrimSpace(tag)
		if trimmed != "" {
			return []string{trimmed}
		}
	}
	return []string{}
}

func metadataNeedsActiveBackfill(meta core.UserMetadata, user core.UserAccount) bool {
	return len(meta.InboundTags) == 0 || (meta.Credential == "" && user.UUID != "") || (meta.Flow == "" && user.Flow != "") || (meta.VmessSecurity == "" && user.VmessSecurity != "") || (meta.VmessAlterID == 0 && user.VmessAlterID != 0)
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
			if errors.Is(err, os.ErrInvalid) || errors.Is(err, core.ErrUserAssignedToAnotherInbound) {
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
		Credential:    req.UUID,
		Flow:          req.Flow,
		VmessSecurity: req.VmessSecurity,
		VmessAlterID:  req.VmessAlterID,
		InboundTags:   canonicalInboundTags(req.InboundTag),
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
		if existingMeta != nil && existingMeta.Credential != "" {
			req.UUID = existingMeta.Credential
		} else {
			req.UUID = uuid.NewString()
		}
	}

	// inboundTags tracks which inbound tags to persist in metadata.
	var inboundTags []string
	if existingMeta != nil {
		inboundTags = canonicalInboundTags(existingMeta.InboundTags...)
	}
	if req.InboundTag != "" {
		inboundTags = canonicalInboundTags(req.InboundTag)
	}

	if enabled {
		nameChanged := originalName != req.Name
		if nameChanged {
			if err := s.config.RenameUser(originalName, req.Name, req.UUID, req.Flow, req.VmessSecurity, req.VmessAlterID); err != nil {
				http.Error(w, "Failed to rename user in config: "+err.Error(), http.StatusInternalServerError)
				return
			}
			if err := s.store.RenameUserTrafficIdentity(originalName, req.Name); err != nil {
				if rollbackErr := s.config.RenameUser(req.Name, originalName, req.UUID, req.Flow, req.VmessSecurity, req.VmessAlterID); rollbackErr != nil {
					http.Error(w, "Failed to rename user traffic identity: "+err.Error()+" (rollback failed: "+rollbackErr.Error()+")", http.StatusInternalServerError)
					return
				}
				http.Error(w, "Failed to rename user traffic identity: "+err.Error(), http.StatusInternalServerError)
				return
			}
		} else {
			// Re-enable path: restore user in all previously known inbounds.
			// Determine the list of inbounds to restore.
			tagsToRestore := inboundTags
			if len(tagsToRestore) == 0 && req.InboundTag != "" {
				tagsToRestore = canonicalInboundTags(req.InboundTag)
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
			inboundTags = canonicalInboundTags(currentInbounds[0].Tag)
			if currentInbounds[0].UUID != "" {
				req.UUID = currentInbounds[0].UUID
			} else if currentInbounds[0].Password != "" {
				req.UUID = currentInbounds[0].Password
			}
			if currentInbounds[0].Flow != "" {
				req.Flow = currentInbounds[0].Flow
			}
			if currentInbounds[0].VmessSecurity != "" {
				req.VmessSecurity = currentInbounds[0].VmessSecurity
			}
			if currentInbounds[0].VmessAlterID != 0 {
				req.VmessAlterID = currentInbounds[0].VmessAlterID
			}
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
		Credential:    req.UUID,
		Flow:          req.Flow,
		VmessSecurity: req.VmessSecurity,
		VmessAlterID:  req.VmessAlterID,
		InboundTags:   inboundTags,
	}
	if err := s.store.SaveUserMetadata(meta); err != nil {
		if originalName != req.Name {
			if rollbackErr := s.store.RenameUserTrafficIdentity(req.Name, originalName); rollbackErr != nil {
				http.Error(w, "Failed to save metadata: "+err.Error()+" (store rollback failed: "+rollbackErr.Error()+")", http.StatusInternalServerError)
				return
			}
			if rollbackErr := s.config.RenameUser(req.Name, originalName, req.UUID, req.Flow, req.VmessSecurity, req.VmessAlterID); rollbackErr != nil {
				http.Error(w, "Failed to save metadata: "+err.Error()+" (config rollback failed: "+rollbackErr.Error()+")", http.StatusInternalServerError)
				return
			}
		}
		http.Error(w, "Failed to save metadata: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if originalName != req.Name {
		s.store.DeleteUserMetadata(originalName)
	}
	shouldReconcileQuota := existingMeta == nil ||
		existingMeta.QuotaLimit != req.QuotaLimit ||
		existingMeta.QuotaPeriod != req.QuotaPeriod ||
		!existingMeta.Enabled
	if shouldReconcileQuota {
		if err := s.store.ReconcileUserQuotaNow(req.Name, s.config); err != nil {
			http.Error(w, "Failed to reconcile user quota: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	s.cache.Del("api:status")
	s.InvalidateSubCache()
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

	if err := s.store.RemoveUserFromSubscriptions(name); err != nil {
		http.Error(w, "Failed to remove user from subscriptions: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := s.store.DeleteUserMetadata(name); err != nil {
		http.Error(w, "Failed to delete metadata: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.cache.Del("api:status")
	s.InvalidateSubCache()
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

	if err := s.removeUserFromSubscriptionsIfUnassigned(name); err != nil {
		http.Error(w, "Failed to remove user from subscriptions: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.InvalidateSubCache()
	w.WriteHeader(http.StatusOK)
}

func (s *Server) removeUserFromSubscriptionsIfUnassigned(name string) error {
	if s.store == nil || s.config == nil {
		return nil
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	inbounds, err := s.config.GetUserInbounds(name)
	if err != nil {
		return err
	}
	if len(inbounds) > 0 {
		return nil
	}
	return s.store.RemoveUserFromSubscriptions(name)
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

	if meta, err := s.store.GetUserMetadata(name); err == nil && meta != nil {
		meta.Credential = req.UUID
		meta.Flow = req.Flow
		if req.VmessSecurity != "" {
			meta.VmessSecurity = req.VmessSecurity
		}
		if req.VmessAlterID != 0 {
			meta.VmessAlterID = req.VmessAlterID
		}
		meta.InboundTags = canonicalInboundTags(tag)
		_ = s.store.SaveUserMetadata(*meta)
	}

	if err := s.store.ReconcileUserQuotaNow(name, s.config); err != nil {
		http.Error(w, "Failed to reconcile user quota: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.InvalidateSubCache()
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
			Credential:    req.UUID,
			Flow:          req.Flow,
			VmessSecurity: req.VmessSecurity,
			VmessAlterID:  req.VmessAlterID,
			InboundTags:   canonicalInboundTags(req.InboundTag),
		}
		s.store.SaveUserMetadata(meta)
		_ = s.store.ReconcileUserQuotaNow(req.Name, s.config)
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
	filterUser := strings.TrimSpace(r.URL.Query().Get("q"))
	if filterUser == "" {
		filterUser = strings.TrimSpace(r.URL.Query().Get("user"))
	}
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
	compiledQuery := s.compileLogQuery(filterUser)
	postFilterTail := filterUser != "" && compiledQuery.requiresPostFilter()
	censoredTail := func() bool {
		p := getPermissions(r)
		return p != nil && p.CanReadLogsCensored
	}()

	if postFilterTail {
		lines, err = s.readAllSearchableLogLines(r.Context())
	} else if s.config.LogSource == "journal" || s.config.AccessLogPath == "" {
		if s.executor != nil {
			lines, err = s.executor.ReadJournal(r.Context(), "sing-box", limit)
		} else {
			lines, err = readJournalLines(r.Context(), "sing-box", limit)
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
			} else if linesJ, jErr := readJournalLines(r.Context(), "sing-box", limit); jErr == nil {
				lines = linesJ
				err = nil
			}
		}
	}
	if errors.Is(err, context.Canceled) {
		return
	}
	if err != nil {
		log.Printf("handleGetLogs: cannot read logs: %v", err)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"logs": []string{"Failed to read logs: " + err.Error()},
		})
		return
	}
	sanitizeLogLines(lines)
	if !postFilterTail {
		if censoredTail {
			for i, ln := range lines {
				lines[i] = core.CensorLine(ln)
			}
		}
		if filterUser != "" {
			lines = filterLogLines(lines, compiledQuery)
		}
	} else {
		if censoredTail {
			for i, ln := range lines {
				lines[i] = core.CensorLine(ln)
			}
		}
		lines = filterLogLines(lines, compiledQuery)
		if len(lines) == 0 && s.config.LogSource == "file" {
			if journalLines, jErr := s.readAllJournalLogLines(r.Context()); jErr == nil {
				lines = journalLines
				sanitizeLogLines(lines)
				if censoredTail {
					for i, ln := range lines {
						lines[i] = core.CensorLine(ln)
					}
				}
				lines = filterLogLines(lines, compiledQuery)
			}
		}
		lines, _ = truncateRecentLogMatches(lines, limit)
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
	timeRange, err := parseLogTimeRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		http.Error(w, "invalid time range: "+err.Error(), http.StatusBadRequest)
		return
	}
	page, pageSize, effectiveLimit := parseSearchPageParams(r)
	censoredSearch := func() bool {
		p := getPermissions(r)
		return p != nil && p.CanReadLogsCensored
	}()
	var chunks [][]string
	chunkSize := effectiveLimit
	if chunkSize > 200 {
		chunkSize = 200
	}
	summary, err := s.searchLogsIncrementally(r.Context(), logSearchOptions{
		query:     s.compileLogQuery(q),
		timeRange: timeRange,
		censored:  censoredSearch,
		limit:     effectiveLimit,
		chunkSize: chunkSize,
	}, func(chunk []string, _ int) error {
		chunks = append(chunks, chunk)
		return nil
	}, nil)
	if errors.Is(err, context.Canceled) {
		return
	}
	if err != nil {
		log.Printf("handleSearchLogs: cannot search logs: %v", err)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"logs": []string{"Failed to search logs: " + err.Error()},
		})
		return
	}
	lines := collectSearchChunks(chunks)
	start := (page - 1) * pageSize
	end := start + pageSize
	if start > len(lines) {
		start = len(lines)
	}
	if end > len(lines) {
		end = len(lines)
	}
	paged := lines[start:end]
	hasMore := summary.truncated && end == len(lines)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"logs":      paged,
		"page":      page,
		"page_size": pageSize,
		"has_more":  hasMore,
	})
}

func (s *Server) handleSearchLogsStream(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		http.Error(w, "q is required", http.StatusBadRequest)
		return
	}
	timeRange, err := parseLogTimeRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		http.Error(w, "invalid time range: "+err.Error(), http.StatusBadRequest)
		return
	}
	_, _, effectiveLimit := parseSearchPageParams(r)
	censoredSearch := func() bool {
		p := getPermissions(r)
		return p != nil && p.CanReadLogsCensored
	}()

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	writeEvent := func(event map[string]interface{}) error {
		if err := json.NewEncoder(w).Encode(event); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	summary, err := s.searchLogsIncrementally(r.Context(), logSearchOptions{
		query:     s.compileLogQuery(q),
		timeRange: timeRange,
		censored:  censoredSearch,
		limit:     effectiveLimit,
		chunkSize: 100,
	}, func(chunk []string, matched int) error {
		return writeEvent(map[string]interface{}{
			"type":    "chunk",
			"logs":    chunk,
			"matched": matched,
		})
	}, func(message string, matched int) error {
		return writeEvent(map[string]interface{}{
			"type":    "status",
			"message": message,
			"matched": matched,
		})
	})
	if errors.Is(err, context.Canceled) {
		return
	}
	if err != nil {
		log.Printf("handleSearchLogsStream: cannot search logs: %v", err)
		_ = writeEvent(map[string]interface{}{
			"type":    "error",
			"message": "Failed to search logs: " + err.Error(),
		})
		return
	}
	_ = writeEvent(map[string]interface{}{
		"type":      "done",
		"matched":   summary.matched,
		"truncated": summary.truncated,
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

func readJournalLines(ctx context.Context, unit string, maxLines int) ([]string, error) {
	if _, err := exec.LookPath("journalctl"); err != nil {
		log.Printf("journalctl not found: %v", err)
		return []string{"(journalctl not available on this system)"}, nil
	}
	cmd := exec.CommandContext(ctx, "journalctl", "-u", unit, "-n", strconv.Itoa(maxLines), "--no-pager")
	out, err := cmd.CombinedOutput()
	if ctx != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
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

func searchJournalLines(ctx context.Context, unit, query string, maxLines int) ([]string, error) {
	if _, err := exec.LookPath("journalctl"); err != nil {
		log.Printf("journalctl not found: %v", err)
		return []string{"(journalctl not available on this system)"}, nil
	}
	// NOTE for search: --merge includes all rotated journal segments (system@*.journal).
	// Without it, journalctl may only scan the active journal file, missing older entries.
	// -o cat strips the syslog timestamp prefix so filters match message content only.
	cmd := exec.CommandContext(ctx, "journalctl", "-u", unit, "--no-pager", "--merge", "-o", "cat")
	out, err := cmd.CombinedOutput()
	if ctx != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
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
