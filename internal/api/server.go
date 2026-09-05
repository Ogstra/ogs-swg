package api

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
	sqlcStore "github.com/Ogstra/ogs-swg/internal/core/store"
	"github.com/Ogstra/ogs-swg/internal/sys"
	"github.com/alitto/pond"
	"github.com/dgraph-io/ristretto"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
)

const (
	cacheKeyAllUsers         = "api:users:all"
	cacheKeyAllInboundMeta   = "api:inbound-meta:all"
	cacheKeyAllSubscriptions = "api:subscriptions:all"
	cacheKeyAllRouteTags     = "api:route-tags:all"
	cacheKeyHappConfig       = "api:happ-config"
	cacheKeyAllPanelUsers    = "api:panel-users:all"
	cacheKeySamplerHistory   = "api:db:sampler-history"
	cacheKeySubHistory       = "api:db:subscription-history"
	cacheKeyAuditLog         = "api:db:audit-log"
)

type Server struct {
	store                *core.Store
	auditStore           *core.AuditStore
	config               *core.Config
	executor             core.SystemExecutor
	now                  func() time.Time
	sampler              *core.StatsSampler
	pool                 *pond.WorkerPool
	validate             *validator.Validate
	cache                *ristretto.Cache
	samplerHistoryVer    atomic.Int64
	subHistoryVer        atomic.Int64
	auditLogVer          atomic.Int64
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
	// logSearchSem caps the number of concurrent log-search scans so that
	// rapid or parallel search requests cannot saturate all CPU cores.
	logSearchSem chan struct{}
	logStore     *core.LogStore
	logIngester  *core.LogIngester
}

func (s *Server) invalidateSamplerHistoryCache() {
	s.samplerHistoryVer.Add(1)
}

func (s *Server) invalidateSubscriptionHistoryCache() {
	s.subHistoryVer.Add(1)
}

func (s *Server) invalidateAuditLogCache() {
	s.auditLogVer.Add(1)
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
		auditStore:          nil, // initialized by StartServer
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
		// Allow at most 2 concurrent log scans. A buffered channel acts as a
		// counting semaphore: acquire by sending, release by receiving.
		logSearchSem: make(chan struct{}, 2),
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
			writeErr(w, http.StatusUnauthorized, "Unauthorized")
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

// DEPRECATED: granular permission enforcement is disabled.
// The per-permission role system (CanRead*/CanWrite*) is preserved for future
// reimplementation. All authenticated requests are currently treated as fully
// privileged. See: internal/core/store.go PanelUserPermissions.
// TODO(reimplement): restore role-based access control when the permission model
// is redesigned. Re-enable the original body below and remove this stub.
func (s *Server) requirePerm(check func(*core.PanelUserPermissions) bool, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// DEPRECATED stub — permission check skipped, all authenticated users pass.
		h(w, r)
		/* original enforcement (disabled):
		p := getPermissions(r)
		// nil permissions means API-key auth — grant full access for backward compatibility
		if p != nil && !check(p) {
			writeErr(w, http.StatusForbidden, "Forbidden")
			return
		}
		h(w, r)
		*/
	}
}

func (s *Server) requireSingbox(w http.ResponseWriter) bool {
	if !s.config.EnableSingbox {
		writeErr(w, http.StatusServiceUnavailable, "sing-box disabled")
		return false
	}
	return true
}

