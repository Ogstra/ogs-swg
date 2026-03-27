package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
)

// DashboardData Structs
type DashboardData struct {
	Status                map[string]interface{}  `json:"status"`
	StatsCards            map[string]TrafficStats `json:"stats_cards"`
	WireGuardInterfaces   map[string]TrafficStats `json:"wireguard_interfaces,omitempty"`
	ChartData             []UnifiedChartPoint     `json:"chart_data"`
	TopConsumers          map[string][]Consumer   `json:"top_consumers"`
	SingboxPendingChanges bool                    `json:"singbox_pending_changes"`
	PublicIP              string                  `json:"public_ip"`
}

type TrafficStats struct {
	Uplink   int64 `json:"uplink"`
	Downlink int64 `json:"downlink"`
}

type UnifiedChartPoint struct {
	Timestamp int64 `json:"ts"`
	UpSB      int64 `json:"up_sb"`
	DownSB    int64 `json:"down_sb"`
	UpWG      int64 `json:"up_wg"`
	DownWG    int64 `json:"down_wg"`
}

type Consumer struct {
	Name       string `json:"name"`
	Total      int64  `json:"total"`
	Flow       string `json:"flow"`
	Interface  string `json:"interface_name,omitempty"`
	QuotaLimit int64  `json:"quota_limit"` // 0 if none
	Key        string `json:"key"`         // For linking/identification
}

type DashboardConsumerChartData struct {
	ChartData []UnifiedChartPoint `json:"chart_data"`
}

func resolveConsumerKey(rawKey, mode, name, iface string, s *Server, r *http.Request) string {
	key := strings.TrimSpace(rawKey)
	if key != "" && key != maskedValue {
		return key
	}

	displayName := strings.TrimSpace(name)
	if displayName == "" {
		return ""
	}

	switch mode {
	case "singbox":
		return displayName
	case "wireguard":
		_, aliases, interfaces := s.discoverWireGuardPeersByInterface(r.Context())
		targetIface := strings.TrimSpace(iface)
		matches := make([]string, 0, 1)
		for candidateKey, alias := range aliases {
			if targetIface != "" && interfaces[candidateKey] != targetIface {
				continue
			}
			trimmedAlias := strings.TrimSpace(alias)
			if trimmedAlias == displayName || (trimmedAlias == "" && strings.HasPrefix(candidateKey, displayName)) {
				matches = append(matches, candidateKey)
			}
		}
		if len(matches) == 1 {
			return matches[0]
		}
	}

	return ""
}

func wireGuardFlowLabel(interfaceName string) string {
	iface := strings.TrimSpace(interfaceName)
	if iface == "" {
		return "WireGuard"
	}
	return fmt.Sprintf("WireGuard:%s", iface)
}

func resolveDashboardWindow(rangeStr, startStr, endStr string) (int64, int64, int64) {
	var start, end int64
	now := time.Now().Unix()

	if startStr != "" && endStr != "" {
		sVal, _ := strconv.ParseInt(startStr, 10, 64)
		eVal, _ := strconv.ParseInt(endStr, 10, 64)
		start = sVal
		end = eVal
	}

	if start == 0 || end == 0 {
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
		default:
			duration = 24 * time.Hour
		}

		var baseRes int64 = 60
		if duration >= 6*time.Hour && duration <= 24*time.Hour {
			baseRes = 300
		} else if duration > 24*time.Hour {
			baseRes = 3600
		}

		quantizedEnd := (now / baseRes) * baseRes
		end = quantizedEnd
		start = end - int64(duration.Seconds())
	}

	diff := end - start
	var interval int64
	if diff <= 1800 {
		interval = 60
	} else if diff <= 3600 {
		interval = 120
	} else if diff <= 21600 {
		interval = 900
	} else if diff <= 86400 {
		interval = 3600
	} else if diff <= 604800 {
		interval = 21600
	} else {
		interval = 86400
	}

	return start, end, interval
}

