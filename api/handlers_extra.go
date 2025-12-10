package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Ogstra/ogs-swg/core"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type PeerWithStats struct {
	core.WireGuardPeer
	Stats       core.PeerStats `json:"stats"`
	QRAvailable bool           `json:"qr_available"`
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
		ps := PeerWithStats{
			WireGuardPeer: p,
			QRAvailable:   s.hasQRConfig(p.PublicKey),
		}
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
	Alias    string `json:"alias"`
	Email    string `json:"email,omitempty"`
	IP       string `json:"ip"`
	Endpoint string `json:"endpoint,omitempty"`
	Private  string `json:"private_key,omitempty"`
}

func normalizeAllowedIPs(raw string) ([]string, string, error) {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	var primary string

	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		// If no mask provided, default to /32 for IPv4 or /128 for IPv6
		if !strings.Contains(p, "/") {
			if ip := net.ParseIP(p); ip != nil {
				if ip.To4() != nil {
					p = fmt.Sprintf("%s/32", ip.String())
				} else {
					p = fmt.Sprintf("%s/128", ip.String())
				}
			} else {
				return nil, "", fmt.Errorf("invalid IP: %s", p)
			}
		}

		_, ipNet, err := net.ParseCIDR(p)
		if err != nil || ipNet == nil {
			return nil, "", fmt.Errorf("invalid CIDR: %s", p)
		}

		out = append(out, ipNet.String())
		if primary == "" {
			primary = ipNet.String()
		}
	}

	if len(out) == 0 {
		return nil, "", fmt.Errorf("no valid IPs provided")
	}

	return out, primary, nil
}

func firstInterfaceCIDR(cfg *core.WireGuardConfig) (*net.IPNet, error) {
	addr := strings.TrimSpace(cfg.Interface.Address)
	if addr == "" {
		addr = strings.TrimSpace(cfg.Interface.BindAddress)
	}
	if addr == "" {
		return nil, fmt.Errorf("interface address not set")
	}
	first := strings.TrimSpace(strings.Split(addr, ",")[0])
	if first == "" {
		return nil, fmt.Errorf("interface address not set")
	}
	if !strings.Contains(first, "/") {
		return nil, fmt.Errorf("interface address missing mask")
	}
	_, ipNet, err := net.ParseCIDR(first)
	if err != nil {
		return nil, err
	}
	return ipNet, nil
}

func addUsedIP(used map[string]bool, cidr string) {
	if cidr == "" {
		return
	}
	cidr = strings.TrimSpace(cidr)
	host := cidr
	if idx := strings.Index(host, "/"); idx != -1 {
		host = strings.TrimSpace(host[:idx])
	}
	// Track the exact host IP to avoid assigning it.
	if ip := net.ParseIP(host); ip != nil {
		used[ip.String()] = true
	}
	// Also track the network address if a CIDR is provided.
	if strings.Contains(cidr, "/") {
		if _, netblock, err := net.ParseCIDR(cidr); err == nil && netblock != nil {
			used[netblock.IP.String()] = true
		}
	}
}

