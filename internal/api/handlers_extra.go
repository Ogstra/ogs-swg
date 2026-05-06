package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
	sqlcStore "github.com/Ogstra/ogs-swg/internal/core/store"
)

type ServiceActionRequest struct {
	Service string `json:"service" validate:"required"`
}

var (
	detachedServiceActionDelay   = 150 * time.Millisecond
	detachedServiceActionTimeout = 15 * time.Second
)

func (s *Server) shouldDetachServiceAction(action, service string) bool {
	return service == "sing-box" && (action == "restart" || action == "stop")
}

func (s *Server) writeAcceptedServiceAction(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, `{"status":"accepted"}`)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (s *Server) dispatchDetachedServiceAction(action, service string, run func(context.Context, string) error, afterSuccess func()) {
	time.AfterFunc(detachedServiceActionDelay, func() {
		ctx, cancel := context.WithTimeout(context.Background(), detachedServiceActionTimeout)
		defer cancel()

		if err := run(ctx, service); err != nil {
			log.Printf("service action failed after response: action=%s service=%s err=%v", action, service, err)
			return
		}

		if afterSuccess != nil {
			afterSuccess()
		}
	})
}

func (s *Server) handleRestartService(w http.ResponseWriter, r *http.Request) {
	var req ServiceActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.validate.Struct(req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := validateService(req.Service); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if s.executor == nil {
		http.Error(w, "System executor not initialized", http.StatusInternalServerError)
		return
	}

	if s.shouldDetachServiceAction("restart", req.Service) {
		var afterSuccess func()
		if req.Service == "sing-box" {
			afterSuccess = func() {
				s.config.ClearSingboxPendingChanges()
				s.InvalidateSubCache()
			}
		}
		s.writeAcceptedServiceAction(w)
		s.dispatchDetachedServiceAction("restart", req.Service, s.executor.RestartService, afterSuccess)
		return
	}

	if err := s.executor.RestartService(r.Context(), req.Service); err != nil {
		http.Error(w, "Failed to restart service: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if req.Service == "wireguard" {
		s.clearWireGuardPending()
	} else if req.Service == "sing-box" {
		s.config.ClearSingboxPendingChanges()
		s.InvalidateSubCache()
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleStartService(w http.ResponseWriter, r *http.Request) {
	var req ServiceActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.validate.Struct(req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := validateService(req.Service); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if s.executor != nil {
		if err := s.executor.StartService(r.Context(), req.Service); err != nil {
			http.Error(w, "Failed to start service: "+err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		http.Error(w, "System executor not initialized", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleStopService(w http.ResponseWriter, r *http.Request) {
	var req ServiceActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.validate.Struct(req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := validateService(req.Service); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if s.executor == nil {
		http.Error(w, "System executor not initialized", http.StatusInternalServerError)
		return
	}

	if s.shouldDetachServiceAction("stop", req.Service) {
		s.writeAcceptedServiceAction(w)
		s.dispatchDetachedServiceAction("stop", req.Service, s.executor.StopService, nil)
		return
	}

	if err := s.executor.StopService(r.Context(), req.Service); err != nil {
		http.Error(w, "Failed to stop service: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}
	content, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	var js interface{}
	if err := json.Unmarshal(content, &js); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.config.UpdateSingboxConfig(string(content)); err != nil {
		http.Error(w, "Failed to update config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleGetStats(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}
	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")
	rangeStr := r.URL.Query().Get("range")

	var start, end int64

	if startStr != "" && endStr != "" {
		if s, err := strconv.ParseInt(startStr, 10, 64); err == nil {
			start = s
		} else if t, err := time.Parse("2006-01-02", startStr); err == nil {
			start = t.Unix()
		}

		if e, err := strconv.ParseInt(endStr, 10, 64); err == nil {
			end = e
		} else if t, err := time.Parse("2006-01-02", endStr); err == nil {
			end = t.Add(24 * time.Hour).Unix()
		}
	} else {
		var duration time.Duration
		switch rangeStr {
		case "30m":
			duration = 30 * time.Minute
		case "1h":
			duration = 1 * time.Hour
		case "6h":
			duration = 6 * time.Hour
		case "24h":
			duration = 24 * time.Hour
		case "1w":
			duration = 7 * 24 * time.Hour
		case "1m":
			duration = 30 * 24 * time.Hour
		case "1y":
			duration = 365 * 24 * time.Hour
		default:
			duration = 24 * time.Hour
		}
		end = time.Now().Unix()
		start = time.Now().Add(-duration).Unix()
	}

	history, err := s.store.GetGlobalTraffic(start, end)
	if err != nil {
		http.Error(w, "Failed to get stats: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if history == nil {
		history = []core.TrafficPoint{}
	}

	// Determine aggregation interval based on range duration
	diff := end - start
	var interval int64
	if diff <= 1800 { // <= 30m
		interval = 60 // 1m
	} else if diff <= 3600 { // <= 1h
		interval = 120 // 2m
	} else if diff <= 21600 { // <= 6h
		interval = 900 // 15m
	} else if diff <= 86400 { // <= 24h
		interval = 3600 // 1h
	} else if diff <= 604800 { // <= 1w
		interval = 21600 // 6h
	} else {
		interval = 86400 // 1d
	}

	// Resample/Bucket the data
	// Create buckets from start to end
	var result []core.TrafficPoint
	inputIdx := 0

	for t := start; t < end; t += interval {
		bucketEnd := t + interval
		var up, down int64

		// Sum up all points within strictly [t, bucketEnd)
		// Assuming history is sorted ASC by GetGlobalTraffic
		for inputIdx < len(history) {
			p := history[inputIdx]
			if p.Timestamp >= bucketEnd {
				break
			}
			if p.Timestamp >= t {
				up += p.Uplink
				down += p.Downlink
			}
			inputIdx++
		}

		result = append(result, core.TrafficPoint{
			Timestamp: t, // Or t + interval/2 for midpoint
			Uplink:    up,
			Downlink:  down,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (s *Server) handleGetSystemStatus(w http.ResponseWriter, r *http.Request) {
	cacheKey := "api:status"
	if cachedPayload, found := s.cache.Get(cacheKey); found {
		if payload, ok := cachedPayload.(map[string]interface{}); ok {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(payload)
			return
		}
	}

	singboxStatus := false
	wireguardStatus := false
	activeUsersSB := int64(0)
	activeUsersList := []string{}
	activeUsersWG := 0
	activeWGList := []string{}
	var sysStats *core.SysStats
	var samplesCount int64
	var dbSizeBytes int64
	samplerPaused := false

	if s.config.EnableSingbox {
		if s.config.DemoMode {
			singboxStatus = true
		} else {
			singboxStatus = s.checkService(r.Context(), "sing-box")
		}
		activeUsersSB, _ = s.store.GetActiveUserCountWithThreshold(5*time.Minute, s.config.ActiveThresholdBytes)
		if lst, err := s.store.GetActiveUsersWithThreshold(5*time.Minute, s.config.ActiveThresholdBytes); err == nil {
			activeUsersList = lst
		}
		// Fallback: if threshold-based result is empty, show sessions with any traffic.
		if activeUsersSB == 0 {
			if lst, err := s.store.GetActiveUsers(5 * time.Minute); err == nil {
				activeUsersList = lst
				activeUsersSB = int64(len(lst))
			}
		}
		if s.config.DemoMode && activeUsersSB == 0 {
			if users := s.demoActiveSingboxUsers(time.Now()); len(users) > 0 {
				activeUsersList = users
				activeUsersSB = int64(len(users))
			}
		}
		if !s.config.DemoMode {
			if xc := core.NewSingboxClient(s.config.SingboxAPIAddr, s.executor); xc != nil {
				if stats, err := xc.GetSysStats(); err == nil {
					sysStats = stats
				}
				xc.Close()
			}
		}
		if s.sampler != nil && s.sampler.IsPaused() {
			samplerPaused = true
		}
	}

	if cnt, err := s.store.CountSamples(); err == nil {
		samplesCount = cnt
	}
	if info, err := os.Stat(s.config.DatabasePath); err == nil {
		dbSizeBytes = info.Size()
	}

	if s.config.EnableWireGuard {
		// For WireGuard, we might want to check interface status too, but service check is a good start
		// If we are in container, we can't easily check wg-quick@wg0 unless via SSH.
		// If local and containerized without systemd, this fails.
		// But s.checkService now uses s.executor.IsServiceActive.
		if s.config.DemoMode {
			wireguardStatus = true
		} else {
			wireguardStatus = s.checkService(r.Context(), "wireguard")
		}
		wgCfg, _ := s.loadWireGuardConfig(r.Context())
		pubToDisplay := make(map[string]string)
		if wgCfg != nil {
			for _, p := range wgCfg.Peers {
				name := p.Alias
				if name == "" {
					name = p.Email
				}
				display := name
				if display == "" {
					display = p.AllowedIPs
				}
				if display == "" {
					display = p.PublicKey
				}
				pubToDisplay[p.PublicKey] = display
			}
		}
		var (
			stats map[string]core.PeerStats
			err   error
		)
		if s.executor != nil {
			stats, err = s.executor.GetWireGuardStats(r.Context())
		} else {
			stats, err = core.GetWireGuardStats()
		}
		threshold := time.Now().Add(-3 * time.Minute).Unix()
		if err == nil {
			for _, peer := range stats {
				if peer.LatestHandshake >= threshold {
					activeUsersWG++
					display := peer.PublicKey
					if v, ok := pubToDisplay[peer.PublicKey]; ok && v != "" {
						display = v
					}
					activeWGList = append(activeWGList, display)
				}
			}
		}
		if s.config.DemoMode {
			demoList := s.demoActiveWireGuardPeers(threshold, pubToDisplay, time.Now())
			if len(demoList) > 0 {
				activeWGList = demoList
				activeUsersWG = len(demoList)
			}
		}
	}

	if s.config.DemoMode {
		// Demo mode intentionally shows both services as running for UX demos.
		singboxStatus = true
		wireguardStatus = true
	}

	status := map[string]interface{}{
		"singbox":                     singboxStatus,
		"wireguard":                   wireguardStatus,
		"wireguard_pending_restart":   s.wgPendingRestart,
		"wg_sample_interval_sec":      s.config.WGSamplerIntervalSec,
		"active_users_singbox":        activeUsersSB,
		"active_users_wireguard":      activeUsersWG,
		"active_users_singbox_list":   activeUsersList,
		"active_users_wireguard_list": activeWGList,
		"enable_singbox":              s.config.EnableSingbox,
		"enable_wireguard":            s.config.EnableWireGuard,
		"singbox_sys_stats":           sysStats,
		"samples_count":               samplesCount,
		"db_size_bytes":               dbSizeBytes,
		"sampler_paused":              samplerPaused,
		"systemctl_available":         s.executor != nil,
		"journalctl_available":        s.executor != nil,
	}

	// Cost of 1, TTL of 15 seconds
	s.cache.SetWithTTL(cacheKey, status, 1, 15*time.Second)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func (s *Server) checkService(ctx context.Context, service string) bool {
	if s.executor == nil {
		return false
	}

	// Detach service checks from request cancellation so transient client/network
	// aborts don't force false "stopped" statuses.
	checkCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	active, err := s.executor.IsServiceActive(checkCtx, service)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			log.Printf("checkService: timeout/canceled checking %s: %v", service, err)
			return false
		}
		log.Printf("checkService: failed to check %s: %v", service, err)
		return false
	}
	return active
}

func (s *Server) requireAnyService(w http.ResponseWriter) bool {
	if !s.config.EnableSingbox && !s.config.EnableWireGuard {
		http.Error(w, "No services enabled", http.StatusServiceUnavailable)
		return false
	}
	return true
}

func (s *Server) handleRunSampler(w http.ResponseWriter, r *http.Request) {
	if !s.requireAnyService(w) {
		return
	}
	if s.sampler != nil {
		s.sampler.TriggerOnce()
	}
	// Also trigger WireGuard sampler
	if s.config.EnableWireGuard {
		go s.runWireGuardSample()
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handlePauseSampler(w http.ResponseWriter, r *http.Request) {
	if !s.requireAnyService(w) {
		return
	}
	if s.sampler != nil {
		s.sampler.SetPaused(true)
	}
	if s.config.EnableWireGuard {
		s.wgMux.Lock()
		s.wgSamplerPaused = true
		s.wgMux.Unlock()
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleResumeSampler(w http.ResponseWriter, r *http.Request) {
	if !s.requireAnyService(w) {
		return
	}
	if s.sampler != nil {
		s.sampler.SetPaused(false)
	}
	if s.config.EnableWireGuard {
		s.wgMux.Lock()
		s.wgSamplerPaused = false
		s.wgMux.Unlock()
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleSamplerHistory(w http.ResponseWriter, r *http.Request) {
	limit := 5
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil {
			limit = v
		}
	}
	runs, err := s.store.GetSamplerRuns(limit)
	if err != nil {
		http.Error(w, "Failed to read sampler history: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(runs)
}

type subscriptionRequestHistoryPageResponse struct {
	Items      []sqlcStore.GetSubscriptionRequestHistoryRow `json:"items"`
	HasMore    bool                                         `json:"has_more"`
	NextOffset int                                          `json:"next_offset"`
}

func parseBoundedIntQuery(r *http.Request, key string, fallback, minValue, maxValue int) int {
	value := fallback
	if raw := r.URL.Query().Get(key); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			value = parsed
		}
	}
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func (s *Server) handleSubscriptionRequestHistory(w http.ResponseWriter, r *http.Request) {
	limit := parseBoundedIntQuery(r, "limit", 5, 1, 100)
	offset := parseBoundedIntQuery(r, "offset", 0, 0, 1_000_000)
	subID := parseBoundedIntQuery(r, "sub_id", 0, 0, 1_000_000_000)
	censor := shouldCensorSubscriptionRequestHistory(r)
	pageRequested := r.URL.Query().Has("offset")

	page, err := s.getSubscriptionRequestHistoryPage(r.Context(), limit, offset, subID, censor)
	if err != nil {
		http.Error(w, "Failed to read subscription request history: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if page.HasMore {
		s.prefetchSubscriptionRequestHistoryPage(limit, page.NextOffset, subID, censor)
	}

	w.Header().Set("Content-Type", "application/json")
	if pageRequested {
		json.NewEncoder(w).Encode(page)
		return
	}
	json.NewEncoder(w).Encode(page.Items)
}

func (s *Server) getSubscriptionRequestHistoryPage(ctx context.Context, limit, offset, subID int, censor bool) (subscriptionRequestHistoryPageResponse, error) {
	cacheKey := fmt.Sprintf("subscription-request-history:v2:censor=%t:sub=%d:limit=%d:offset=%d", censor, subID, limit, offset)
	if offset > 0 {
		if cached, found := s.cache.Get(cacheKey); found {
			if page, ok := cached.(subscriptionRequestHistoryPageResponse); ok {
				return page, nil
			}
		}
	}

	page, err := s.loadSubscriptionRequestHistoryPage(ctx, limit, offset, subID, censor)
	if err != nil {
		return subscriptionRequestHistoryPageResponse{}, err
	}
	if offset > 0 {
		s.cache.SetWithTTL(cacheKey, page, 1, 30*time.Second)
	}
	return page, nil
}

func (s *Server) prefetchSubscriptionRequestHistoryPage(limit, offset, subID int, censor bool) {
	if offset <= 0 {
		return
	}

	cacheKey := fmt.Sprintf("subscription-request-history:v2:censor=%t:sub=%d:limit=%d:offset=%d", censor, subID, limit, offset)
	if _, found := s.cache.Get(cacheKey); found {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		page, err := s.loadSubscriptionRequestHistoryPage(ctx, limit, offset, subID, censor)
		if err != nil {
			log.Printf("subscription request history: prefetch failed sub_id=%d offset=%d limit=%d: %v", subID, offset, limit, err)
			return
		}
		s.cache.SetWithTTL(cacheKey, page, 1, 30*time.Second)
	}()
}

func (s *Server) loadSubscriptionRequestHistoryPage(ctx context.Context, limit, offset, subID int, censor bool) (subscriptionRequestHistoryPageResponse, error) {
	rows, err := s.store.Queries.GetSubscriptionRequestHistory(ctx, sqlcStore.GetSubscriptionRequestHistoryParams{
		SubID:  int64(subID),
		Limit:  int64(limit + 1),
		Offset: int64(offset),
	})
	if err != nil {
		return subscriptionRequestHistoryPageResponse{}, err
	}

	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	s.prepareSubscriptionRequestHistoryRows(ctx, rows, censor)

	return subscriptionRequestHistoryPageResponse{
		Items:      rows,
		HasMore:    hasMore,
		NextOffset: offset + len(rows),
	}, nil
}

func (s *Server) prepareSubscriptionRequestHistoryRows(ctx context.Context, runs []sqlcStore.GetSubscriptionRequestHistoryRow, censor bool) {
	if s != nil && s.store != nil {
		currentUsers := make(map[int64]string, len(runs))
		for i := range runs {
			runs[i].DeviceModel = resolveSubscriptionDeviceModel(runs[i].DeviceModel)
			if names, ok := currentUsers[runs[i].SubID]; ok {
				runs[i].UserName = names
				continue
			}
			users, usersErr := s.store.Queries.GetUsersForSubscription(ctx, runs[i].SubID)
			if usersErr != nil {
				log.Printf("subscription request history: failed to resolve current users for sub_id=%d: %v", runs[i].SubID, usersErr)
				continue
			}
			if s.config != nil {
				filtered := users[:0]
				for _, user := range users {
					if strings.TrimSpace(user) == "" {
						continue
					}
					if inbounds, cfgErr := s.config.GetUserInbounds(user); cfgErr == nil && len(inbounds) > 0 {
						filtered = append(filtered, user)
					}
				}
				users = filtered
			}
			names := strings.Join(users, ", ")
			currentUsers[runs[i].SubID] = names
			runs[i].UserName = names
		}
	}
	if censor {
		for i := range runs {
			runs[i].UserName = "Restricted"
			if runs[i].RequestIp != "" {
				runs[i].RequestIp = "***"
			}
			runs[i].RequestHost = ""
			runs[i].RequestPath = ""
			runs[i].UserAgent = ""
			runs[i].DeviceModel = ""
			runs[i].DeviceOs = ""
			runs[i].DeviceOsVersion = ""
			runs[i].AppVersion = ""
			runs[i].Country = ""
			runs[i].HwidHash = ""
			runs[i].HwidPrefix = ""
		}
	}
}

func shouldCensorSubscriptionRequestHistory(r *http.Request) bool {
	perms := getPermissions(r)
	if perms == nil {
		return false
	}
	return !perms.CanReadLogs || perms.CanReadLogsCensored
}

func (s *Server) handlePruneNow(w http.ResponseWriter, r *http.Request) {
	// Respect config values primarily, but prioritize retention settings
	days := s.config.RetentionDays
	if days <= 0 {
		days = 90
	}
	wgDays := s.config.WGRetentionDays
	if wgDays <= 0 {
		wgDays = 30
	}

	var payload map[string]int
	if err := json.NewDecoder(r.Body).Decode(&payload); err == nil {
		if v, ok := payload["days"]; ok && v > 0 {
			days = v
		}
	}

	var totalDeleted int64

	// Prune main samples
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour).Unix()
	if err := s.store.PruneOlderThan(cutoff); err == nil {
		totalDeleted += 1 // cannot accurately report anymore
	} else {
		log.Printf("PruneNow: samples prune failed: %v", err)
	}

	if err := s.store.PruneSubscriptionRequestsOlderThan(cutoff); err == nil {
		totalDeleted += 1
	} else {
		log.Printf("PruneNow: subscription requests prune failed: %v", err)
	}

	// Prune WG samples
	if s.config.WGRetentionDays > 0 {
		wgCutoff := time.Now().Add(-time.Duration(wgDays) * 24 * time.Hour).Unix()
		if err := s.store.PruneWGSamplesOlderThan(wgCutoff); err == nil {
			totalDeleted += 1 // Cannot precisely measure anymore
		} else {
			log.Printf("PruneNow: WG prune failed: %v", err)
		}
	}

	// Optimize DB
	if err := s.store.Vacuum(); err != nil {
		log.Printf("PruneNow: Vacuum failed: %v", err)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"deleted": totalDeleted,
		"cutoff":  cutoff,
		"days":    days,
	})
}

func (s *Server) handleGetFeatures(w http.ResponseWriter, r *http.Request) {
	resp := map[string]interface{}{
		"enable_singbox":          s.config.EnableSingbox,
		"enable_wireguard":        s.config.EnableWireGuard,
		"retention_enabled":       s.config.RetentionEnabled,
		"retention_days":          s.config.RetentionDays,
		"wg_retention_days":       s.config.WGRetentionDays,
		"sampler_interval_sec":    s.config.SamplerIntervalSec,
		"wg_sampler_interval_sec": s.config.WGSamplerIntervalSec,
		"sampler_paused":          s.sampler != nil && s.sampler.IsPaused(),
		"active_threshold_bytes":  s.config.ActiveThresholdBytes,
		"aggregation_enabled":     s.config.AggregationEnabled,
		"aggregation_days":        s.config.AggregationDays,
		"log_source":              s.config.LogSource,
		"access_log_path":         s.config.AccessLogPath,
		"systemctl_available":     s.executor != nil,
		"journalctl_available":    s.executor != nil,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleUpdateFeatures(w http.ResponseWriter, r *http.Request) {
	var payload map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	if val, ok := payload["enable_singbox"].(bool); ok {
		s.config.EnableSingbox = val
	}
	if val, ok := payload["enable_wireguard"].(bool); ok {
		s.config.EnableWireGuard = val
	}
	if val, ok := payload["retention_enabled"].(bool); ok {
		s.config.RetentionEnabled = val
	}
	if v, ok := payload["active_threshold_bytes"]; ok {
		switch t := v.(type) {
		case float64:
			s.config.ActiveThresholdBytes = int64(t)
		case int64:
			s.config.ActiveThresholdBytes = t
		case int:
			s.config.ActiveThresholdBytes = int64(t)
		}
		if s.config.ActiveThresholdBytes < 0 {
			s.config.ActiveThresholdBytes = 0
		}
	}
	if v, ok := payload["retention_days"]; ok {
		switch t := v.(type) {
		case float64:
			s.config.RetentionDays = int(t)
		case int:
			s.config.RetentionDays = t
		}
		if s.config.RetentionDays < 1 {
			s.config.RetentionDays = 1
		}
	}
	if v, ok := payload["wg_retention_days"]; ok {
		switch t := v.(type) {
		case float64:
			s.config.WGRetentionDays = int(t)
		case int:
			s.config.WGRetentionDays = t
		}
		if s.config.WGRetentionDays < 1 {
			s.config.WGRetentionDays = 1
		}
	}
	if v, ok := payload["sampler_interval_sec"]; ok {
		switch t := v.(type) {
		case float64:
			s.config.SamplerIntervalSec = int(t)
		case int:
			s.config.SamplerIntervalSec = t
		}
		if s.config.SamplerIntervalSec < 30 {
			s.config.SamplerIntervalSec = 30
		}
	}
	if v, ok := payload["wg_sampler_interval_sec"]; ok {
		switch t := v.(type) {
		case float64:
			s.config.WGSamplerIntervalSec = int(t)
		case int:
			s.config.WGSamplerIntervalSec = t
		}
		if s.config.WGSamplerIntervalSec < 15 {
			s.config.WGSamplerIntervalSec = 15
		}
	}
	if val, ok := payload["aggregation_enabled"].(bool); ok {
		s.config.AggregationEnabled = val
	}
	if v, ok := payload["aggregation_days"]; ok {
		switch t := v.(type) {
		case float64:
			s.config.AggregationDays = int(t)
		case int:
			s.config.AggregationDays = t
		}
		if s.config.AggregationDays < 1 {
			s.config.AggregationDays = 1
		}
	}

	if err := s.config.SaveAppConfig(); err != nil {
		log.Printf("Failed to persist config toggles: %v", err)
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleGetPublicIP(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"public_ip": s.config.PublicIP})
}

func (s *Server) dashboardPreferencesPrincipal(r *http.Request) (string, bool) {
	if username, ok := currentPanelUsername(r); ok {
		return username, true
	}
	if s.config.APIKey != "" && r.Header.Get("X-API-Key") == s.config.APIKey {
		return "__api_key__", true
	}
	return "", false
}

func (s *Server) handleGetDashboardPreferences(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.dashboardPreferencesPrincipal(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	prefs, err := s.store.GetDashboardPreferences(r.Context(), principal)
	if err != nil {
		http.Error(w, "Failed to load dashboard preferences: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(prefs)
}

func (s *Server) handleUpdateDashboardPreferences(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.dashboardPreferencesPrincipal(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	var prefs core.DashboardPreferences
	if err := json.NewDecoder(r.Body).Decode(&prefs); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}
	if err := s.store.UpdateDashboardPreferences(r.Context(), principal, prefs); err != nil {
		http.Error(w, "Failed to save dashboard preferences: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleUpdatePublicIP(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}
	var payload struct {
		PublicIP string `json:"public_ip"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}
	s.config.PublicIP = strings.TrimSpace(payload.PublicIP)
	if err := s.config.SaveAppConfig(); err != nil {
		http.Error(w, "Failed to save public IP: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleGetSubscriptionDomain(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"subscription_domain": s.config.SubscriptionDomain})
}

func (s *Server) handleUpdateSubscriptionDomain(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}
	var payload struct {
		SubscriptionDomain string `json:"subscription_domain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}
	s.config.SubscriptionDomain = strings.TrimSpace(payload.SubscriptionDomain)
	if err := s.config.SaveAppConfig(); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleGetCFWorkerURL(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"cf_worker_url": s.config.CFWorkerURL})
}

func (s *Server) handleUpdateCFWorkerURL(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}
	var payload struct {
		CFWorkerURL string `json:"cf_worker_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}
	raw := strings.TrimSpace(payload.CFWorkerURL)
	// Validate: must be empty or a valid http(s) URL
	if raw != "" {
		if !strings.HasPrefix(raw, "https://") && !strings.HasPrefix(raw, "http://") {
			http.Error(w, "cf_worker_url must be a valid http(s) URL or empty", http.StatusBadRequest)
			return
		}
		raw = strings.TrimRight(raw, "/")
	}
	s.config.CFWorkerURL = raw
	if err := s.config.SaveAppConfig(); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleBackupConfig(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}
	src := s.config.SingboxConfigPath
	if err := s.createConfigBackup(r.Context(), src); err != nil {
		http.Error(w, "Backup failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleRestoreConfig(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}
	src := s.config.SingboxConfigPath + ".bak"
	dst := s.config.SingboxConfigPath
	if err := s.copyConfig(r.Context(), src, dst); err != nil {
		if isNotFoundErr(err) {
			http.Error(w, "Backup not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Restore failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	var content []byte
	var err error
	if s.executor != nil {
		content, err = s.executor.ReadConfig(r.Context(), dst)
	} else {
		content, err = os.ReadFile(dst)
	}
	if err != nil {
		http.Error(w, "Restore succeeded but failed to read restored file: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(content)
}

func (s *Server) handleGetBackupMeta(w http.ResponseWriter, r *http.Request) {
	singboxBak := s.config.SingboxConfigPath + ".bak"
	wgBak := s.config.WireGuardConfigPath + ".bak"

	info := map[string]*time.Time{}
	if s.executor != nil {
		now := time.Now()
		if _, err := s.executor.ReadConfig(r.Context(), singboxBak); err == nil {
			t := now
			info["singbox_last_backup"] = &t
		}
		if _, err := s.executor.ReadConfig(r.Context(), wgBak); err == nil {
			t := now
			info["wireguard_last_backup"] = &t
		}
	} else {
		if st, err := os.Stat(singboxBak); err == nil {
			t := st.ModTime()
			info["singbox_last_backup"] = &t
		}
		if st, err := os.Stat(wgBak); err == nil {
			t := st.ModTime()
			info["wireguard_last_backup"] = &t
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

type configBackupEntry struct {
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

func (s *Server) handleListConfigBackups(w http.ResponseWriter, r *http.Request) {
	backups, err := s.listConfigBackups(s.config.SingboxConfigPath, 10)
	if err != nil {
		http.Error(w, "Failed to list backups: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(backups)
}

func (s *Server) handleGetConfigBackup(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		http.Error(w, "backup name is required", http.StatusBadRequest)
		return
	}
	content, err := s.readNamedBackup(r.Context(), s.config.SingboxConfigPath, name)
	if err != nil {
		if isNotFoundErr(err) {
			http.Error(w, "Backup not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to read backup: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write(content)
}

func backupTimestamp() string {
	return time.Now().UTC().Format("20060102-150405")
}

func timestampedBackupPath(src string) string {
	return filepath.Join(filepath.Dir(src), fmt.Sprintf("%s.%s.bak", filepath.Base(src), backupTimestamp()))
}

func (s *Server) createConfigBackup(ctx context.Context, src string) error {
	if err := s.copyConfig(ctx, src, timestampedBackupPath(src)); err != nil {
		return err
	}
	return s.copyConfig(ctx, src, src+".bak")
}

func (s *Server) listConfigBackups(src string, limit int) ([]configBackupEntry, error) {
	dir := filepath.Dir(src)
	base := filepath.Base(src)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	backups := make([]configBackupEntry, 0, limit)
	type backupFile struct {
		name string
		when time.Time
	}
	files := make([]backupFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == base+".bak" {
			continue
		}
		if !strings.HasPrefix(name, base+".") || !strings.HasSuffix(name, ".bak") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, backupFile{name: name, when: info.ModTime()})
	}

	sort.Slice(files, func(i, j int) bool { return files[i].when.After(files[j].when) })
	if limit > 0 && len(files) > limit {
		files = files[:limit]
	}
	for _, file := range files {
		backups = append(backups, configBackupEntry{
			Name:      file.name,
			CreatedAt: file.when.UTC().Format(time.RFC3339),
		})
	}
	return backups, nil
}

func (s *Server) readNamedBackup(ctx context.Context, src, name string) ([]byte, error) {
	base := filepath.Base(src)
	cleanName := filepath.Base(strings.TrimSpace(name))
	if cleanName == "" || cleanName != name {
		return nil, os.ErrNotExist
	}
	if !strings.HasPrefix(cleanName, base+".") || !strings.HasSuffix(cleanName, ".bak") || cleanName == base+".bak" {
		return nil, os.ErrNotExist
	}
	fullPath := filepath.Join(filepath.Dir(src), cleanName)
	if s.executor != nil {
		return s.executor.ReadConfig(ctx, fullPath)
	}
	return os.ReadFile(fullPath)
}

func (s *Server) copyConfig(ctx context.Context, src, dst string) error {
	if s.executor != nil {
		content, err := s.executor.ReadConfig(ctx, src)
		if err != nil {
			return err
		}
		return s.executor.WriteConfig(ctx, dst, content, 0644)
	}
	return copyFile(src, dst)
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func validateService(service string) error {
	allowed := []string{"sing-box", "wireguard", "cron"}
	for _, s := range allowed {
		if s == service {
			return nil
		}
	}
	return fmt.Errorf("service '%s' is not allowed", service)
}