func (s *Server) requireWireGuard(w http.ResponseWriter) bool {
	if !s.config.EnableWireGuard {
		writeErr(w, http.StatusServiceUnavailable, "WireGuard disabled")
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

	// Audit log
	protected.HandleFunc("GET /api/audit-log", s.secure(s.handleGetAuditLog))

	// Auth: any authenticated user can change their own password/username
	protected.HandleFunc("PUT /api/auth/password", s.secure(s.AuditLogger("auth", "update", s.handleUpdatePassword)))
	protected.HandleFunc("PUT /api/auth/username", s.secure(s.requirePerm(canWriteSettings, s.handleUpdateUsername)))

	protected.HandleFunc("GET /api/settings/subscription-protection", s.secure(s.requirePerm(canReadSettings, s.handleGetSubscriptionProtection)))
	protected.HandleFunc("PUT /api/settings/subscription-protection", s.secure(s.requirePerm(canWriteSettings, s.handleUpdateSubscriptionProtection)))
	protected.HandleFunc("GET /api/settings/protection-rules", s.secure(s.requirePerm(canReadSettings, s.handleGetProtectionRules)))
	protected.HandleFunc("POST /api/settings/protection-rules", s.secure(s.requirePerm(canWriteSettings, s.handleCreateProtectionRule)))
	protected.HandleFunc("DELETE /api/settings/protection-rules/{id}", s.secure(s.requirePerm(canWriteSettings, s.AuditLogger("protection", "delete", s.handleDeleteProtectionRule))))
	protected.HandleFunc("GET /api/settings/protection-rules/blocked-log", s.secure(s.requirePerm(canReadSettings, s.handleGetBlockedLog)))

	// VPN Users
	protected.HandleFunc("GET /api/users", s.secure(s.requirePerm(canReadUsers, s.handleGetUsers)))
	protected.HandleFunc("POST /api/users", s.secure(s.requirePerm(canWriteUsers, s.AuditLogger("user", "create", s.handleCreateUser))))
	protected.HandleFunc("PUT /api/users", s.secure(s.requirePerm(canWriteUsers, s.AuditLogger("user", "update", s.handleUpdateUser))))
	protected.HandleFunc("DELETE /api/users", s.secure(s.requirePerm(canWriteUsers, s.AuditLogger("user", "delete", s.handleDeleteUser))))
	protected.HandleFunc("GET /api/users/{name}/inbounds", s.secure(s.requirePerm(canReadUsers, s.handleGetUserInbounds)))
	protected.HandleFunc("GET /api/users/{name}/vless", s.secure(s.requirePerm(canWriteUsers, s.handleGetUserVLESSLink)))
	protected.HandleFunc("GET /api/users/{name}/link", s.secure(s.requirePerm(canWriteUsers, s.handleGetUserLink)))
	protected.HandleFunc("DELETE /api/users/{name}/inbounds/{tag}", s.secure(s.requirePerm(canWriteUsers, s.AuditLogger("user", "update", s.handleRemoveUserFromInbound))))
	protected.HandleFunc("PUT /api/users/{name}/inbounds/{tag}", s.secure(s.requirePerm(canWriteUsers, s.AuditLogger("user", "update", s.handleUpdateUserInInbound))))
	protected.HandleFunc("PUT /api/users/{name}/route-tags", s.secure(s.requirePerm(canWriteUsers, s.AuditLogger("user", "update", s.handleUpdateUserRouteTags))))
	protected.HandleFunc("PUT /api/users/{name}/external-profiles", s.secure(s.requirePerm(canWriteUsers, s.AuditLogger("user", "update", s.handleUpdateUserExternalProfiles))))
	protected.HandleFunc("POST /api/users/bulk", s.secure(s.requirePerm(canWriteUsers, s.AuditLogger("user", "create", s.handleBulkCreateUsers))))
	protected.HandleFunc("GET /api/user-route-tags", s.secure(s.requirePerm(canReadUsers, s.handleGetUserRouteTags)))
	protected.HandleFunc("POST /api/user-route-tags", s.secure(s.requirePerm(canWriteUsers, s.handleCreateUserRouteTag)))
	protected.HandleFunc("PUT /api/user-route-tags/{id}", s.secure(s.requirePerm(canWriteUsers, s.handleUpdateUserRouteTag)))
	protected.HandleFunc("DELETE /api/user-route-tags/{id}", s.secure(s.requirePerm(canWriteUsers, s.handleDeleteUserRouteTag)))
	protected.HandleFunc("GET /api/user-route-tags/compatible-rules", s.secure(s.requirePerm(canReadConfig, s.handleGetCompatibleUserRouteRules)))
	protected.HandleFunc("GET /api/external-profiles", s.secure(s.requirePerm(canReadConfig, s.handleListExternalProfiles)))
	protected.HandleFunc("POST /api/external-profiles", s.secure(s.requirePerm(canWriteConfig, s.AuditLogger("external_profile", "upsert", s.handleUpsertExternalProfile))))
	protected.HandleFunc("DELETE /api/external-profiles/{id}", s.secure(s.requirePerm(canWriteConfig, s.AuditLogger("external_profile", "delete", s.handleDeleteExternalProfile))))
	protected.HandleFunc("GET /api/external-profiles/{id}/link", s.secure(s.requirePerm(canWriteUsers, s.handleGetExternalProfileLink)))

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
	protected.HandleFunc("POST /api/wireguard/interfaces", s.secure(s.requirePerm(canWriteWG, s.AuditLogger("wireguard", "create", s.handleCreateWireGuardInterface))))
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
	protected.HandleFunc("POST /api/wireguard/interfaces/{iface}/config/backup", s.secure(s.requirePerm(canWriteWG, s.AuditLogger("wireguard", "backup", s.handleBackupWireGuardConfigForInterface))))
	protected.HandleFunc("GET /api/wireguard/interfaces/{iface}/config/backups", s.secure(s.requirePerm(canReadWG, s.handleListWireGuardConfigBackupsForInterface)))
	protected.HandleFunc("GET /api/wireguard/interfaces/{iface}/config/backup", s.secure(s.requirePerm(canReadWG, s.handleGetWireGuardConfigBackupForInterface)))
	protected.HandleFunc("GET /api/wireguard/interfaces/{iface}/traffic", s.secure(s.requirePerm(canReadWG, s.handleGetWireGuardTrafficForInterface)))
	protected.HandleFunc("GET /api/wireguard/interfaces/{iface}/traffic/series", s.secure(s.requirePerm(canReadWG, s.handleGetWireGuardTrafficSeriesForInterface)))
	protected.HandleFunc("POST /api/wireguard/interfaces/{iface}/enable", s.secure(s.requirePerm(canWriteWG, s.AuditLogger("wireguard", "enable", s.handleEnableWireGuardInterface))))
	protected.HandleFunc("POST /api/wireguard/interfaces/{iface}/disable", s.secure(s.requirePerm(canWriteWG, s.AuditLogger("wireguard", "disable", s.handleDisableWireGuardInterface))))
	protected.HandleFunc("DELETE /api/wireguard/interfaces/{iface}", s.secure(s.requirePerm(canWriteWG, s.AuditLogger("wireguard", "delete", s.handleDeleteWireGuardInterface))))

	// Config / Sing-box / service control
	protected.HandleFunc("GET /api/config", s.secure(s.requirePerm(canReadConfig, s.handleGetConfig)))
	protected.HandleFunc("PUT /api/config", s.secure(s.requirePerm(canWriteConfig, s.AuditLogger("config", "update", s.handleUpdateConfig))))
	protected.HandleFunc("GET /api/singbox/config", s.secure(s.requirePerm(canReadConfig, s.handleGetSingboxConfig)))
	protected.HandleFunc("PUT /api/singbox/config", s.secure(s.requirePerm(canWriteConfig, s.AuditLogger("singbox", "update", s.handleUpdateSingboxConfig))))
	protected.HandleFunc("GET /api/singbox/dns", s.secure(s.requirePerm(canReadConfig, s.handleGetSingboxDNS)))
	protected.HandleFunc("PUT /api/singbox/dns", s.secure(s.requirePerm(canWriteConfig, s.handleUpdateSingboxDNS)))
	protected.HandleFunc("GET /api/singbox/outbounds", s.secure(s.requirePerm(canReadConfig, s.handleGetSingboxOutbounds)))
	protected.HandleFunc("PUT /api/singbox/outbounds/domain-strategy", s.secure(s.requirePerm(canWriteConfig, s.handleUpdateSingboxOutboundDomainStrategies)))
	protected.HandleFunc("GET /api/singbox/inbounds", s.secure(s.requirePerm(canReadConfig, s.handleGetSingboxInbounds)))
	protected.HandleFunc("POST /api/singbox/inbound", s.secure(s.requirePerm(canWriteConfig, s.AuditLogger("singbox", "create", s.handleAddSingboxInbound))))
	protected.HandleFunc("PUT /api/singbox/inbound", s.secure(s.requirePerm(canWriteConfig, s.AuditLogger("singbox", "update", s.handleUpdateSingboxInbound))))
	protected.HandleFunc("DELETE /api/singbox/inbound", s.secure(s.requirePerm(canWriteConfig, s.AuditLogger("singbox", "delete", s.handleDeleteSingboxInbound))))
	protected.HandleFunc("POST /api/singbox/apply", s.secure(s.requirePerm(canWriteConfig, s.AuditLogger("singbox", "apply", s.handleApplySingboxChanges))))
	protected.HandleFunc("GET /api/singbox/route/rules", s.secure(s.requirePerm(canReadConfig, s.handleGetSingboxRouteRules)))
	protected.HandleFunc("POST /api/singbox/route/rules/upsert", s.secure(s.requirePerm(canWriteConfig, s.handleUpsertSingboxRouteRules)))
	protected.HandleFunc("POST /api/service/restart", s.secure(s.requirePerm(canWriteConfig, s.AuditLogger("system", "restart", s.handleRestartService))))
	protected.HandleFunc("POST /api/service/start", s.secure(s.requirePerm(canWriteConfig, s.AuditLogger("system", "start", s.handleStartService))))
	protected.HandleFunc("POST /api/service/stop", s.secure(s.requirePerm(canWriteConfig, s.AuditLogger("system", "stop", s.handleStopService))))
	protected.HandleFunc("POST /api/config/backup", s.secure(s.requirePerm(canWriteConfig, s.AuditLogger("config", "backup", s.handleBackupConfig))))
	protected.HandleFunc("POST /api/config/restore", s.secure(s.requirePerm(canWriteConfig, s.AuditLogger("config", "restore", s.handleRestoreConfig))))
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
	protected.HandleFunc("GET /api/settings/cf-worker-url", s.secure(s.requirePerm(canReadSettings, s.handleGetCFWorkerURL)))
	protected.HandleFunc("PUT /api/settings/cf-worker-url", s.secure(s.requirePerm(canWriteSettings, s.handleUpdateCFWorkerURL)))
	protected.HandleFunc("POST /api/sampler/run", s.secure(s.requirePerm(canWriteSettings, s.handleRunSampler)))
	protected.HandleFunc("GET /api/sampler/history", s.secure(s.requirePerm(canReadSettings, s.handleSamplerHistory)))
	protected.HandleFunc("GET /api/subscription-requests/history", s.secure(s.requirePerm(canReadSettings, s.handleSubscriptionRequestHistory)))
	protected.HandleFunc("DELETE /api/subscription-requests/{id}", s.secure(s.requirePerm(canWriteSettings, s.AuditLogger("subscription_request", "delete", s.handleDeleteSubscriptionRequest))))
	protected.HandleFunc("DELETE /api/subscription-requests", s.secure(s.requirePerm(canWriteSettings, s.AuditLogger("subscription_request", "delete", s.handleDeleteSubscriptionRequests))))
	protected.HandleFunc("POST /api/sampler/pause", s.secure(s.requirePerm(canWriteSettings, s.handlePauseSampler)))
	protected.HandleFunc("POST /api/sampler/resume", s.secure(s.requirePerm(canWriteSettings, s.handleResumeSampler)))
	protected.HandleFunc("POST /api/retention/prune", s.secure(s.requirePerm(canWriteSettings, s.AuditLogger("retention", "prune", s.handlePruneNow))))
	protected.HandleFunc("GET /api/settings/logs/stats", s.secure(s.requirePerm(canReadSettings, s.handleGetLogStoreStats)))
	protected.HandleFunc("GET /api/settings/backup/download", s.secure(s.requirePerm(canWriteSettings, s.handleDownloadDBBackup)))
	protected.HandleFunc("POST /api/settings/backup/trigger", s.secure(s.requirePerm(canWriteSettings, s.AuditLogger("backup", "manual", s.handleTriggerDBBackup))))

	// Panel user management
	protected.HandleFunc("GET /api/panel-users", s.secure(s.requirePerm(canReadPanelUsers, s.handleGetPanelUsers)))
	protected.HandleFunc("POST /api/panel-users", s.secure(s.requirePerm(canWritePanelUsers, s.AuditLogger("panel_user", "create", s.handleCreatePanelUser))))
	protected.HandleFunc("PUT /api/panel-users/permissions", s.secure(s.requirePerm(canWritePanelUsers, s.AuditLogger("panel_user", "update", s.handleUpdatePanelUserPermissions))))
	protected.HandleFunc("PUT /api/panel-users/username", s.secure(s.requirePerm(canWritePanelUsers, s.handleUpdatePanelUserUsername)))
	protected.HandleFunc("PUT /api/panel-users/password", s.secure(s.requirePerm(canWritePanelUsers, s.AuditLogger("panel_user", "update", s.handleUpdatePanelUserPassword))))
	protected.HandleFunc("DELETE /api/panel-users", s.secure(s.requirePerm(canWritePanelUsers, s.AuditLogger("panel_user", "delete", s.handleDeletePanelUser))))

	// Subscriptions
	protected.HandleFunc("GET /api/subscriptions", s.secure(s.requirePerm(canReadUsers, s.handleGetSubscriptions)))
	protected.HandleFunc("POST /api/subscriptions", s.secure(s.requirePerm(canWriteUsers, s.AuditLogger("subscription", "create", s.handleCreateSubscription))))
	protected.HandleFunc("GET /api/subscriptions/defaults", s.secure(s.requirePerm(canReadUsers, s.handleGetSubscriptionDefaults)))
	protected.HandleFunc("GET /api/subscriptions/default-destinations", s.secure(s.requirePerm(canReadUsers, s.handleGetSubscriptionDefaultDestinations)))
	protected.HandleFunc("PUT /api/subscriptions/defaults", s.secure(s.requirePerm(canWriteUsers, s.handleUpdateSubscriptionDefaults)))
	protected.HandleFunc("GET /api/subscriptions/happ-config", s.secure(s.requirePerm(canReadUsers, s.handleGetSubscriptionHappConfig)))
	protected.HandleFunc("PUT /api/subscriptions/happ-config", s.secure(s.requirePerm(canWriteUsers, s.handleUpdateSubscriptionHappConfig)))
	protected.HandleFunc("POST /api/happ/encrypt-link", s.secure(s.requirePerm(canReadUsers, s.handleEncryptHappLink)))
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

	if s.auditStore != nil {
		s.auditStore.Close()
	}

	if s.logIngester != nil {
		s.logIngester.Stop()
	}
	if s.logStore != nil {
		s.logStore.Close()
	}
}

func StartServer(cfg *core.Config) *Server {
	cfg.ApplyWireGuardTestModeDefaults()

	store, err := core.NewStore(cfg.DatabasePath)
	if err != nil {
		panic("StartServer: failed to open database: " + err.Error())
	}

	if err := store.EnsureDefaultPanelUser(); err != nil {
		panic("StartServer: failed to ensure default panel user: " + err.Error())
	}

	auditStore, err := core.NewAuditStore(core.AuditDBPathFor(cfg.DatabasePath))
	if err != nil {
		panic("StartServer: failed to open audit database: " + err.Error())
	}

	logStore, err := core.NewLogStore(core.LogDBPathFor(cfg.DatabasePath))
	if err != nil {
		panic("StartServer: failed to open log database: " + err.Error())
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
	server.auditStore = auditStore
	server.logStore = logStore

	// Start log ingester whenever sing-box access logging is configured, regardless
	// of sampling mode — this ensures lines reach the hot SQLite tier in all cases.
	if cfg.EnableSingbox && cfg.AccessLogPath != "" && !cfg.DemoMode {
		ingester := core.NewLogIngester(cfg.AccessLogPath, logStore)
		ingester.Start()
		server.logIngester = ingester
	}

	if cfg.EnableSingbox && !cfg.DemoMode {
		sbClient := core.NewSingboxClient(cfg.SingboxAPIAddr, executor)
		if cfg.UseStatsSampler {
			sampler := core.NewStatsSampler(sbClient, store, cfg)
			sampler.Start()
			server.sampler = sampler
		} else {
			inboundTags := cfg.StatsInbounds
			if len(inboundTags) == 0 {
				inboundTags = cfg.ManagedInbounds
			}
			calc := core.NewCalculator(server.logIngester, sbClient, store, inboundTags)
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

				// Audit Log size-based retention
				if cfg.AuditLogMaxMB > 0 {
					maxBytes := int64(cfg.AuditLogMaxMB) * 1024 * 1024
					if server.auditStore != nil && server.auditStore.SizeBytes() > maxBytes {
						server.auditStore.PruneToSize(maxBytes)
						log.Printf("Audit log pruned to %d MB limit", cfg.AuditLogMaxMB)
						server.insertSystemAuditEntry("retention", "auto_prune", "audit.db",
							fmt.Sprintf("max_mb:%d", cfg.AuditLogMaxMB))
					}
				}

				// Log hot tier retention -> cold export
				if server.logStore != nil {
					coldDir := server.logColdDir()
					if err := os.MkdirAll(coldDir, 0755); err == nil {
						mode := cfg.LogRetentionMode
						if mode == "" {
							mode = "size"
						}
						if seg, err := server.logStore.CheckRetention(context.Background(), mode, cfg.LogRetentionMB, cfg.LogRetentionTargetPct, cfg.LogRetentionMaxExportPct, cfg.LogRetentionDays, cfg.LogRetentionUnit, coldDir); err != nil {
							log.Printf("Log retention export error: %v", err)
						} else if seg != nil {
							log.Printf("Log retention: exported %d rows to %s", seg.RowCount, seg.Filename)
							server.insertSystemAuditEntry("backup", "cold_export", seg.Filename,
								fmt.Sprintf("rows:%d KB:%d", seg.RowCount, seg.SizeBytes/1024))
						}
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
		// Scheduled DB backups on a separate configurable ticker.
		if cfg.DBBackupIntervalHours > 0 {
			go func() {
				time.Sleep(5 * time.Minute) // initial delay so startup I/O settles
				runBackup := func() {
					backupDir := cfg.DBBackupPath
					if backupDir == "" {
						backupDir = "data/backups"
					}
					if err := os.MkdirAll(backupDir, 0755); err != nil {
						log.Printf("DB backup: mkdir %s: %v", backupDir, err)
						return
					}
					ctx := context.Background()
					ts := time.Now().Format("2006-01-02_150405")
					var created []string

					mainName := fmt.Sprintf("ogs_%s.tar.gz", ts)
					if err := core.BackupDBToTarGz(ctx, store.DB(), "stats.db", filepath.Join(backupDir, mainName)); err != nil {
						log.Printf("DB backup (main): %v", err)
					} else {
						created = append(created, mainName)
					}

					auditName := fmt.Sprintf("audit_%s.tar.gz", ts)
					if server.auditStore != nil {
						if err := core.BackupDBToTarGz(ctx, server.auditStore.DB(), "audit.db", filepath.Join(backupDir, auditName)); err != nil {
							log.Printf("DB backup (audit): %v", err)
						} else {
							created = append(created, auditName)
						}
					}

					if server.logStore != nil {
						firstMs, lastMs, _ := server.logStore.HotDateRange(ctx)
						var logName string
						if firstMs == 0 && lastMs == 0 {
							logName = fmt.Sprintf("singbox_logs_empty_%s.tar.gz", time.Now().Format("2006-01-02"))
						} else {
							logName = fmt.Sprintf("singbox_logs_%s_%s.tar.gz",
								time.UnixMilli(firstMs).Format("2006-01-02"),
								time.UnixMilli(lastMs).Format("2006-01-02"))
						}
						if err := core.BackupDBToTarGz(ctx, server.logStore.DB(), "singbox_logs.db", filepath.Join(backupDir, logName)); err != nil {
							log.Printf("DB backup (logs): %v", err)
						} else {
							created = append(created, logName)
						}
					}

					if len(created) > 0 {
						server.insertSystemAuditEntry("backup", "auto", created[0],
							fmt.Sprintf("archives:%d", len(created)))
					}
				}
				runBackup()
				ticker := time.NewTicker(time.Duration(cfg.DBBackupIntervalHours) * time.Hour)
				defer ticker.Stop()
				for range ticker.C {
					runBackup()
				}
			}()
		}
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
			writeErr(w, http.StatusInternalServerError, "frontend assets not found")
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
		// Subscription routes are handled by the dedicated GET /s/{token} handler.
		// If a request reaches here with a /s/ prefix it means routing missed (e.g.
		// wrong method, unclean path after a redirect). Return 404 rather than
		// serving index.html, which would cause the SPA to redirect to /login.
		if strings.HasPrefix(r.URL.Path, "/s/") {
			http.NotFound(w, r)
			return
		}
		if distDir == "" {
			writeErr(w, http.StatusInternalServerError, "frontend assets not found")
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
	InboundTags       []string               `json:"inbound_tags"`
	RouteTags         []UserRouteTagStatus   `json:"route_tags"`
	ExternalProfiles  []core.ExternalProfile `json:"external_profiles,omitempty"`
	SubscriptionQuota *SubQuotaInfo          `json:"subscription_quota,omitempty"`
}

func (s *Server) replaceUserInRouteRules(oldName, newName string) error {
	rules, err := s.config.GetSingboxRouteRules()
	if err != nil || len(rules) == 0 {
		return nil
	}
	changed := false
	for _, rule := range rules {
		if raw, ok := rule["auth_user"]; ok {
			if updated, modified := replaceUserInAuthUser(raw, oldName, newName); modified {
				rule["auth_user"] = updated
				changed = true
			}
		}
	}
	if changed {
		return s.config.ReplaceSingboxRouteRules(rules)
	}
	return nil
}

func replaceUserInAuthUser(raw interface{}, oldName, newName string) (interface{}, bool) {
	changed := false
	switch value := raw.(type) {
	case string:
		if value == oldName {
			if newName == "" {
				return []string{}, true
			}
			return newName, true
		}
	case []interface{}:
		newUsers := make([]interface{}, 0, len(value))
		for _, u := range value {
			if str, ok := u.(string); ok && str == oldName {
				if newName != "" {
					newUsers = append(newUsers, newName)
				}
				changed = true
			} else {
				newUsers = append(newUsers, u)
			}
		}
		if changed {
			return newUsers, true
		}
	case []string:
		newUsers := make([]string, 0, len(value))
		for _, u := range value {
			if u == oldName {
				if newName != "" {
					newUsers = append(newUsers, newName)
				}
				changed = true
			} else {
				newUsers = append(newUsers, u)
			}
		}
		if changed {
			return newUsers, true
		}
	}
	return raw, false
}

func (s *Server) handleGetUsers(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}
	if cached, found := s.cache.Get(cacheKeyAllUsers); found {
		if b, ok := cached.([]byte); ok {
			w.Header().Set("Content-Type", "application/json")
			w.Write(b)
			return
		}
	}
	// 1. Load active users from Singbox Config
	activeUsers, err := s.config.GetActiveUsers()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to load users: "+err.Error())
		return
	}

	// 2. Load all metadata (includes disabled users)
	allMeta, err := s.store.GetAllUserMetadata()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to load metadata: "+err.Error())
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
		writeErr(w, http.StatusInternalServerError, "Failed to load route tags: "+err.Error())
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

		// Stats calculation for user table indicators.
		// Traffic (uplink/downlink/total) uses the user's quota period window so the
		// displayed usage matches what the quota enforcement engine sees.
		// lastSeen always uses the calendar month so the activity signal is not affected
		// by quota period changes.
		now := s.now()
		startOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		combinedSamples, err := s.store.GetCombinedReport(name, core.QuotaWindowStart(period, now), now.Unix())
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
		routeTags, err := s.routeTagStatusesForUser(name, routeTagDefinitions, inboundTags)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "Failed to resolve route tags: "+err.Error())
			return
		}

		var externalProfiles []core.ExternalProfile
		if s.store != nil {
			if eps, err := s.store.GetUserExternalProfiles(name); err == nil {
				externalProfiles = eps
			}
		}

		// Subscription quota: check if this user belongs to a subscription with quota_limit > 0.
		var subQuota *SubQuotaInfo
		if subs, subErr := s.store.Queries.GetSubscriptionsForUser(r.Context(), name); subErr == nil {
			for _, sub := range subs {
				if sub.QuotaLimit.Int64 > 0 {
					subUsed, _ := s.store.Queries.GetSubscriptionUsageInRange(r.Context(), sqlcStore.GetSubscriptionUsageInRangeParams{
						SubID: sub.ID,
						Ts:    core.SubscriptionUsageWindowStart(sub.QuotaPeriod.String, now),
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
			ExternalProfiles:  externalProfiles,
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

	b, _ := json.Marshal(result)
	s.cache.SetWithTTL(cacheKeyAllUsers, b, int64(len(b)), 30*time.Second)
	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
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
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Name == "" {
		writeErr(w, http.StatusBadRequest, "Name is required")
		return
	}
	if req.InboundTag == "" {
		writeErr(w, http.StatusBadRequest, "Inbound Tag is required")
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
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			writeErr(w, http.StatusInternalServerError, "Failed to add user to config: "+err.Error())
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
		writeErr(w, http.StatusInternalServerError, "Failed to save metadata: "+err.Error())
		return
	}

	s.cache.Del("api:status")
	s.cache.Del(cacheKeyAllUsers)
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}
	var req CreateUserRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	if req.Name == "" {
		writeErr(w, http.StatusBadRequest, "Name is required")
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
				writeErr(w, http.StatusInternalServerError, "Failed to rename user in config: "+err.Error())
				return
			}
			if err := s.store.RenameUserTrafficIdentity(originalName, req.Name); err != nil {
				if rollbackErr := s.config.RenameUser(req.Name, originalName, req.UUID, req.Flow, req.VmessSecurity, req.VmessAlterID); rollbackErr != nil {
					writeErr(w, http.StatusInternalServerError, "Failed to rename user traffic identity: "+err.Error()+" (rollback failed: "+rollbackErr.Error()+")")
					return
				}
				writeErr(w, http.StatusInternalServerError, "Failed to rename user traffic identity: "+err.Error())
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
								writeErr(w, http.StatusInternalServerError, "Failed to restore user in inbound "+tag+": "+addErr.Error())
								return
							}
						}
					}
				}
			} else {
				// No known inbounds: fall back to UpdateUser across all managed inbounds.
				if err := s.config.UpdateUser(req.Name, req.UUID, req.Flow, req.InboundTag, req.VmessSecurity, req.VmessAlterID); err != nil {
					if err := s.config.AddUser(req.Name, req.UUID, req.Flow, req.InboundTag, req.VmessSecurity, req.VmessAlterID); err != nil {
						writeErr(w, http.StatusInternalServerError, "Failed to update user in config: "+err.Error())
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
				writeErr(w, http.StatusInternalServerError, "Failed to save metadata: "+err.Error()+" (store rollback failed: "+rollbackErr.Error()+")")
				return
			}
			if rollbackErr := s.config.RenameUser(req.Name, originalName, req.UUID, req.Flow, req.VmessSecurity, req.VmessAlterID); rollbackErr != nil {
				writeErr(w, http.StatusInternalServerError, "Failed to save metadata: "+err.Error()+" (config rollback failed: "+rollbackErr.Error()+")")
				return
			}
		}
		writeErr(w, http.StatusInternalServerError, "Failed to save metadata: "+err.Error())
		return
	}
	if originalName != req.Name {
		_ = s.store.DeleteUserMetadata(originalName)
		if err := s.replaceUserInRouteRules(originalName, req.Name); err != nil {
			log.Printf("Failed to rename user in route rules: %v", err)
		}
	}
	shouldReconcileQuota := existingMeta == nil ||
		existingMeta.QuotaLimit != req.QuotaLimit ||
		existingMeta.QuotaPeriod != req.QuotaPeriod ||
		!existingMeta.Enabled
	if shouldReconcileQuota {
		if err := s.store.ReconcileUserQuotaNow(req.Name, s.config); err != nil {
			writeErr(w, http.StatusInternalServerError, "Failed to reconcile user quota: "+err.Error())
			return
		}
	}

	s.cache.Del("api:status")
	s.cache.Del(cacheKeyAllUsers)
	s.InvalidateSubCache()
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" {
		writeErr(w, http.StatusBadRequest, "Name is required")
		return
	}

	if err := s.config.RemoveUser(name); err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to remove user from config: "+err.Error())
		return
	}

	if err := s.replaceUserInRouteRules(name, ""); err != nil {
		log.Printf("Failed to remove user from route rules: %v", err)
	}

	if err := s.store.RemoveUserFromSubscriptions(name); err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to remove user from subscriptions: "+err.Error())
		return
	}

	if err := s.store.DeleteUserMetadata(name); err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to delete metadata: "+err.Error())
		return
	}

	s.cache.Del("api:status")
	s.cache.Del(cacheKeyAllUsers)
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
		writeErr(w, http.StatusBadRequest, "Name and tag are required")
		return
	}

	if err := s.config.RemoveUserFromInbound(name, tag); err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to remove user from inbound: "+err.Error())
		return
	}

	if err := s.removeUserFromSubscriptionsIfUnassigned(name); err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to remove user from subscriptions: "+err.Error())
		return
	}

	s.cache.Del(cacheKeyAllUsers)
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
		writeErr(w, http.StatusBadRequest, "Name and tag are required")
		return
	}

	var req struct {
		UUID          string `json:"uuid"`
		Flow          string `json:"flow"`
		VmessSecurity string `json:"vmess_security,omitempty"`
		VmessAlterID  int    `json:"vmess_alter_id,omitempty"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.UUID == "" {
		writeErr(w, http.StatusBadRequest, "UUID is required")
		return
	}

	if err := s.config.UpdateUserInInbound(name, req.UUID, req.Flow, tag, req.VmessSecurity, req.VmessAlterID); err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to update user in inbound: "+err.Error())
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
		writeErr(w, http.StatusInternalServerError, "Failed to reconcile user quota: "+err.Error())
		return
	}

	s.cache.Del(cacheKeyAllUsers)
	s.InvalidateSubCache()
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleBulkCreateUsers(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}
	var reqs []CreateUserRequest
	if err := decodeJSON(r, &reqs); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
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
	s.cache.Del(cacheKeyAllUsers)
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
		writeErr(w, http.StatusInternalServerError, err.Error())
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

	writeJSON(w, http.StatusOK, result)
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
		writeErr(w, http.StatusInternalServerError, err.Error())
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

	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}
	content, err := s.config.GetSingboxConfig()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to read config: "+err.Error())
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
	if s.logStore == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"logs": []string{}, "max_id": 0})
		return
	}

	limit := 200
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 5000 {
			limit = v
		}
	}

	censored := func() bool {
		p := getPermissions(r)
		return p != nil && p.CanReadLogsCensored
	}()

	qRaw := strings.TrimSpace(r.URL.Query().Get("q"))
	if qRaw == "" {
		qRaw = strings.TrimSpace(r.URL.Query().Get("user"))
	}
	compiledQuery := compileLogQuery(qRaw)

	// When any filter is active, fetch a larger window so substring matches and
	// connection-ID correlation have enough context. Without this, a simple query
	// like "git" would only see the last 200 lines while a complex query like
	// "[user] AND git" would see 5000.
	fetchLimit := limit
	if qRaw != "" {
		fetchLimit = 5000
	}

	var rows []core.LogRow
	var err error
	if afterStr := r.URL.Query().Get("after_id"); afterStr != "" {
		afterID, _ := strconv.ParseInt(afterStr, 10, 64)
		rows, err = s.logStore.PollAfterID(r.Context(), afterID, fetchLimit)
	} else {
		rows, err = s.logStore.TailHot(r.Context(), fetchLimit)
	}
	if errors.Is(err, context.Canceled) {
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "log read failed: "+err.Error())
		return
	}

	lines := make([]string, 0, len(rows))
	var maxID int64
	for _, row := range rows {
		if row.ID > maxID {
			maxID = row.ID
		}
		line := sanitizeLogLine(row.Raw)
		if censored {
			line = core.CensorLine(line)
		}
		lines = append(lines, line)
	}

	if !compiledQuery.isEmpty() {
		lines = filterLogLines(lines, compiledQuery)
	}
	if qRaw != "" && compiledQuery.requiresPostFilter() {
		lines, _ = truncateRecentLogMatches(lines, limit)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"logs":   lines,
		"max_id": maxID,
	})
}

func (s *Server) handleSearchLogs(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeErr(w, http.StatusBadRequest, "q is required")
		return
	}
	timeRange, err := parseLogTimeRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid time range: "+err.Error())
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
		writeJSON(w, http.StatusOK, map[string]interface{}{
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

	writeJSON(w, http.StatusOK, map[string]interface{}{
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
		writeErr(w, http.StatusBadRequest, "q is required")
		return
	}
	timeRange, err := parseLogTimeRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid time range: "+err.Error())
		return
	}
	_, _, effectiveLimit := parseSearchPageParams(r)
	censoredSearch := func() bool {
		p := getPermissions(r)
		return p != nil && p.CanReadLogsCensored
	}()

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming unsupported")
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
