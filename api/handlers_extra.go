package api

import (
	"encoding/json"
	"fmt"
	"github.com/Ogstra/ogs-swg/core"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type PeerWithStats struct {
	core.WireGuardPeer
	Stats core.PeerStats `json:"stats"`
}

func (s *Server) handleGetWireGuardPeers(w http.ResponseWriter, r *http.Request) {
	if !s.requireWireGuard(w) {
		return
	}
	wgConfig, err := core.LoadWireGuardConfig(s.config.WireGuardConfigPath)
	if err != nil {
		http.Error(w, "Failed to load WireGuard config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	stats, _ := core.GetWireGuardStats()

	response := make([]PeerWithStats, 0)
	for _, p := range wgConfig.Peers {
		if p.Alias == "" && p.Email != "" {
			p.Alias = p.Email
		}
		ps := PeerWithStats{WireGuardPeer: p}
		if s, ok := stats[p.PublicKey]; ok {
			ps.Stats = s
		}
		response = append(response, ps)
	}

	log.Printf("DEBUG: GetWireGuardPeers called. Response size: %d", len(response))
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("DEBUG: Encode error: %v", err)
	}
}

type CreatePeerRequest struct {
	Alias string `json:"alias"`
	Email string `json:"email,omitempty"`
}

func (s *Server) handleCreateWireGuardPeer(w http.ResponseWriter, r *http.Request) {
	if !s.requireWireGuard(w) {
		return
	}
	var req CreatePeerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Alias == "" {
		req.Alias = req.Email
	}
	if req.Alias == "" {
		http.Error(w, "Alias is required", http.StatusBadRequest)
		return
	}

	wgConfig, err := core.LoadWireGuardConfig(s.config.WireGuardConfigPath)
	if err != nil {
		http.Error(w, "Failed to load WireGuard config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	priv, pub, err := core.GenerateWireGuardKeys()
	if err != nil {
		http.Error(w, "Failed to generate keys: "+err.Error(), http.StatusInternalServerError)
		return
	}

	baseIP := "10.100.0."
	usedIPs := make(map[int]bool)
	for _, p := range wgConfig.Peers {
		parts := strings.Split(p.AllowedIPs, "/")
		if len(parts) > 0 {
			ipParts := strings.Split(parts[0], ".")
			if len(ipParts) == 4 {
				if last, err := strconv.Atoi(ipParts[3]); err == nil {
					usedIPs[last] = true
				}
			}
		}
	}

	nextIP := 2
	for {
		if !usedIPs[nextIP] {
			break
		}
		nextIP++
		if nextIP > 254 {
			http.Error(w, "No IP addresses available", http.StatusInternalServerError)
			return
		}
	}

	peer := core.WireGuardPeer{
		PublicKey:  pub,
		PrivateKey: priv,
		AllowedIPs: fmt.Sprintf("%s%d/32", baseIP, nextIP),
		Alias:      req.Alias,
	}

	if err := wgConfig.AddPeer(peer); err != nil {
		http.Error(w, "Failed to add peer: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.reloadWireGuard()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(peer)
}

func (s *Server) handleDeleteWireGuardPeer(w http.ResponseWriter, r *http.Request) {
	if !s.requireWireGuard(w) {
		return
	}
	pubKey := r.URL.Query().Get("public_key")
	if pubKey == "" {
		http.Error(w, "public_key is required", http.StatusBadRequest)
		return
	}

	wgConfig, err := core.LoadWireGuardConfig(s.config.WireGuardConfigPath)
	if err != nil {
		http.Error(w, "Failed to load WireGuard config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := wgConfig.RemovePeer(pubKey); err != nil {
		http.Error(w, "Failed to remove peer: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.reloadWireGuard()
	w.WriteHeader(http.StatusOK)
}

type ServiceActionRequest struct {
	Service string `json:"service"`
}

func (s *Server) handleRestartService(w http.ResponseWriter, r *http.Request) {
	var req ServiceActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := runSystemCtl("restart", req.Service); err != nil {
		http.Error(w, "Failed to restart service: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleStartService(w http.ResponseWriter, r *http.Request) {
	var req ServiceActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := runSystemCtl("start", req.Service); err != nil {
		http.Error(w, "Failed to start service: "+err.Error(), http.StatusInternalServerError)
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

	if err := runSystemCtl("stop", req.Service); err != nil {
		http.Error(w, "Failed to stop service: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func runSystemCtl(action, service string) error {
	if runtime.GOOS == "windows" {
		fmt.Printf("MOCK: systemctl %s %s\n", action, service)
		return nil
	}

	if !hasSystemctl() {
		return fmt.Errorf("systemctl not available in this environment")
	}

	unitName := service
	if service == "wireguard" {
		unitName = "wg-quick@wg0"
	}

	cmd := exec.Command("systemctl", action, unitName)
	return cmd.Run()
}

func hasSystemctl() bool {
	if runtime.GOOS == "windows" {
		return false
	}
	_, err := exec.LookPath("systemctl")
	return err == nil
}

func hasJournalctl() bool {
	if runtime.GOOS == "windows" {
		return false
	}
	_, err := exec.LookPath("journalctl")
	return err == nil
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

	if err := os.WriteFile(s.config.SingboxConfigPath, content, 0644); err != nil {
		http.Error(w, "Failed to write config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleGetWireGuardConfig(w http.ResponseWriter, r *http.Request) {
	if !s.requireWireGuard(w) {
		return
	}
	content, err := os.ReadFile(s.config.WireGuardConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte(""))
			return
		}
		http.Error(w, "Failed to read config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Write(content)
}

func (s *Server) handleUpdateWireGuardConfig(w http.ResponseWriter, r *http.Request) {
	if !s.requireWireGuard(w) {
		return
	}
	content, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := os.WriteFile(s.config.WireGuardConfigPath, content, 0644); err != nil {
		http.Error(w, "Failed to write config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.reloadWireGuard()
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleGetWireGuardInterface(w http.ResponseWriter, r *http.Request) {
	if !s.requireWireGuard(w) {
		return
	}
	wgConfig, err := core.LoadWireGuardConfig(s.config.WireGuardConfigPath)
	if err != nil {
		http.Error(w, "Failed to load WireGuard config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if wgConfig.Interface.PublicKey == "" && wgConfig.Interface.PrivateKey != "" {
		if pk, err := wgtypes.ParseKey(wgConfig.Interface.PrivateKey); err == nil {
			wgConfig.Interface.PublicKey = pk.PublicKey().String()
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(wgConfig.Interface)
}

func (s *Server) handleUpdateWireGuardInterface(w http.ResponseWriter, r *http.Request) {
	if !s.requireWireGuard(w) {
		return
	}
	var req core.WireGuardInterface
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	wgConfig, err := core.LoadWireGuardConfig(s.config.WireGuardConfigPath)
	if err != nil {
		http.Error(w, "Failed to load WireGuard config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := wgConfig.UpdateInterface(req); err != nil {
		http.Error(w, "Failed to update interface: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.reloadWireGuard()
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleUpdateWireGuardPeer(w http.ResponseWriter, r *http.Request) {
	if !s.requireWireGuard(w) {
		return
	}
	pubKey := r.URL.Query().Get("public_key")
	if pubKey == "" {
		http.Error(w, "public_key is required", http.StatusBadRequest)
		return
	}

	var req core.WireGuardPeer
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	wgConfig, err := core.LoadWireGuardConfig(s.config.WireGuardConfigPath)
	if err != nil {
		http.Error(w, "Failed to load WireGuard config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := wgConfig.UpdatePeer(pubKey, req); err != nil {
		http.Error(w, "Failed to update peer: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.reloadWireGuard()
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

	// Ensure chart updates even if no new inserts: append a zero point at the end of the range when last sample is older than end.
	if end > 0 {
		var lastTs int64
		if n := len(history); n > 0 {
			lastTs = history[n-1].Timestamp
		}
		if lastTs < end {
			history = append(history, core.TrafficPoint{
				Timestamp: end,
				Uplink:    0,
				Downlink:  0,
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(history)
}

func (s *Server) handleGetSystemStatus(w http.ResponseWriter, r *http.Request) {
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
		singboxStatus = checkService("sing-box")
		activeUsersSB, _ = s.store.GetActiveUserCountWithThreshold(5*time.Minute, s.config.ActiveThresholdBytes)
		if lst, err := s.store.GetActiveUsersWithThreshold(5*time.Minute, s.config.ActiveThresholdBytes); err == nil {
			activeUsersList = lst
		}
		if xc := core.NewSingboxClient(s.config.SingboxAPIAddr); xc != nil {
			if stats, err := xc.GetSysStats(); err == nil {
				sysStats = stats
			}
			xc.Close()
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
		wireguardStatus = checkService("wireguard")
		wgCfg, _ := core.LoadWireGuardConfig(s.config.WireGuardConfigPath)
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
		if stats, err := core.GetWireGuardStats(); err == nil {
			threshold := time.Now().Add(-3 * time.Minute).Unix()
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
	}

	status := map[string]interface{}{
		"singbox":                     singboxStatus,
		"wireguard":                   wireguardStatus,
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
		"systemctl_available":         hasSystemctl(),
		"journalctl_available":        hasJournalctl(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func checkService(service string) bool {
	if runtime.GOOS == "windows" {
		return true
	}

	if !hasSystemctl() {
		log.Printf("checkService: cannot verify %s (systemctl not present in container)", service)
		return false
	}

	unitName := service
	if service == "wireguard" {
		unitName = "wg-quick@wg0"
	}

	cmd := exec.Command("systemctl", "is-active", unitName)
	if err := cmd.Run(); err != nil {
		log.Printf("checkService: %s is not active or cannot be checked: %v", unitName, err)
		return false
	}
	return true
}

func (s *Server) handleRunSampler(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}
	if s.sampler == nil {
		http.Error(w, "Sampler not running", http.StatusServiceUnavailable)
		return
	}
	s.sampler.TriggerOnce()
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handlePauseSampler(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}
	if s.sampler == nil {
		http.Error(w, "Sampler not running", http.StatusServiceUnavailable)
		return
	}
	s.sampler.SetPaused(true)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleResumeSampler(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}
	if s.sampler == nil {
		http.Error(w, "Sampler not running", http.StatusServiceUnavailable)
		return
	}
	s.sampler.SetPaused(false)
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

func (s *Server) handlePruneNow(w http.ResponseWriter, r *http.Request) {
	days := s.config.RetentionDays
	if days <= 0 {
		days = 90
	}
	var payload map[string]int
	if err := json.NewDecoder(r.Body).Decode(&payload); err == nil {
		if v, ok := payload["days"]; ok && v > 0 {
			days = v
		}
	}
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour).Unix()
	deleted, err := s.store.PruneOlderThan(cutoff)
	if err != nil {
		http.Error(w, "Prune failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"deleted": deleted,
		"cutoff":  cutoff,
		"days":    days,
	})
}

func (s *Server) handleGetFeatures(w http.ResponseWriter, r *http.Request) {
	resp := map[string]interface{}{
		"enable_singbox":         s.config.EnableSingbox,
		"enable_wireguard":       s.config.EnableWireGuard,
		"retention_enabled":      s.config.RetentionEnabled,
		"retention_days":         s.config.RetentionDays,
		"sampler_interval_sec":   s.config.SamplerIntervalSec,
		"sampler_paused":         s.sampler != nil && s.sampler.IsPaused(),
		"active_threshold_bytes": s.config.ActiveThresholdBytes,
		"log_source":             s.config.LogSource,
		"access_log_path":        s.config.AccessLogPath,
		"systemctl_available":    hasSystemctl(),
		"journalctl_available":   hasJournalctl(),
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

	if err := s.config.SaveAppConfig(); err != nil {
		log.Printf("Failed to persist config toggles: %v", err)
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleBackupConfig(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}
	src := s.config.SingboxConfigPath
	dst := src + ".bak"
	if err := copyFile(src, dst); err != nil {
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
	if _, err := os.Stat(src); err != nil {
		http.Error(w, "Backup not found", http.StatusNotFound)
		return
	}
	dst := s.config.SingboxConfigPath
	if err := copyFile(src, dst); err != nil {
		http.Error(w, "Restore failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	content, _ := os.ReadFile(dst)
	w.Header().Set("Content-Type", "application/json")
	w.Write(content)
}

func (s *Server) handleBackupWireGuardConfig(w http.ResponseWriter, r *http.Request) {
	if !s.requireWireGuard(w) {
		return
	}
	src := s.config.WireGuardConfigPath
	dst := src + ".bak"
	if err := copyFile(src, dst); err != nil {
		http.Error(w, "Backup failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleRestoreWireGuardConfig(w http.ResponseWriter, r *http.Request) {
	if !s.requireWireGuard(w) {
		return
	}
	src := s.config.WireGuardConfigPath + ".bak"
	if _, err := os.Stat(src); err != nil {
		http.Error(w, "Backup not found", http.StatusNotFound)
		return
	}
	dst := s.config.WireGuardConfigPath
	if err := copyFile(src, dst); err != nil {
		http.Error(w, "Restore failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	content, _ := os.ReadFile(dst)
	w.Header().Set("Content-Type", "text/plain")
	w.Write(content)
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