func buildConsumerChartData(start, end, interval int64, buckets map[int64]TrafficStats, mode string) []UnifiedChartPoint {
	var chartData []UnifiedChartPoint
	var accUp, accDown int64
	gridStart := (start / interval) * interval

	for t := gridStart; t <= end; t += interval {
		stat := buckets[t]
		accUp += stat.Uplink
		accDown += stat.Downlink

		point := UnifiedChartPoint{Timestamp: t}
		if mode == "wireguard" {
			point.UpWG = accUp
			point.DownWG = accDown
		} else {
			point.UpSB = accUp
			point.DownSB = accDown
		}
		chartData = append(chartData, point)
	}

	return chartData
}

func apiTrafficBucketsFromCore(in map[int64]core.TrafficStats) map[int64]TrafficStats {
	out := make(map[int64]TrafficStats, len(in))
	for ts, stat := range in {
		out[ts] = TrafficStats{Uplink: stat.Uplink, Downlink: stat.Downlink}
	}
	return out
}

func sumCoreTrafficMap(stats map[int64]core.TrafficStats) TrafficStats {
	var out TrafficStats
	for _, st := range stats {
		out.Uplink += st.Uplink
		out.Downlink += st.Downlink
	}
	return out
}

func (s *Server) discoverWireGuardPeersByInterface(ctx context.Context) (map[string][]string, map[string]string, map[string]string) {
	byInterface := make(map[string][]string)
	aliasByKey := make(map[string]string)
	interfaceByKey := make(map[string]string)
	seenGlobal := make(map[string]struct{})

	var registry core.WireGuardRegistry
	ifaces, err := registry.DiscoverInterfaces(s.wireGuardConfigDir())
	if err != nil || len(ifaces) == 0 {
		ifaces = []string{defaultWireGuardInterfaceName(s.config.WireGuardConfigPath)}
	}
	sort.Strings(ifaces)

	for _, iface := range ifaces {
		cfg, err := s.loadWireGuardConfigForIface(ctx, iface)
		if err != nil || cfg == nil {
			continue
		}

		seenIface := make(map[string]struct{})
		for _, p := range cfg.Peers {
			key := strings.TrimSpace(p.PublicKey)
			if key == "" {
				continue
			}
			if _, ok := seenIface[key]; !ok {
				byInterface[iface] = append(byInterface[iface], key)
				seenIface[key] = struct{}{}
			}
			if _, ok := seenGlobal[key]; !ok {
				interfaceByKey[key] = iface
				seenGlobal[key] = struct{}{}
			}

			alias := strings.TrimSpace(p.Alias)
			if alias == "" {
				alias = strings.TrimSpace(p.Email)
			}
			if alias != "" {
				aliasByKey[key] = alias
			}
		}
	}

	return byInterface, aliasByKey, interfaceByKey
}

