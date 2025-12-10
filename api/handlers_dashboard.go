package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/Ogstra/ogs-swg/core"
)

// DashboardData Structs
type DashboardData struct {
	Status       map[string]interface{}  `json:"status"`
	StatsCards   map[string]TrafficStats `json:"stats_cards"`
	ChartData    []UnifiedChartPoint     `json:"chart_data"`
	TopConsumers map[string][]Consumer   `json:"top_consumers"`
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

	// 1. Fetch System Status
	status := s.collectSystemStatus()

	// 2. Fetch Singbox Stats (Global History)
	sbHistory, _ := s.store.GetGlobalTraffic(start, end)
	if sbHistory == nil {
		sbHistory = []core.TrafficPoint{}
	}

	// 3. Fetch WireGuard Series (All Peers)
	wgSeriesRaw := make(map[string][]core.WGSample)
	if s.config.EnableWireGuard {
		wgCfg, _ := core.LoadWireGuardConfig(s.config.WireGuardConfigPath)
		if wgCfg != nil {
			for _, p := range wgCfg.Peers {
				series, _ := s.store.GetWGTrafficSeries(p.PublicKey, start, end, 5000)
				wgSeriesRaw[p.PublicKey] = series
			}
		}
	}

	// 4. Aggregate Chart Data (Downsampling)
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

	// Buckets
	sbBuckets := make(map[int64]TrafficStats)
	wgBuckets := make(map[int64]TrafficStats)

	// Process Singbox
	for _, p := range sbHistory {
		bucketTs := (p.Timestamp / interval) * interval
		if bucketTs < start || bucketTs >= end {
			continue
		}
		entry := sbBuckets[bucketTs]
		entry.Uplink += p.Uplink
		entry.Downlink += p.Downlink
		sbBuckets[bucketTs] = entry
	}

	// Process WireGuard
	var totalWGRx, totalWGTx int64
	for _, series := range wgSeriesRaw {
		if len(series) < 2 {
			continue
		}
		// Calculate buckets
		for i := 1; i < len(series); i++ {
			prev := series[i-1]
			curr := series[i]
			ts := curr.Timestamp
			if ts < start || ts >= end {
				continue
			}

			bucketTs := (ts / interval) * interval

			dx := curr.Tx - prev.Tx
			dr := curr.Rx - prev.Rx
			if dx < 0 {
				dx = 0
			}
			if dr < 0 {
				dr = 0
			}

			entry := wgBuckets[bucketTs]
			entry.Uplink += dx   // Sent (Tx)
			entry.Downlink += dr // Received (Rx)
			wgBuckets[bucketTs] = entry
		}

		// Calculate Totals for Stats Card (Windowed)
		first := series[0]
		last := series[len(series)-1]
		rx := last.Rx - first.Rx
		tx := last.Tx - first.Tx
		if rx < 0 {
			rx = 0
		}
		if tx < 0 {
			tx = 0
		}
		totalWGRx += rx
		totalWGTx += tx
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

	// WG Top Consumers
	wgAliases := make(map[string]string)
	if s.config.EnableWireGuard {
		wgCfg, _ := core.LoadWireGuardConfig(s.config.WireGuardConfigPath)
		if wgCfg != nil {
			for _, p := range wgCfg.Peers {
				wgAliases[p.PublicKey] = p.Alias
			}
		}
	}

	for pubKey, series := range wgSeriesRaw {
		if len(series) < 1 {
			continue
		}
		first := series[0]
		last := series[len(series)-1]
		rx := last.Rx - first.Rx
		tx := last.Tx - first.Tx
		if rx < 0 {
			rx = 0
		}
		if tx < 0 {
			tx = 0
		}
		total := rx + tx
		if total > 0 {
			name := wgAliases[pubKey]
			if name == "" {
				name = pubKey[0:8]
			}
			topWG = append(topWG, Consumer{
				Name:       name,
				Key:        pubKey,
				Total:      total,
				Flow:       "WireGuard",
				QuotaLimit: 0,
			})
		}
	}
	sort.Slice(topWG, func(i, j int) bool { return topWG[i].Total > topWG[j].Total })
	if len(topWG) > 5 {
		topWG = topWG[:5]
	}

	// Singbox Top Consumers
	if s.config.EnableSingbox {
		// Fetch usage per user for the range
		usageMap, _ := s.store.GetTrafficPerUser(start, end)
		// Fetch all users to map UUID -> Name
		allUsers, _ := s.store.GetUsers()

		userLookup := make(map[string]core.User)
		for _, u := range allUsers {
			userLookup[u.Uuid] = u
		}

		for uuid, stats := range usageMap {
			total := stats.Uplink + stats.Downlink
			if total > 0 {
				user, exists := userLookup[uuid]
				name := uuid
				var limit int64 = 0
				if exists {
					name = user.Username
					limit = user.DataLimit
				}
				topSB = append(topSB, Consumer{
					Name:       name,
					Total:      total,
					Flow:       "Proxy",
					QuotaLimit: limit,
					Key:        uuid,
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
	}

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
