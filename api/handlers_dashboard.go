package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/Ogstra/ogs-swg/core"
)

// DashboardData Structs
type DashboardData struct {
	Status                map[string]interface{}  `json:"status"`
	StatsCards            map[string]TrafficStats `json:"stats_cards"`
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
	QuotaLimit int64  `json:"quota_limit"` // 0 if none
	Key        string `json:"key"`         // For linking/identification
}

// simple in-memory cache for dashboard responses
var dashboardCache = struct {
	mu   sync.Mutex
	data map[string]cachedDashboard
	ttl  time.Duration
}{
	data: make(map[string]cachedDashboard),
	ttl:  15 * time.Second,
}

type cachedDashboard struct {
	expires time.Time
	payload DashboardData
}

func (s *Server) handleGetDashboardData(w http.ResponseWriter, r *http.Request) {
	// Parse range
	rangeStr := r.URL.Query().Get("range")
	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")

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
		end = now
		start = now - int64(duration.Seconds())
	}

	// Cache key
	cacheKey := strconv.FormatInt(start, 10) + ":" + strconv.FormatInt(end, 10)
	dashboardCache.mu.Lock()
	if entry, ok := dashboardCache.data[cacheKey]; ok && time.Now().Before(entry.expires) {
		dashboardCache.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(entry.payload)
		return
	}
	dashboardCache.mu.Unlock()

	// 1. Fetch System Status
	status := s.collectSystemStatus()

	// 2. Fetch WireGuard peers for range calculations
	var wgPeerKeys []string
	wgAliases := make(map[string]string)
	if s.config.EnableWireGuard {
		wgCfg, _ := core.LoadWireGuardConfig(s.config.WireGuardConfigPath)
		if wgCfg != nil {
			wgPeerKeys = make([]string, 0, len(wgCfg.Peers))
			for _, p := range wgCfg.Peers {
				wgPeerKeys = append(wgPeerKeys, p.PublicKey)
				wgAliases[p.PublicKey] = p.Alias
			}
		}
	}

	// 3. Aggregate Chart Data (Downsampling)
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

	// 4. Fetch buckets
	sbBuckets := make(map[int64]TrafficStats)
	wgBuckets := make(map[int64]TrafficStats)

	if s.config.EnableSingbox {
		if buckets, err := s.store.GetSBTrafficBuckets(start, end, interval); err == nil {
			for ts, stat := range buckets {
				sbBuckets[ts] = TrafficStats{Uplink: stat.Uplink, Downlink: stat.Downlink}
			}
		}
	}

	// Process WireGuard using DB bucket aggregation (avoids truncation issues on long ranges)
	var totalWGRx, totalWGTx int64
	if s.config.EnableWireGuard && len(wgPeerKeys) > 0 {
		if buckets, err := s.store.GetWGTrafficBuckets(wgPeerKeys, start, end, interval); err == nil {
			for ts, stats := range buckets {
				wgBuckets[ts] = TrafficStats{Uplink: stats.Uplink, Downlink: stats.Downlink}
			}
		}
		for _, pubKey := range wgPeerKeys {
			rx, tx, _ := s.store.GetWGTrafficDelta(pubKey, start, end)
			totalWGRx += rx
			totalWGTx += tx
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

	// WG Top Consumers (delta in selected range) via single query
	if len(wgPeerKeys) > 0 {
		if totals, err := s.store.GetWGTopTotals(start, end, 5); err == nil {
			for _, t := range totals {
				if t.Total <= 0 {
					continue
				}
				name := wgAliases[t.Key]
				if name == "" && len(t.Key) >= 8 {
					name = t.Key[0:8]
				}
				topWG = append(topWG, Consumer{
					Name:       name,
					Key:        t.Key,
					Total:      t.Total,
					Flow:       "WireGuard",
					QuotaLimit: 0,
				})
			}
		}
	}
	sort.Slice(topWG, func(i, j int) bool { return topWG[i].Total > topWG[j].Total })
	if len(topWG) > 5 {
		topWG = topWG[:5]
	}

	// Singbox Top Consumers
	if s.config.EnableSingbox {
		allUsers, _ := s.store.GetUsers()
		userLookup := make(map[string]core.User)
		for _, u := range allUsers {
			userLookup[u.Uuid] = u
		}

		if totals, err := s.store.GetSBTopTotals(start, end, 5); err == nil {
			for _, t := range totals {
				if t.Total <= 0 {
					continue
				}
				name := t.Key
				var limit int64
				if u, ok := userLookup[t.Key]; ok {
					if u.Username != "" {
						name = u.Username
					}
					limit = u.DataLimit
				}
				topSB = append(topSB, Consumer{
					Name:       name,
					Total:      t.Total,
					Flow:       "Proxy",
					QuotaLimit: limit,
					Key:        t.Key,
				})
			}
		}
	}
	sort.Slice(topSB, func(i, j int) bool { return topSB[i].Total > topSB[j].Total })
	if len(topSB) > 5 {
		topSB = topSB[:5]
	}

	var totalSBUplink, totalSBDownlink int64
	// Use the final accumulator values for Singbox totals (matches chart)
	totalSBUplink = accUpSB
	totalSBDownlink = accDownSB

	resp := DashboardData{
		Status: status,
		StatsCards: map[string]TrafficStats{
			"singbox":   {Uplink: totalSBUplink, Downlink: totalSBDownlink},
			"wireguard": {Uplink: totalWGTx, Downlink: totalWGRx},
		},
		ChartData: chartData,
		TopConsumers: map[string][]Consumer{
			"wireguard": topWG,
			"singbox":   topSB,
		},
		SingboxPendingChanges: s.config.SingboxPendingChanges,
		PublicIP:              getPublicIP(s.config),
	}

	// cache response
	dashboardCache.mu.Lock()
	dashboardCache.data[cacheKey] = cachedDashboard{
		expires: time.Now().Add(dashboardCache.ttl),
		payload: resp,
	}
	dashboardCache.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) collectSystemStatus() map[string]interface{} {
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
		singboxStatus = checkService("sing-box")
		// Fetch active users list (previously we only fetched count)
		// We use the same threshold mechanism
		if users, err := s.store.GetActiveUsersWithThreshold(5*time.Minute, s.config.ActiveThresholdBytes); err == nil {
			activeUsersSBList = users
			activeUsersSB = int64(len(users))
		}
	}

	if s.config.EnableWireGuard {
		wireguardStatus = checkService("wireguard")
		if stats, err := core.GetWireGuardStats(); err == nil {
			threshold := time.Now().Add(-3 * time.Minute).Unix()
			wgCfg, _ := core.LoadWireGuardConfig(s.config.WireGuardConfigPath)
			peerAliases := make(map[string]string)
			if wgCfg != nil {
				for _, p := range wgCfg.Peers {
					peerAliases[p.PublicKey] = p.Alias
				}
			}

			for _, peer := range stats {
				if peer.LatestHandshake >= threshold {
					activeUsersWG++
					name := peerAliases[peer.PublicKey]
					if name == "" {
						name = peer.PublicKey[0:8]
					}
					activeUsersWGList = append(activeUsersWGList, name)
				}
			}
		}
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