func (s *Server) handleGetDashboardData(w http.ResponseWriter, r *http.Request) {
	// Parse range
	rangeStr := r.URL.Query().Get("range")
	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")

	start, end, interval := resolveDashboardWindow(rangeStr, startStr, endStr)

	// Cache key
	// For presets we key by (rangeStr, quantized end), so multiple requests in the same
	// time window reuse the same cached payload even if "now" changes by a few seconds.
	// For custom ranges we key by exact start/end so different manual selections
	// are cached independently.
	var cacheKey string
	if startStr != "" && endStr != "" {
		cacheKey = "custom:" + strconv.FormatInt(start, 10) + ":" + strconv.FormatInt(end, 10)
	} else {
		if rangeStr == "" {
			rangeStr = "24h"
		}
		cacheKey = "range:" + rangeStr + ":" + strconv.FormatInt(end, 10)
	}

	if cachedPayload, found := s.cache.Get(cacheKey); found {
		if payload, ok := cachedPayload.(DashboardData); ok {
			payload.SingboxPendingChanges = s.config.GetSingboxPendingChanges()
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(payload)
			return
		}
	}

	// 1. Fetch System Status
	status := s.collectSystemStatus(r.Context())

	// 2. Fetch WireGuard peers for range calculations
	var wgPeerKeys []string
	wgAliases := make(map[string]string)
	wgInterfaceByKey := make(map[string]string)
	wgKeysByInterface := make(map[string][]string)
	if s.config.EnableWireGuard {
		wgKeysByInterface, wgAliases, wgInterfaceByKey = s.discoverWireGuardPeersByInterface(r.Context())
		seen := make(map[string]struct{})
		ifaces := make([]string, 0, len(wgKeysByInterface))
		for iface := range wgKeysByInterface {
			ifaces = append(ifaces, iface)
		}
		sort.Strings(ifaces)
		for _, iface := range ifaces {
			for _, key := range wgKeysByInterface[iface] {
				if _, ok := seen[key]; ok {
					continue
				}
				seen[key] = struct{}{}
				wgPeerKeys = append(wgPeerKeys, key)
			}
		}
	}

	// 4. Fetch buckets
	sbBuckets := make(map[int64]TrafficStats)
	wgBuckets := make(map[int64]TrafficStats)
	wgInterfaceStats := make(map[string]TrafficStats)

	if s.config.EnableSingbox {
		if buckets, err := s.store.GetSBTrafficBuckets(start, end, interval); err == nil {
			for ts, stat := range buckets {
				sbBuckets[ts] = TrafficStats{Uplink: stat.Uplink, Downlink: stat.Downlink}
			}
		}
	}

	// Process WireGuard using DB bucket aggregation (avoids truncation issues on long ranges)
	if s.config.EnableWireGuard && len(wgPeerKeys) > 0 {
		if buckets, err := s.store.GetWGTrafficBuckets(wgPeerKeys, start, end, interval); err == nil {
			for ts, stats := range buckets {
				wgBuckets[ts] = TrafficStats{Uplink: stats.Uplink, Downlink: stats.Downlink}
			}
		}
		ifaces := make([]string, 0, len(wgKeysByInterface))
		for iface := range wgKeysByInterface {
			ifaces = append(ifaces, iface)
		}
		sort.Strings(ifaces)
		for _, iface := range ifaces {
			keys := wgKeysByInterface[iface]
			if len(keys) == 0 {
				continue
			}
			buckets, err := s.store.GetWGTrafficBuckets(keys, start, end, interval)
			if err != nil {
				continue
			}
			wgInterfaceStats[iface] = sumCoreTrafficMap(buckets)
		}
	}

	// Merge Chart Data (Cumulative for Graph)
	var chartData []UnifiedChartPoint
	var accUpSB, accDownSB, accUpWG, accDownWG int64

	// Align start to interval grid to match bucket keys
	gridStart := (start / interval) * interval
	for t := gridStart; t <= end; t += interval {
		sbStat := sbBuckets[t]
		wgStat := wgBuckets[t]

		accUpSB += sbStat.Uplink
		accDownSB += sbStat.Downlink
		accUpWG += wgStat.Uplink
		accDownWG += wgStat.Downlink

		chartData = append(chartData, UnifiedChartPoint{
			Timestamp: t,
			UpSB:      accUpSB,
			DownSB:    accDownSB,
			UpWG:      accUpWG,
			DownWG:    accDownWG,
		})
	}

	// 5. Calculate Top Consumers
	topSB := []Consumer{}
	topWG := []Consumer{}

	topLimit := 20

	wgPeerSet := make(map[string]bool)
	for _, key := range wgPeerKeys {
		wgPeerSet[key] = true
	}

	// WG Top Consumers (delta in selected range) via single query
	if len(wgPeerKeys) > 0 {
		if totals, err := s.store.GetWGTopTotals(start, end, topLimit); err == nil {
			for _, t := range totals {
				if t.Total <= 0 {
					continue
				}
				if !wgPeerSet[t.Key] {
					continue
				}
				name := wgAliases[t.Key]
				if name == "" && len(t.Key) >= 8 {
					name = t.Key[0:8]
				}
				iface := wgInterfaceByKey[t.Key]
				topWG = append(topWG, Consumer{
					Name:       name,
					Key:        t.Key,
					Total:      t.Total,
					Flow:       wireGuardFlowLabel(iface),
					Interface:  iface,
					QuotaLimit: 0,
				})
			}
		}
	}
	sort.Slice(topWG, func(i, j int) bool { return topWG[i].Total > topWG[j].Total })
	if len(topWG) > topLimit {
		topWG = topWG[:topLimit]
	}

	// Singbox Top Consumers
	if s.config.EnableSingbox {
		allMeta, _ := s.store.GetAllUserMetadata()
		metaLookup := make(map[string]core.UserMetadata)
		for _, m := range allMeta {
			metaLookup[m.Email] = m
		}

		if totals, err := s.store.GetSBTopTotals(start, end, topLimit); err == nil {
			for _, t := range totals {
				if t.Total <= 0 {
					continue
				}
				meta, ok := metaLookup[t.Key]
				if !ok {
					continue
				}
				name := meta.Email
				if name == "" {
					name = t.Key
				}
				topSB = append(topSB, Consumer{
					Name:       name,
					Total:      t.Total,
					Flow:       "Proxy",
					QuotaLimit: meta.QuotaLimit,
					Key:        t.Key,
				})
			}
		}
	}
	sort.Slice(topSB, func(i, j int) bool { return topSB[i].Total > topSB[j].Total })
	if len(topSB) > topLimit {
		topSB = topSB[:topLimit]
	}

	if shouldRedactWireGuardReadOnly(r) {
		for i := range topWG {
			topWG[i].Key = maskedValue
		}
	}
	if shouldRedactUsersReadOnly(r) {
		for i := range topSB {
			topSB[i].Key = maskedValue
		}
	}

	var totalSBUplink, totalSBDownlink int64
	var totalWGTx, totalWGRx int64
	// Use the final accumulator values for Singbox totals (matches chart)
	totalSBUplink = accUpSB
	totalSBDownlink = accDownSB
	// Use the final accumulator values for WireGuard totals (matches chart)
	totalWGTx = accUpWG
	totalWGRx = accDownWG

	resp := DashboardData{
		Status: status,
		StatsCards: map[string]TrafficStats{
			"singbox":   {Uplink: totalSBUplink, Downlink: totalSBDownlink},
			"wireguard": {Uplink: totalWGTx, Downlink: totalWGRx},
		},
		WireGuardInterfaces: wgInterfaceStats,
		ChartData:           chartData,
		TopConsumers: map[string][]Consumer{
			"wireguard": topWG,
			"singbox":   topSB,
		},
		SingboxPendingChanges: s.config.GetSingboxPendingChanges(),
		PublicIP:              getPublicIP(s.config),
	}

	// cache response
	// Setting cost to 1 as default and TTL to 15 seconds
	s.cache.SetWithTTL(cacheKey, resp, 1, 15*time.Second)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleGetDashboardConsumerChart(w http.ResponseWriter, r *http.Request) {
	mode := strings.TrimSpace(r.URL.Query().Get("mode"))
	key := strings.TrimSpace(r.URL.Query().Get("key"))
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	iface := strings.TrimSpace(r.URL.Query().Get("interface_name"))
	rangeStr := r.URL.Query().Get("range")
	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")

	key = resolveConsumerKey(key, mode, name, iface, s, r)
	if key == "" || key == maskedValue {
		http.Error(w, "Missing consumer key", http.StatusBadRequest)
		return
	}
	if mode != "singbox" && mode != "wireguard" {
		http.Error(w, "Invalid consumer mode", http.StatusBadRequest)
		return
	}

	start, end, interval := resolveDashboardWindow(rangeStr, startStr, endStr)
	if end <= start {
		http.Error(w, "Invalid dashboard window", http.StatusBadRequest)
		return
	}

	var (
		buckets map[int64]TrafficStats
		err     error
	)

	switch mode {
	case "singbox":
		var coreBuckets map[int64]core.TrafficStats
		coreBuckets, err = s.store.GetSBUserTrafficBuckets(key, start, end, interval)
		buckets = apiTrafficBucketsFromCore(coreBuckets)
	case "wireguard":
		var coreBuckets map[int64]core.TrafficStats
		coreBuckets, err = s.store.GetWGTrafficBuckets([]string{key}, start, end, interval)
		buckets = apiTrafficBucketsFromCore(coreBuckets)
	}
	if err != nil {
		http.Error(w, "Failed to fetch consumer chart: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(DashboardConsumerChartData{
		ChartData: buildConsumerChartData(start, end, interval, buckets, mode),
	})
}

func (s *Server) collectSystemStatus(ctx context.Context) map[string]interface{} {
	// Replicating logic from handleGetSystemStatus
	// Ideally refactor to shared method, but copy-paste is safer for now to avoid breaking legacy endpoint
	singboxStatus := false
	wireguardStatus := false
	activeUsersSB := int64(0)
	activeUsersWG := 0
	// We don't need lists for Dashboard main view, just counts

	var activeUsersSBList []string
	var activeUsersWGList []string

	if s.config.EnableSingbox {
		if s.config.DemoMode {
			singboxStatus = true
		} else {
			singboxStatus = s.checkService(ctx, "sing-box")
		}
		// Fetch active users list (previously we only fetched count)
		// We use the same threshold mechanism
		if users, err := s.store.GetActiveUsersWithThreshold(5*time.Minute, s.config.ActiveThresholdBytes); err == nil {
			activeUsersSBList = users
			activeUsersSB = int64(len(users))
		}
		// Fallback: if threshold-based result is empty, show sessions with any traffic.
		if activeUsersSB == 0 {
			if users, err := s.store.GetActiveUsers(5 * time.Minute); err == nil {
				activeUsersSBList = users
				activeUsersSB = int64(len(users))
			}
		}
		if s.config.DemoMode && activeUsersSB == 0 {
			if users := s.demoActiveSingboxUsers(time.Now()); len(users) > 0 {
				activeUsersSBList = users
				activeUsersSB = int64(len(users))
			}
		}
	}

	if s.config.EnableWireGuard {
		if s.config.DemoMode {
			wireguardStatus = true
		} else {
			wireguardStatus = s.checkService(ctx, "wireguard")
		}
		storedPeers, _ := s.store.GetWGPeerMeta()
		var (
			stats map[string]core.PeerStats
			err   error
		)
		if s.executor != nil {
			stats, err = s.executor.GetWireGuardStats(ctx)
		} else {
			stats, err = core.GetWireGuardStats()
		}
		threshold := time.Now().Add(-3 * time.Minute).Unix()
		if err == nil {
			for _, peer := range stats {
				if peer.LatestHandshake >= threshold {
					activeUsersWG++
					name := ""
					if meta, ok := storedPeers[peer.PublicKey]; ok && meta.Alias != "" {
						name = meta.Alias
					}
					if name == "" {
						if len(peer.PublicKey) >= 8 {
							name = peer.PublicKey[:8]
						} else {
							name = peer.PublicKey
						}
					}
					activeUsersWGList = append(activeUsersWGList, name)
				}
			}
		}
		if s.config.DemoMode {
			preferred := make(map[string]string, len(storedPeers))
			for publicKey, meta := range storedPeers {
				if strings.TrimSpace(meta.Alias) != "" {
					preferred[publicKey] = meta.Alias
				}
			}
			demoList := s.demoActiveWireGuardPeers(threshold, preferred, time.Now())
			if len(demoList) > 0 {
				activeUsersWGList = demoList
				activeUsersWG = len(demoList)
			}
		}
	}

	if s.config.DemoMode {
		// Demo mode intentionally shows both services as running for UX demos.
		singboxStatus = true
		wireguardStatus = true
	}

	return map[string]interface{}{
		"singbox":                     singboxStatus,
		"wireguard":                   wireguardStatus,
		"active_users_singbox":        activeUsersSB,
		"active_users_wireguard":      activeUsersWG,
		"active_users_singbox_list":   activeUsersSBList,
		"active_users_wireguard_list": activeUsersWGList,
		"enable_singbox":              s.config.EnableSingbox,
		"enable_wireguard":            s.config.EnableWireGuard,
	}
}

func getPublicIP(cfg *core.Config) string {
	if cfg.PublicIP != "" {
		return cfg.PublicIP
	}
	return core.DetectPublicIP()
}
