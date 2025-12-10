package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Ogstra/ogs-swg/core"
	"github.com/google/uuid"
)

type Server struct {
	store            *core.Store
	config           *core.Config
	sampler          *core.StatsSampler
	wgPendingRestart bool
	wgQRCache        map[string]qrEntry
	wgSamplerStop    chan struct{}
	wgSamplerTicker  *time.Ticker
	wgSampleInterval time.Duration
	wgMux            sync.RWMutex
	wgLast           map[string]core.WGSample
	wgSamplerPaused  bool
}

type qrEntry struct {
	Config    string
	ExpiresAt time.Time
}

func NewServer(store *core.Store, config *core.Config) *Server {
	interval := 60 * time.Second
	if config.WGSamplerIntervalSec > 0 {
		interval = time.Duration(config.WGSamplerIntervalSec) * time.Second
	}
	return &Server{
		store:            store,
		config:           config,
		sampler:          nil,
		wgPendingRestart: false,
		wgQRCache:        make(map[string]qrEntry),
		wgSamplerStop:    make(chan struct{}),
		wgSamplerTicker:  time.NewTicker(interval),
		wgSampleInterval: interval,
		wgLast:           make(map[string]core.WGSample),
		wgSamplerPaused:  false,
	}
}

func (s *Server) secure(handler http.HandlerFunc) http.HandlerFunc {
	if s.config.APIKey == "" {
		return handler
	}

	return func(w http.ResponseWriter, r *http.Request) {
		// If authenticated via JWT (AuthMiddleware), allow
		if r.Context().Value("user") != nil {
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

func (s *Server) reloadWireGuard() {
	if !s.config.EnableWireGuard {
		return
	}
	if err := runSystemCtl("restart", "wireguard"); err != nil {
		log.Printf("Failed to reload/restart WireGuard: %v", err)
	}
}

func (s *Server) markWireGuardPending() {
	if s.config.EnableWireGuard {
		s.wgPendingRestart = true
	}
}

func (s *Server) clearWireGuardPending() {
	s.wgPendingRestart = false
}

func (s *Server) startWireGuardSampler() {
	go func() {
		for {
			select {
			case <-s.wgSamplerTicker.C:
				if !s.wgSamplerPaused {
					s.runWireGuardSample()
				}
			case <-s.wgSamplerStop:
				if s.wgSamplerTicker != nil {
					s.wgSamplerTicker.Stop()
				}
				return
			}
		}
	}()
}

func (s *Server) runWireGuardSample() {
	s.wgMux.Lock()
	defer s.wgMux.Unlock()

	stats, err := core.GetWireGuardStats()
	if err != nil {
		log.Printf("wg sampler: failed to read stats: %v", err)
		return
	}
	var samples []core.WGSample
	now := time.Now().Unix()
	for _, st := range stats {
		prev, ok := s.wgLast[st.PublicKey]

		// If we have previous stats, check if they changed
		hasChanged := false
		if !ok {
			// First run for this peer: treat as changed so we establish a baseline
			// Actually, if we want to be strict about "dedup", we need to decide if the first point is needed.
			// Yes, we need at least one point.
			hasChanged = true
		} else {
			if st.TransferRx != prev.Rx || st.TransferTx != prev.Tx {
				hasChanged = true
			}
		}

		if hasChanged {
			samples = append(samples, core.WGSample{
				PublicKey: st.PublicKey,
				Timestamp: now,
				Rx:        st.TransferRx,
				Tx:        st.TransferTx,
				Endpoint:  st.Endpoint,
			})
		}

		// Update cache with current absolute values
		s.wgLast[st.PublicKey] = core.WGSample{
			PublicKey: st.PublicKey,
			Rx:        st.TransferRx,
			Tx:        st.TransferTx,
		}
	}

	if s.store != nil {
		start := time.Now()
		if len(samples) > 0 {
			if err := s.store.InsertWGSamples(samples); err != nil {
				log.Printf("wg sampler: insert error: %v", err)
				s.store.LogSamplerRun(now, time.Since(start).Milliseconds(), int64(len(samples)), err.Error(), "wireguard")
			} else {
				s.store.LogSamplerRun(now, time.Since(start).Milliseconds(), int64(len(samples)), "", "wireguard")
			}
		} else {
			// Log empty run for visibility
			s.store.LogSamplerRun(now, time.Since(start).Milliseconds(), 0, "", "wireguard")
		}
	}
}

func (s *Server) syncWireGuardConfig(wgConfig *core.WireGuardConfig) bool {
	if !s.config.EnableWireGuard {
		return false
	}
	if _, err := exec.LookPath("wg"); err != nil {
		log.Printf("wg syncconf skipped: wg binary not found (%v)", err)
		return false
	}

	iface := strings.TrimSuffix(filepath.Base(s.config.WireGuardConfigPath), filepath.Ext(s.config.WireGuardConfigPath))
	if iface == "" {
		iface = "wg0"
	}

	syncPath, cleanup, err := s.writeSyncConf(wgConfig)
	if err != nil {
		log.Printf("wg syncconf prepare failed: %v", err)
		return false
	}
	defer cleanup()

	cmd := exec.Command("wg", "syncconf", iface, syncPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("wg syncconf failed (cmd: wg syncconf %s %s): %v - output: %s", iface, syncPath, err, strings.TrimSpace(string(out)))
		return false
	}

	s.clearWireGuardPending()
	return true
}

func (s *Server) writeSyncConf(wgConfig *core.WireGuardConfig) (string, func(), error) {
	if wgConfig == nil {
		cfg, err := core.LoadWireGuardConfig(s.config.WireGuardConfigPath)
		if err != nil {
			return "", func() {}, err
		}
		wgConfig = cfg
	}

	tmpFile, err := os.CreateTemp("", "wg-sync-*.conf")
	if err != nil {
		return "", func() {}, err
	}

	cleanup := func() {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
	}

	var b strings.Builder
	b.WriteString("[Interface]\n")
	if wgConfig.Interface.PrivateKey != "" {
		fmt.Fprintf(&b, "PrivateKey = %s\n", wgConfig.Interface.PrivateKey)
	}
	if wgConfig.Interface.ListenPort != 0 {
		fmt.Fprintf(&b, "ListenPort = %d\n", wgConfig.Interface.ListenPort)
	}
	if wgConfig.Interface.MTU != 0 {
		fmt.Fprintf(&b, "MTU = %d\n", wgConfig.Interface.MTU)
	}
	b.WriteString("\n")

	for _, p := range wgConfig.Peers {
		fmt.Fprintf(&b, "[Peer]\n")
		fmt.Fprintf(&b, "PublicKey = %s\n", p.PublicKey)
		fmt.Fprintf(&b, "AllowedIPs = %s\n", p.AllowedIPs)
		if p.Endpoint != "" {
			fmt.Fprintf(&b, "Endpoint = %s\n", p.Endpoint)
		}
		if p.PresharedKey != "" {
			fmt.Fprintf(&b, "PresharedKey = %s\n", p.PresharedKey)
		}
		fmt.Fprintf(&b, "\n")
	}

	if _, err := tmpFile.WriteString(b.String()); err != nil {
		cleanup()
		return "", func() {}, err
	}
	if err := tmpFile.Sync(); err != nil {
		cleanup()
		return "", func() {}, err
	}

	return tmpFile.Name(), cleanup, nil
}

func (s *Server) storeQRConfig(pubKey, cfg string, ttl time.Duration) {
	if pubKey == "" || cfg == "" {
		return
	}

	s.wgMux.Lock()
	defer s.wgMux.Unlock()
	s.cleanupQRCache()
	s.wgQRCache[pubKey] = qrEntry{
		Config:    cfg,
		ExpiresAt: time.Now().Add(ttl),
	}
}

func (s *Server) fetchQRConfig(pubKey string) (string, bool) {
	s.wgMux.Lock()
	defer s.wgMux.Unlock()
	s.cleanupQRCache()
	if entry, ok := s.wgQRCache[pubKey]; ok {
		if time.Now().Before(entry.ExpiresAt) {
			return entry.Config, true
		}
		delete(s.wgQRCache, pubKey)
	}
	return "", false
}

func (s *Server) hasQRConfig(pubKey string) bool {
	_, ok := s.fetchQRConfig(pubKey)
	return ok
}

func (s *Server) cleanupQRCache() {
	now := time.Now()
	for k, v := range s.wgQRCache {
		if now.After(v.ExpiresAt) {
			delete(s.wgQRCache, k)
		}
	}
}

func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	// Public Login
	mux.HandleFunc("POST /api/login", s.handleLogin)

	// Auth Management
	protected := http.NewServeMux()
	protected.HandleFunc("PUT /api/auth/password", s.secure(s.handleUpdatePassword))
	protected.HandleFunc("PUT /api/auth/username", s.secure(s.handleUpdateUsername))

	protected.HandleFunc("GET /api/users", s.secure(s.handleGetUsers))
	protected.HandleFunc("GET /api/report", s.secure(s.handleGetReport))
	protected.HandleFunc("GET /api/report/summary", s.secure(s.handleGetReportSummary))
	protected.HandleFunc("GET /api/logs", s.secure(s.handleGetLogs))
	protected.HandleFunc("GET /api/logs/search", s.secure(s.handleSearchLogs))
	protected.HandleFunc("POST /api/users", s.secure(s.handleCreateUser))
	protected.HandleFunc("PUT /api/users", s.secure(s.handleUpdateUser))
	protected.HandleFunc("DELETE /api/users", s.secure(s.handleDeleteUser))
	protected.HandleFunc("POST /api/users/bulk", s.secure(s.handleBulkCreateUsers))

	protected.HandleFunc("GET /api/wireguard/peers", s.secure(s.handleGetWireGuardPeers))
	protected.HandleFunc("POST /api/wireguard/peers", s.secure(s.handleCreateWireGuardPeer))
	protected.HandleFunc("DELETE /api/wireguard/peers", s.secure(s.handleDeleteWireGuardPeer))
	protected.HandleFunc("GET /api/wireguard/interface", s.secure(s.handleGetWireGuardInterface))
	protected.HandleFunc("PUT /api/wireguard/interface", s.secure(s.handleUpdateWireGuardInterface))
	protected.HandleFunc("PUT /api/wireguard/peer", s.secure(s.handleUpdateWireGuardPeer))
	protected.HandleFunc("GET /api/wireguard/peer/config", s.secure(s.handleGetWireGuardPeerConfig))

	protected.HandleFunc("POST /api/service/restart", s.secure(s.handleRestartService))
	protected.HandleFunc("POST /api/service/start", s.secure(s.handleStartService))
	protected.HandleFunc("POST /api/service/stop", s.secure(s.handleStopService))

	protected.HandleFunc("GET /api/settings/features", s.secure(s.handleGetFeatures))
	protected.HandleFunc("PUT /api/settings/features", s.secure(s.handleUpdateFeatures))
	protected.HandleFunc("POST /api/sampler/run", s.secure(s.handleRunSampler))
	protected.HandleFunc("GET /api/sampler/history", s.secure(s.handleSamplerHistory))
	protected.HandleFunc("POST /api/sampler/pause", s.secure(s.handlePauseSampler))
	protected.HandleFunc("POST /api/sampler/resume", s.secure(s.handleResumeSampler))
	protected.HandleFunc("POST /api/retention/prune", s.secure(s.handlePruneNow))
	protected.HandleFunc("POST /api/config/backup", s.secure(s.handleBackupConfig))
	protected.HandleFunc("POST /api/config/restore", s.secure(s.handleRestoreConfig))
	protected.HandleFunc("GET /api/config/backup/meta", s.secure(s.handleGetBackupMeta))
	protected.HandleFunc("POST /api/wireguard/config/backup", s.secure(s.handleBackupWireGuardConfig))
	protected.HandleFunc("POST /api/wireguard/config/restore", s.secure(s.handleRestoreWireGuardConfig))
	protected.HandleFunc("GET /api/wireguard/traffic", s.secure(s.handleGetWireGuardTraffic))
	protected.HandleFunc("GET /api/wireguard/traffic/series", s.secure(s.handleGetWireGuardTrafficSeries))

	protected.HandleFunc("GET /api/config", s.secure(s.handleGetConfig))
	protected.HandleFunc("PUT /api/config", s.secure(s.handleUpdateConfig))
	protected.HandleFunc("GET /api/wireguard/config", s.secure(s.handleGetWireGuardConfig))
	protected.HandleFunc("PUT /api/wireguard/config", s.secure(s.handleUpdateWireGuardConfig))

	protected.HandleFunc("GET /api/stats", s.secure(s.handleGetStats))
	protected.HandleFunc("GET /api/status", s.secure(s.handleGetSystemStatus))

	// Mount protected routes under /api/
	mux.Handle("/api/", s.AuthMiddleware(protected))

	return mux
}

func StartServer(cfg *core.Config) {
	cfg.LogSource = detectLogSource(cfg)

	store, err := core.NewStore(cfg.DatabasePath)
	if err != nil {
		panic("StartServer: failed to open database: " + err.Error())
	}

	if err := store.EnsureDefaultAdmin(); err != nil {
		log.Printf("StartServer: failed to ensure default admin: %v", err)
	}

	server := NewServer(store, cfg)

	if cfg.EnableSingbox {
		sbClient := core.NewSingboxClient(cfg.SingboxAPIAddr)
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
				deleted, err := store.PruneOlderThan(cutoff)
				if err != nil {
					log.Printf("Retention prune error: %v", err)
				} else if deleted > 0 {
					log.Printf("Retention prune: removed %d samples older than %d", deleted, cutoff)
					vacuumNeeded = true
				}
			}

			// WireGuard Stats Retention
			if cfg.WGRetentionDays > 0 {
				cutoff := time.Now().Add(-time.Duration(cfg.WGRetentionDays) * 24 * time.Hour).Unix()
				deleted, err := store.PruneWGSamplesOlderThan(cutoff)
				if err != nil {
					log.Printf("WG retention prune error: %v", err)
				} else if deleted > 0 {
					log.Printf("WG retention prune: removed %d samples older than %d", deleted, cutoff)
					vacuumNeeded = true
				}
			}

			// Aggregation / Rollup
			if cfg.AggregationEnabled && cfg.AggregationDays > 0 {
				aggCutoff := time.Now().Add(-time.Duration(cfg.AggregationDays) * 24 * time.Hour).Unix()
				compressed, err := store.CompressOldSamples(aggCutoff)
				if err != nil {
					log.Printf("Aggregation compression error: %v", err)
				} else if compressed > 0 {
					log.Printf("Aggregation: compressed %d samples older than %d", compressed, aggCutoff)
					vacuumNeeded = true
				}

				wgCompressed, err := store.CompressOldWGSamples(aggCutoff)
				if err != nil {
					log.Printf("WG Aggregation compression error: %v", err)
				} else if wgCompressed > 0 {
					log.Printf("WG Aggregation: compressed %d samples older than %d", wgCompressed, aggCutoff)
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

	distDir := "./frontend/dist"
	if _, err := os.Stat(distDir); os.IsNotExist(err) {
		if exe, e2 := os.Executable(); e2 == nil {
			distDir = filepath.Join(filepath.Dir(exe), "frontend", "dist")
		}
	}
	log.Printf("Serving static files from %s", distDir)
	fs := http.FileServer(http.Dir(distDir))
	router.Handle("/assets/", http.StripPrefix("/", fs))
	router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join(distDir, "index.html"))
	})

	if err := http.ListenAndServe(cfg.ListenAddr, router); err != nil {
		panic("HTTP server error: " + err.Error())
	}
}

type UserStatus struct {
	Name        string `json:"name"`
	UUID        string `json:"uuid"`
	Flow        string `json:"flow"`
	Uplink      int64  `json:"uplink"`
	Downlink    int64  `json:"downlink"`
	Total       int64  `json:"total"`
	QuotaLimit  int64  `json:"quota_limit"`
	QuotaPeriod string `json:"quota_period"`
	ResetDay    int    `json:"reset_day"`
	Enabled     bool   `json:"enabled"`
	LastSeen    int64  `json:"last_seen"`
}

func (s *Server) handleGetUsers(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}
	// 1. Load active users from Singbox Config
	activeUsers, err := core.LoadUsersFromSingboxConfig(s.config.SingboxConfigPath, s.config.ManagedInbounds)
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
	for k := range activeMap {
		uniqueNames[k] = true
	}
	for k := range metaMap {
		uniqueNames[k] = true
	}

	result := []UserStatus{}

	for name := range uniqueNames {
		// Default values
		uuid := ""
		flow := ""
		limit := int64(0)
		period := "monthly"
		resetDay := 1
		enabled := false // Default to false, check below

		// If in active list, they are definitely enabled (and have UUID/Flow)
		if u, ok := activeMap[name]; ok {
			uuid = u.UUID
			flow = u.Flow
			enabled = true
		}

		// Overlay metadata if available
		if meta, ok := metaMap[name]; ok {
			limit = meta.QuotaLimit
			period = meta.QuotaPeriod
			resetDay = meta.ResetDay
			// If user is NOT in activeMap, we trust metadata's 'Enabled' flag,
			// but since they are not active, they are effectively disabled.
			// However, to show the correct UI state, if metadata says Enabled=true but they are missing from config,
			// something is wrong. But mostly, we expect:
			// - In Config: Enabled=true
			// - Not in Config: Enabled=false (usually)
			// We'll use the presence in activeMap as the source of truth for "Enabled",
			// UNLESS we want to show "Disabled" users.
			// If not in activeMap, enabled stays false (or we can use meta.Enabled if we want to show intended state).
			// Let's rely on presence in activeMap for the reported 'enabled' status to be safe,
			// OR we can trust meta.Enabled if we want to show "User thinks they are enabled but system broken".
			// For now: "Enabled" means "Is currently in Singbox config".
			// UPDATE: User wants toggle. If I toggle OFF, I remove from config. meta.Enabled = false.
			// If I toggle ON, I add to config. meta.Enabled = true.
			// So relying on activeMap presence is correct for "Is Actually Running".
			// But for UI state, if I manually removed them from config file, UI should show disabled.

			// If I strictly use activeMap, then disabled users show as disabled. Perfect.
			if !enabled && meta.Enabled {
				// Edge case: In DB as enabled, but not in Config.
				// Treat as disabled or error? Let's just treat as disabled.
				enabled = false
			}
		}

		// Stats calculation
		now := time.Now()
		var startOfPeriod time.Time

		if resetDay < 1 {
			resetDay = 1
		}
		if resetDay > 31 {
			resetDay = 31
		}

		if now.Day() < resetDay {
			lastMonth := now.AddDate(0, -1, 0)
			startOfPeriod = time.Date(lastMonth.Year(), lastMonth.Month(), resetDay, 0, 0, 0, 0, now.Location())
		} else {
			startOfPeriod = time.Date(now.Year(), now.Month(), resetDay, 0, 0, 0, 0, now.Location())
		}

		samples, err := s.store.GetSamples(name, startOfPeriod.Unix(), now.Unix())
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
			Name:        name,
			UUID:        uuid,
			Flow:        flow,
			Uplink:      up,
			Downlink:    down,
			Total:       up + down,
			QuotaLimit:  limit,
			QuotaPeriod: period,
			ResetDay:    resetDay,
			Enabled:     enabled,
			LastSeen:    lastSeen,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

type CreateUserRequest struct {
	Name         string `json:"name"`
	OriginalName string `json:"original_name,omitempty"`
	UUID         string `json:"uuid"`
	Flow         string `json:"flow"`
	QuotaLimit   int64  `json:"quota_limit"`
	QuotaPeriod  string `json:"quota_period"`
	ResetDay     int    `json:"reset_day"`
	Enabled      *bool  `json:"enabled,omitempty"`
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

	if req.Name == "" || req.UUID == "" {
		if req.Name == "" {
			http.Error(w, "Name is required", http.StatusBadRequest)
			return
		}
		if req.UUID == "" {
			req.UUID = uuid.NewString()
		}
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	if enabled {
		if err := s.config.AddUser(req.Name, req.UUID, req.Flow); err != nil {
			if errors.Is(err, os.ErrInvalid) {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			http.Error(w, "Failed to add user to config: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	meta := core.UserMetadata{
		Email:       req.Name,
		QuotaLimit:  req.QuotaLimit,
		QuotaPeriod: req.QuotaPeriod,
		ResetDay:    req.ResetDay,
		Enabled:     enabled,
	}
	if err := s.store.SaveUserMetadata(meta); err != nil {
		http.Error(w, "Failed to save metadata: "+err.Error(), http.StatusInternalServerError)
		return
	}

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

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	} else {
		if meta, _ := s.store.GetUserMetadata(originalName); meta != nil {
			enabled = meta.Enabled
		}
	}

	if req.UUID == "" {
		req.UUID = uuid.NewString()
	}

	if enabled {
		if originalName != req.Name {
			s.config.RemoveUser(originalName)
		}
		if err := s.config.UpdateUser(req.Name, req.UUID, req.Flow); err != nil {
			if err := s.config.AddUser(req.Name, req.UUID, req.Flow); err != nil {
				http.Error(w, "Failed to update user in config: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
	} else {
		s.config.RemoveUser(originalName)
		if originalName != req.Name {
			s.config.RemoveUser(req.Name)
		}
	}

	meta := core.UserMetadata{
		Email:       req.Name,
		QuotaLimit:  req.QuotaLimit,
		QuotaPeriod: req.QuotaPeriod,
		ResetDay:    req.ResetDay,
		Enabled:     enabled,
	}
	if err := s.store.SaveUserMetadata(meta); err != nil {
		http.Error(w, "Failed to save metadata: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if originalName != req.Name {
		s.store.DeleteUserMetadata(originalName)
	}

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
		if req.Name == "" || req.UUID == "" {
			continue
		}

		if err := s.config.AddUser(req.Name, req.UUID, req.Flow); err != nil {
			continue
		}

		meta := core.UserMetadata{
			Email:       req.Name,
			QuotaLimit:  req.QuotaLimit,
			QuotaPeriod: req.QuotaPeriod,
			ResetDay:    req.ResetDay,
			Enabled:     true,
		}
		s.store.SaveUserMetadata(meta)
	}

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

	users, err := core.LoadUsersFromSingboxConfig(s.config.SingboxConfigPath, s.config.ManagedInbounds)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	result := []UserStatus{}
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
		result = append(result, UserStatus{
			Name:     user.Name,
			UUID:     user.UUID,
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

	users, err := core.LoadUsersFromSingboxConfig(s.config.SingboxConfigPath, s.config.ManagedInbounds)
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
	content, err := os.ReadFile(s.config.SingboxConfigPath)
	if err != nil {
		http.Error(w, "Failed to read config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(content)
}

func (s *Server) handleGetLogs(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}
	filterUser := strings.TrimSpace(r.URL.Query().Get("user"))
	var lines []string
	var err error
	if s.config.LogSource == "journal" || s.config.AccessLogPath == "" {
		lines, err = readJournalLines("sing-box", 200)
	} else {
		lines, err = tailFileLines(s.config.AccessLogPath, 256*1024, 200)
		if err != nil && s.config.LogSource == "file" {
			// Fallback to journal if file missing or unreadable
			if linesJ, jErr := readJournalLines("sing-box", 200); jErr == nil {
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
		lines, err = searchJournalLines("sing-box", q, effectiveLimit)
	} else {
		lines, err = searchFileLines(s.config.AccessLogPath, q, effectiveLimit)
		if (err != nil || len(lines) == 0) && s.config.LogSource == "file" {
			// Fallback to journal if file missing/unreadable or no matches
			if linesJ, jErr := searchJournalLines("sing-box", q, effectiveLimit); jErr == nil {
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
