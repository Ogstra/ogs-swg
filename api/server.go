package api

import (
	"encoding/json"
	"errors"
	"github.com/Ogstra/ogs-swg/core"
	"github.com/google/uuid"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Server struct {
	store   *core.Store
	config  *core.Config
	sampler *core.StatsSampler
}

func NewServer(store *core.Store, config *core.Config) *Server {
	return &Server{
		store:   store,
		config:  config,
		sampler: nil,
	}
}

func (s *Server) secure(handler http.HandlerFunc) http.HandlerFunc {
	if s.config.APIKey == "" {
		return handler
	}

	return func(w http.ResponseWriter, r *http.Request) {
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

func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/users", s.secure(s.handleGetUsers))
	mux.HandleFunc("GET /api/report", s.secure(s.handleGetReport))
	mux.HandleFunc("GET /api/report/summary", s.secure(s.handleGetReportSummary))
	mux.HandleFunc("GET /api/logs", s.secure(s.handleGetLogs))
	mux.HandleFunc("GET /api/logs/search", s.secure(s.handleSearchLogs))
	mux.HandleFunc("POST /api/users", s.secure(s.handleCreateUser))
	mux.HandleFunc("PUT /api/users", s.secure(s.handleUpdateUser))
	mux.HandleFunc("DELETE /api/users", s.secure(s.handleDeleteUser))
	mux.HandleFunc("POST /api/users/bulk", s.secure(s.handleBulkCreateUsers))

	mux.HandleFunc("GET /api/wireguard/peers", s.secure(s.handleGetWireGuardPeers))
	mux.HandleFunc("POST /api/wireguard/peers", s.secure(s.handleCreateWireGuardPeer))
	mux.HandleFunc("DELETE /api/wireguard/peers", s.secure(s.handleDeleteWireGuardPeer))
	mux.HandleFunc("GET /api/wireguard/interface", s.secure(s.handleGetWireGuardInterface))
	mux.HandleFunc("PUT /api/wireguard/interface", s.secure(s.handleUpdateWireGuardInterface))
	mux.HandleFunc("PUT /api/wireguard/peer", s.secure(s.handleUpdateWireGuardPeer))

	mux.HandleFunc("POST /api/service/restart", s.secure(s.handleRestartService))
	mux.HandleFunc("POST /api/service/start", s.secure(s.handleStartService))
	mux.HandleFunc("POST /api/service/stop", s.secure(s.handleStopService))

	mux.HandleFunc("GET /api/settings/features", s.secure(s.handleGetFeatures))
	mux.HandleFunc("PUT /api/settings/features", s.secure(s.handleUpdateFeatures))
	mux.HandleFunc("POST /api/sampler/run", s.secure(s.handleRunSampler))
	mux.HandleFunc("GET /api/sampler/history", s.secure(s.handleSamplerHistory))
	mux.HandleFunc("POST /api/sampler/pause", s.secure(s.handlePauseSampler))
	mux.HandleFunc("POST /api/sampler/resume", s.secure(s.handleResumeSampler))
	mux.HandleFunc("POST /api/retention/prune", s.secure(s.handlePruneNow))
	mux.HandleFunc("POST /api/config/backup", s.secure(s.handleBackupConfig))
	mux.HandleFunc("POST /api/config/restore", s.secure(s.handleRestoreConfig))
	mux.HandleFunc("POST /api/wireguard/config/backup", s.secure(s.handleBackupWireGuardConfig))
	mux.HandleFunc("POST /api/wireguard/config/restore", s.secure(s.handleRestoreWireGuardConfig))

	mux.HandleFunc("GET /api/config", s.secure(s.handleGetConfig))
	mux.HandleFunc("PUT /api/config", s.secure(s.handleUpdateConfig))
	mux.HandleFunc("GET /api/wireguard/config", s.secure(s.handleGetWireGuardConfig))
	mux.HandleFunc("PUT /api/wireguard/config", s.secure(s.handleUpdateWireGuardConfig))

	mux.HandleFunc("GET /api/stats", s.secure(s.handleGetStats))
	mux.HandleFunc("GET /api/status", s.secure(s.handleGetSystemStatus))

	return mux
}

func StartServer(cfg *core.Config) {
	cfg.LogSource = detectLogSource(cfg)

	store, err := core.NewStore(cfg.DatabasePath)
	if err != nil {
		panic("StartServer: failed to open database: " + err.Error())
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
			inboundTag := ""
			if len(cfg.ManagedInbounds) > 0 {
				inboundTag = cfg.ManagedInbounds[0]
			}
			calc := core.NewCalculator(watcher, sbClient, store, inboundTag)
			calc.Start()
		}
	} else {
		log.Printf("sing-box disabled via config; skipping watcher/sampler")
	}

	if cfg.RetentionEnabled && cfg.RetentionDays > 0 {
		go func() {
			ticker := time.NewTicker(24 * time.Hour)
			defer ticker.Stop()
			for {
				cutoff := time.Now().Add(-time.Duration(cfg.RetentionDays) * 24 * time.Hour).Unix()
				deleted, err := store.PruneOlderThan(cutoff)
				if err != nil {
					log.Printf("Retention prune error: %v", err)
				} else if deleted > 0 {
					log.Printf("Retention prune: removed %d samples older than %d", deleted, cutoff)
				}
				<-ticker.C
			}
		}()
	}

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
	users, err := core.LoadUsersFromSingboxConfig(s.config.SingboxConfigPath, s.config.ManagedInbounds)
	if err != nil {
		http.Error(w, "Failed to load users: "+err.Error(), http.StatusInternalServerError)
		return
	}

	result := []UserStatus{}

	for _, user := range users {
		meta, _ := s.store.GetUserMetadata(user.Name)
		limit := int64(0)
		period := "monthly"
		resetDay := 1
		enabled := true
		if meta != nil {
			limit = meta.QuotaLimit
			period = meta.QuotaPeriod
			resetDay = meta.ResetDay
			enabled = meta.Enabled
		}

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

		samples, err := s.store.GetSamples(user.Name, startOfPeriod.Unix(), now.Unix())
		if err != nil {
			continue
		}

		var up, down int64
		lastSeen := int64(0)
		for _, smp := range samples {
			up += smp.Uplink
			down += smp.Downlink
			if (smp.Uplink+smp.Downlink) >= s.config.ActiveThresholdBytes && smp.Timestamp > lastSeen {
				lastSeen = smp.Timestamp
			}
		}
		if lastSeen == 0 {
			if ts, err := s.store.GetLastSeenWithThreshold(user.Name, s.config.ActiveThresholdBytes); err == nil && ts > 0 {
				lastSeen = ts
			}
		}

		result = append(result, UserStatus{
			Name:        user.Name,
			UUID:        user.UUID,
			Flow:        user.Flow,
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
		samples, err := s.store.GetSamples(user.Name, start, end)
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
		samples, err := s.store.GetSamples(user.Name, start, end)
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
	limit := 200
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 2000 {
			limit = v
		}
	}

	var lines []string
	var err error
	if s.config.LogSource == "journal" || s.config.AccessLogPath == "" {
		lines, err = searchJournalLines("sing-box", q, limit)
	} else {
		lines, err = searchFileLines(s.config.AccessLogPath, q, limit)
		if (err != nil || len(lines) == 0) && s.config.LogSource == "file" {
			// Fallback to journal if file missing/unreadable or no matches
			if linesJ, jErr := searchJournalLines("sing-box", q, limit); jErr == nil {
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
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"logs": lines,
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