func findAvailableIP(ipNet *net.IPNet, used map[string]bool) (string, error) {
	base := ipNet.IP.To4()
	if base == nil {
		return "", fmt.Errorf("auto-assign only supports IPv4")
	}

	// network and broadcast
	broadcast := make(net.IP, len(base))
	for i := 0; i < 4; i++ {
		broadcast[i] = base[i] | ^ipNet.Mask[i]
	}

	for i := 1; i < 255; i++ { // skip network (.0) and avoid overflow
		candidate := make(net.IP, len(base))
		copy(candidate, base)
		candidate[3] = candidate[3] + byte(i)

		if !ipNet.Contains(candidate) {
			continue
		}
		if candidate.Equal(base) || candidate.Equal(broadcast) {
			continue
		}
		if used[candidate.String()] {
			continue
		}
		return fmt.Sprintf("%s/32", candidate.String()), nil
	}

	return "", fmt.Errorf("no IP addresses available")
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
	if wgConfig.Interface.Address == "" {
		http.Error(w, "Interface address is required before adding peers", http.StatusBadRequest)
		return
	}

	priv := strings.TrimSpace(req.Private)
	var pub string
	var pk wgtypes.Key
	if priv == "" {
		priv, pub, err = core.GenerateWireGuardKeys()
		if err != nil {
			http.Error(w, "Failed to generate keys: "+err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		pk, err = wgtypes.ParseKey(priv)
		if err != nil {
			http.Error(w, "Invalid private key", http.StatusBadRequest)
			return
		}
		pub = pk.PublicKey().String()
	}

	usedIPs := make(map[string]bool)
	// Reserve interface IP
	addUsedIP(usedIPs, strings.TrimSpace(strings.Split(wgConfig.Interface.Address, ",")[0]))

	for _, p := range wgConfig.Peers {
		existing := strings.TrimSpace(strings.Split(p.AllowedIPs, ",")[0])
		addUsedIP(usedIPs, existing)
	}

	var normalizedIPs []string
	var primaryIP string
	if strings.TrimSpace(req.IP) == "" {
		ipNet, err := firstInterfaceCIDR(wgConfig)
		if err != nil {
			if _, fallbackNet, perr := net.ParseCIDR("10.100.0.0/24"); perr == nil {
				ipNet = fallbackNet
				addUsedIP(usedIPs, "10.100.0.1/32")
			}
		}
		if ipNet == nil {
			http.Error(w, "Cannot auto-assign IP: interface address missing", http.StatusBadRequest)
			return
		}
		autoIP, err := findAvailableIP(ipNet, usedIPs)
		if err != nil {
			http.Error(w, "No IP addresses available", http.StatusInternalServerError)
			return
		}
		normalizedIPs = []string{autoIP}
		primaryIP = autoIP
	} else {
		normalizedIPs, primaryIP, err = normalizeAllowedIPs(req.IP)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if _, netblock, err := net.ParseCIDR(primaryIP); err == nil && netblock != nil && usedIPs[netblock.IP.String()] {
			http.Error(w, "IP already assigned to another peer", http.StatusBadRequest)
			return
		}
	}

	peer := core.WireGuardPeer{
		PublicKey:  pub,
		PrivateKey: priv,
		AllowedIPs: strings.Join(normalizedIPs, ", "),
		Alias:      req.Alias,
		Endpoint:   strings.TrimSpace(req.Endpoint),
	}

	if err := wgConfig.AddPeer(peer); err != nil {
		http.Error(w, "Failed to add peer: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if cfgText, err := buildPeerConfig(*wgConfig, peer, priv); err == nil {
		s.storeQRConfig(pub, cfgText, time.Hour)
	}

	if !s.syncWireGuardConfig(wgConfig) {
		s.markWireGuardPending()
	}

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

	if !s.syncWireGuardConfig(wgConfig) {
		s.markWireGuardPending()
	}
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

	if err := validateService(req.Service); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := runSystemCtl("restart", req.Service); err != nil {
		http.Error(w, "Failed to restart service: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if req.Service == "wireguard" {
		s.clearWireGuardPending()
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleStartService(w http.ResponseWriter, r *http.Request) {
	var req ServiceActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := validateService(req.Service); err != nil {
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

	if err := validateService(req.Service); err != nil {
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

	if !s.syncWireGuardConfig(nil) {
		s.markWireGuardPending()
	}
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

	if !s.syncWireGuardConfig(wgConfig) {
		s.markWireGuardPending()
	}
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

	normalizedIPs, primaryIP, err := normalizeAllowedIPs(req.AllowedIPs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	usedIPs := make(map[string]string) // ip -> publicKey
	addUsedIPStr := func(ip string, owner string) {
		if ip == "" {
			return
		}
		if !strings.Contains(ip, "/") {
			ip += "/32"
		}
		if _, netblock, err := net.ParseCIDR(ip); err == nil && netblock != nil {
			usedIPs[netblock.IP.String()] = owner
		}
	}

	addUsedIPStr(strings.TrimSpace(strings.Split(wgConfig.Interface.Address, ",")[0]), "interface")
	for _, p := range wgConfig.Peers {
		existing := strings.TrimSpace(strings.Split(p.AllowedIPs, ",")[0])
		addUsedIPStr(existing, p.PublicKey)
	}

	if _, netblock, err := net.ParseCIDR(primaryIP); err == nil && netblock != nil {
		if owner, ok := usedIPs[netblock.IP.String()]; ok && owner != pubKey {
			http.Error(w, "IP already assigned to another peer", http.StatusBadRequest)
			return
		}
	}

	req.AllowedIPs = strings.Join(normalizedIPs, ", ")
	req.Endpoint = strings.TrimSpace(req.Endpoint)

	// Refresh QR cache if a private key was supplied
	if req.PrivateKey != "" {
		updatedPeer := req
		updatedPeer.PublicKey = pubKey
		if cfgText, err := buildPeerConfig(*wgConfig, updatedPeer, req.PrivateKey); err == nil {
			s.storeQRConfig(pubKey, cfgText, time.Hour)
		}
	}

	if err := wgConfig.UpdatePeer(pubKey, req); err != nil {
		http.Error(w, "Failed to update peer: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if !s.syncWireGuardConfig(wgConfig) {
		s.markWireGuardPending()
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleGetWireGuardPeerConfig(w http.ResponseWriter, r *http.Request) {
	if !s.requireWireGuard(w) {
		return
	}
	pubKey := r.URL.Query().Get("public_key")
	if pubKey == "" {
		http.Error(w, "public_key is required", http.StatusBadRequest)
		return
	}

	if cfgText, ok := s.fetchQRConfig(pubKey); ok {
		response := map[string]string{
			"config": cfgText,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
		return
	}

	// Allow on-demand generation if a private key is provided (not stored).
	priv := strings.TrimSpace(r.URL.Query().Get("private_key"))
	if priv != "" {
		wgConfig, err := core.LoadWireGuardConfig(s.config.WireGuardConfigPath)
		if err != nil {
			http.Error(w, "Failed to load WireGuard config: "+err.Error(), http.StatusInternalServerError)
			return
		}
		var peer *core.WireGuardPeer
		for i := range wgConfig.Peers {
			if wgConfig.Peers[i].PublicKey == pubKey {
				peer = &wgConfig.Peers[i]
				break
			}
		}
		if peer == nil {
			http.Error(w, "Peer not found", http.StatusNotFound)
			return
		}
		cfgText, err := buildPeerConfig(*wgConfig, *peer, priv)
		if err != nil {
			http.Error(w, "Failed to build peer config: "+err.Error(), http.StatusBadRequest)
			return
		}
		s.storeQRConfig(pubKey, cfgText, time.Hour)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"config": cfgText})
		return
	}

	http.Error(w, "QR/config not available for this peer", http.StatusNotFound)
}

func (s *Server) handleGetWireGuardTraffic(w http.ResponseWriter, r *http.Request) {
	if !s.requireWireGuard(w) {
		return
	}
	rangeStr := r.URL.Query().Get("range")
	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")

	var start, end int64
	now := time.Now().Unix()
	if startStr != "" && endStr != "" {
		if s, err := strconv.ParseInt(startStr, 10, 64); err == nil {
			start = s
		}
		if e, err := strconv.ParseInt(endStr, 10, 64); err == nil {
			end = e
		}
	}
	if start == 0 || end == 0 {
		var duration time.Duration
		switch rangeStr {
		case "30m":
			duration = 30 * time.Minute
		case "30d":
			duration = 30 * 24 * time.Hour
		case "6h":
			duration = 6 * time.Hour
		case "24h":
			duration = 24 * time.Hour
		default:
			duration = time.Hour
		}
		end = now
		start = time.Now().Add(-duration).Unix()
	}

	wgConfig, err := core.LoadWireGuardConfig(s.config.WireGuardConfigPath)
	if err != nil {
		http.Error(w, "Failed to load WireGuard config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	result := make(map[string]map[string]int64)
	for _, p := range wgConfig.Peers {
		rx, tx, err := s.store.GetWGTrafficDelta(p.PublicKey, start, end)
		if err != nil {
			http.Error(w, "Failed to read traffic: "+err.Error(), http.StatusInternalServerError)
			return
		}
		result[p.PublicKey] = map[string]int64{
			"rx": rx,
			"tx": tx,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (s *Server) handleGetWireGuardTrafficSeries(w http.ResponseWriter, r *http.Request) {
	if !s.requireWireGuard(w) {
		return
	}
	rangeStr := r.URL.Query().Get("range")
	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")
	filterKey := strings.TrimSpace(r.URL.Query().Get("peer"))
	limit := 500
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 5000 {
			limit = v
		}
	}

	var start, end int64
	now := time.Now().Unix()
	if startStr != "" && endStr != "" {
		if s, err := strconv.ParseInt(startStr, 10, 64); err == nil {
			start = s
		}
		if e, err := strconv.ParseInt(endStr, 10, 64); err == nil {
			end = e
		}
	}
	if start == 0 || end == 0 {
		var duration time.Duration
		switch rangeStr {
		case "30m":
			duration = 30 * time.Minute
		case "30d":
			duration = 30 * 24 * time.Hour
		case "6h":
			duration = 6 * time.Hour
		case "24h":
			duration = 24 * time.Hour
		default:
			duration = time.Hour
		}
		end = now
		start = time.Now().Add(-duration).Unix()
	}

	wgConfig, err := core.LoadWireGuardConfig(s.config.WireGuardConfigPath)
	if err != nil {
		http.Error(w, "Failed to load WireGuard config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	result := make(map[string][]core.WGSample)
	for _, p := range wgConfig.Peers {
		if filterKey != "" && p.PublicKey != filterKey {
			continue
		}
		series, err := s.store.GetWGTrafficSeries(p.PublicKey, start, end, limit)
		if err != nil {
			http.Error(w, "Failed to read traffic series: "+err.Error(), http.StatusInternalServerError)
			return
		}
		result[p.PublicKey] = series
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func buildPeerConfig(cfg core.WireGuardConfig, peer core.WireGuardPeer, clientPrivateKey string) (string, error) {
	if clientPrivateKey == "" {
		return "", fmt.Errorf("peer missing private key")
	}

	serverPub := cfg.Interface.PublicKey
	if serverPub == "" {
		if cfg.Interface.PrivateKey == "" {
			return "", fmt.Errorf("interface private key not set")
		}
		pk, err := wgtypes.ParseKey(cfg.Interface.PrivateKey)
		if err != nil {
			return "", fmt.Errorf("invalid interface private key")
		}
		serverPub = pk.PublicKey().String()
	}

	firstAllowed := strings.TrimSpace(strings.Split(peer.AllowedIPs, ",")[0])
	if firstAllowed == "" {
		return "", fmt.Errorf("peer allowed IPs missing")
	}

	var b strings.Builder
	fmt.Fprintf(&b, "[Interface]\n")
	fmt.Fprintf(&b, "PrivateKey = %s\n", clientPrivateKey)
	fmt.Fprintf(&b, "Address = %s\n", firstAllowed)
	dns := cfg.Interface.DNS
	if strings.TrimSpace(dns) == "" {
		dns = "1.1.1.1, 8.8.8.8"
	}
	fmt.Fprintf(&b, "DNS = %s\n", dns)
	if cfg.Interface.MTU != 0 {
		fmt.Fprintf(&b, "MTU = %d\n", cfg.Interface.MTU)
	}
	fmt.Fprintf(&b, "\n[Peer]\n")
	fmt.Fprintf(&b, "PublicKey = %s\n", serverPub)
	if peer.PresharedKey != "" {
		fmt.Fprintf(&b, "PresharedKey = %s\n", peer.PresharedKey)
	}
	if ep := detectWireGuardEndpoint(cfg); ep != "" {
		fmt.Fprintf(&b, "Endpoint = %s\n", ep)
	}
	fmt.Fprintf(&b, "AllowedIPs = 0.0.0.0/0, ::/0\n")
	fmt.Fprintf(&b, "PersistentKeepalive = 25\n")

	return b.String(), nil
}

func detectWireGuardEndpoint(cfg core.WireGuardConfig) string {
	port := cfg.Interface.ListenPort
	if port == 0 {
		port = 51820
	}
	// Prefer the IP from the interface Address (host part).
	addr := strings.TrimSpace(cfg.Interface.Address)
	if cfg.Interface.BindAddress != "" {
		addr = cfg.Interface.BindAddress
	}
	if addr != "" {
		host := strings.TrimSpace(strings.Split(addr, "/")[0])
		if host != "" {
			return fmt.Sprintf("%s:%d", host, port)
		}
	}
	ip := firstIPv4ForInterface("eth0")
	if ip == "" {
		ip = firstUsableIPv4()
	}
	if ip == "" {
		return ""
	}
	return fmt.Sprintf("%s:%d", ip, port)
}

func firstIPv4ForInterface(name string) string {
	if name == "" {
		return ""
	}
	ifi, err := net.InterfaceByName(name)
	if err != nil {
		return ""
	}
	addrs, err := ifi.Addrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		var ip net.IP
		switch v := a.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip == nil {
			continue
		}
		if v4 := ip.To4(); v4 != nil {
			return v4.String()
		}
	}
	return ""
}

func firstUsableIPv4() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if strings.HasPrefix(iface.Name, "docker") || strings.HasPrefix(iface.Name, "br-") || strings.HasPrefix(iface.Name, "veth") {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil {
				continue
			}
			if v4 := ip.To4(); v4 != nil {
				return v4.String()
			}
		}
	}
	return ""
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
		"wireguard_pending_restart":   s.wgPendingRestart,
		"wg_sample_interval_sec":      int(s.wgSampleInterval.Seconds()),
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
	if n, err := s.store.PruneOlderThan(cutoff); err == nil {
		totalDeleted += n
	} else {
		log.Printf("PruneNow: samples prune failed: %v", err)
	}

	// Prune WG samples
	if s.config.WGRetentionDays > 0 {
		wgCutoff := time.Now().Add(-time.Duration(wgDays) * 24 * time.Hour).Unix()
		if n, err := s.store.PruneWGSamplesOlderThan(wgCutoff); err == nil {
			totalDeleted += n
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
		"log_source":              s.config.LogSource,
		"access_log_path":         s.config.AccessLogPath,
		"systemctl_available":     hasSystemctl(),
		"journalctl_available":    hasJournalctl(),
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

func (s *Server) handleGetBackupMeta(w http.ResponseWriter, r *http.Request) {
	singboxBak := s.config.SingboxConfigPath + ".bak"
	wgBak := s.config.WireGuardConfigPath + ".bak"

	info := map[string]*time.Time{}
	if st, err := os.Stat(singboxBak); err == nil {
		t := st.ModTime()
		info["singbox_last_backup"] = &t
	}
	if st, err := os.Stat(wgBak); err == nil {
		t := st.ModTime()
		info["wireguard_last_backup"] = &t
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
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
