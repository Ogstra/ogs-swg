package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"

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
	wgConfig, err := s.loadWireGuardConfig(r.Context())
	if err != nil {
		http.Error(w, "Failed to load WireGuard config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var stats map[string]core.PeerStats
	if s.executor != nil {
		stats, _ = s.executor.GetWireGuardStats(r.Context())
	} else {
		stats, _ = core.GetWireGuardStats()
	}
	storedPeers, _ := s.store.GetWGPeerMeta()

	response := make([]PeerWithStats, 0)
	redactWG := shouldRedactWireGuardReadOnly(r)
	for _, p := range wgConfig.Peers {
		if p.Alias == "" && p.Email != "" {
			p.Alias = p.Email
		}
		alias := p.Alias
		if alias == "" {
			if len(p.PublicKey) >= 8 {
				alias = p.PublicKey[:8]
			} else {
				alias = p.PublicKey
			}
		}
		_ = s.store.UpsertWGPeer(p.PublicKey, alias, false)
		ps := PeerWithStats{
			WireGuardPeer: p,
			QRAvailable:   s.hasQRConfig(p.PublicKey),
		}
		// Never expose peer private keys in list responses.
		ps.WireGuardPeer.PrivateKey = ""
		if redactWG && strings.TrimSpace(ps.WireGuardPeer.PresharedKey) != "" {
			ps.WireGuardPeer.PresharedKey = maskedValue
		}
		if redactWG && strings.TrimSpace(ps.WireGuardPeer.PublicKey) != "" {
			ps.WireGuardPeer.PublicKey = maskedValue
		}
		if s, ok := stats[p.PublicKey]; ok {
			ps.Stats = s
			if redactWG && strings.TrimSpace(ps.Stats.PublicKey) != "" {
				ps.Stats.PublicKey = maskedValue
			}
			if ps.Stats.LatestHandshake <= 0 {
				if meta, ok := storedPeers[p.PublicKey]; ok {
					ps.Stats.LatestHandshake = meta.LastHandshake
				}
			}
		} else if meta, ok := storedPeers[p.PublicKey]; ok {
			ps.Stats = core.PeerStats{
				PublicKey:       p.PublicKey,
				LatestHandshake: meta.LastHandshake,
			}
			if redactWG && strings.TrimSpace(ps.Stats.PublicKey) != "" {
				ps.Stats.PublicKey = maskedValue
			}
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
	Alias    string `json:"alias" validate:"required"`
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
	if ip, ok := extractHostIP(cidr); ok {
		used[ip.String()] = true
	}
}

func extractHostIP(cidr string) (net.IP, bool) {
	cidr = strings.TrimSpace(cidr)
	if cidr == "" {
		return nil, false
	}
	host := cidr
	if idx := strings.Index(host, "/"); idx != -1 {
		host = strings.TrimSpace(host[:idx])
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return nil, false
	}
	return ip, true
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
	if err := s.validate.Struct(req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Alias == "" && req.Email != "" {
		req.Alias = req.Email
	}

	wgConfig, err := s.loadWireGuardConfig(r.Context())
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
		if ip, ok := extractHostIP(primaryIP); ok && usedIPs[ip.String()] {
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
	alias := peer.Alias
	if alias == "" {
		if len(peer.PublicKey) >= 8 {
			alias = peer.PublicKey[:8]
		} else {
			alias = peer.PublicKey
		}
	}

	if err := mutateWireGuardConfig(wgConfig, func(c *core.WireGuardConfig) error { return c.AddPeer(peer) }); err != nil {
		http.Error(w, "Failed to add peer: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.saveWireGuardConfig(r.Context(), wgConfig); err != nil {
		http.Error(w, "Failed to persist WireGuard config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_ = s.store.UpsertWGPeer(peer.PublicKey, alias, false)

	if cfgText, err := buildPeerConfig(*wgConfig, peer, priv, s.config.PublicIP); err == nil {
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

	wgConfig, err := s.loadWireGuardConfig(r.Context())
	if err != nil {
		http.Error(w, "Failed to load WireGuard config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	alias := ""
	for _, p := range wgConfig.Peers {
		if p.PublicKey == pubKey {
			alias = p.Alias
			if alias == "" && p.Email != "" {
				alias = p.Email
			}
			break
		}
	}
	if alias == "" {
		if len(pubKey) >= 8 {
			alias = pubKey[:8]
		} else {
			alias = pubKey
		}
	}
	_ = s.store.UpsertWGPeer(pubKey, alias, true)

	if err := mutateWireGuardConfig(wgConfig, func(c *core.WireGuardConfig) error { return c.RemovePeer(pubKey) }); err != nil {
		http.Error(w, "Failed to remove peer: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.saveWireGuardConfig(r.Context(), wgConfig); err != nil {
		http.Error(w, "Failed to persist WireGuard config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if !s.syncWireGuardConfig(wgConfig) {
		s.markWireGuardPending()
	}
	w.WriteHeader(http.StatusOK)
}

type RestorePeerRequest struct {
	PublicKey    string `json:"public_key" validate:"required"`
	AllowedIPs   string `json:"allowed_ips" validate:"required"`
	Endpoint     string `json:"endpoint,omitempty"`
	Alias        string `json:"alias,omitempty"`
	Email        string `json:"email,omitempty"`
	PresharedKey string `json:"preshared_key,omitempty"`
}

func (s *Server) handleRestoreWireGuardPeer(w http.ResponseWriter, r *http.Request) {
	if !s.requireWireGuard(w) {
		return
	}
	var req RestorePeerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}
	if err := s.validate.Struct(req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	wgConfig, err := s.loadWireGuardConfig(r.Context())
	if err != nil {
		http.Error(w, "Failed to load WireGuard config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	peer := core.WireGuardPeer{
		PublicKey:    strings.TrimSpace(req.PublicKey),
		AllowedIPs:   strings.TrimSpace(req.AllowedIPs),
		Endpoint:     strings.TrimSpace(req.Endpoint),
		Alias:        strings.TrimSpace(req.Alias),
		Email:        strings.TrimSpace(req.Email),
		PresharedKey: strings.TrimSpace(req.PresharedKey),
	}
	alias := peer.Alias
	if alias == "" && peer.Email != "" {
		alias = peer.Email
	}
	if alias == "" {
		if len(peer.PublicKey) >= 8 {
			alias = peer.PublicKey[:8]
		} else {
			alias = peer.PublicKey
		}
	}

	if err := mutateWireGuardConfig(wgConfig, func(c *core.WireGuardConfig) error { return c.AddPeer(peer) }); err != nil {
		http.Error(w, "Failed to restore peer: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.saveWireGuardConfig(r.Context(), wgConfig); err != nil {
		http.Error(w, "Failed to persist WireGuard config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_ = s.store.UpsertWGPeer(peer.PublicKey, alias, false)

	if !s.syncWireGuardConfig(wgConfig) {
		s.markWireGuardPending()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(peer)
}

func (s *Server) handleGetWireGuardConfig(w http.ResponseWriter, r *http.Request) {
	if !s.requireWireGuard(w) {
		return
	}

	redact := shouldRedactWireGuardReadOnly(r)

	if s.executor != nil {
		content, err := s.executor.ReadConfig(r.Context(), s.config.WireGuardConfigPath)
		if err != nil {
			if isNotFoundErr(err) {
				w.Header().Set("Content-Type", "text/plain")
				w.Write([]byte(""))
				return
			}

			http.Error(w, "Failed to read config: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		if redact {
			w.Write([]byte(redactWireGuardConfigText(string(content))))
			return
		}
		w.Write(content)
		return
	}

	// Fallback/Legacy logic if executor is somehow nil (impossible with new init)
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
	if redact {
		w.Write([]byte(redactWireGuardConfigText(string(content))))
		return
	}
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

	if s.executor != nil {
		if err := s.executor.WriteConfig(r.Context(), s.config.WireGuardConfigPath, content, 0644); err != nil {
			http.Error(w, "Failed to write config: "+err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		if err := os.WriteFile(s.config.WireGuardConfigPath, content, 0644); err != nil {
			http.Error(w, "Failed to write config: "+err.Error(), http.StatusInternalServerError)
			return
		}
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
	wgConfig, err := s.loadWireGuardConfig(r.Context())
	if err != nil {
		http.Error(w, "Failed to load WireGuard config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if wgConfig.Interface.PublicKey == "" && wgConfig.Interface.PrivateKey != "" {
		if pk, err := wgtypes.ParseKey(wgConfig.Interface.PrivateKey); err == nil {
			wgConfig.Interface.PublicKey = pk.PublicKey().String()
		}
	}
	if shouldRedactWireGuardReadOnly(r) {
		redactWireGuardInterfaceSecret(&wgConfig.Interface)
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

	wgConfig, err := s.loadWireGuardConfig(r.Context())
	if err != nil {
		http.Error(w, "Failed to load WireGuard config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if err := mutateWireGuardConfig(wgConfig, func(c *core.WireGuardConfig) error { return c.UpdateInterface(req) }); err != nil {
		http.Error(w, "Failed to update interface: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.saveWireGuardConfig(r.Context(), wgConfig); err != nil {
		http.Error(w, "Failed to persist WireGuard config: "+err.Error(), http.StatusInternalServerError)
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

	wgConfig, err := s.loadWireGuardConfig(r.Context())
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
		if host, ok := extractHostIP(ip); ok {
			usedIPs[host.String()] = owner
		}
	}

	addUsedIPStr(strings.TrimSpace(strings.Split(wgConfig.Interface.Address, ",")[0]), "interface")
	for _, p := range wgConfig.Peers {
		existing := strings.TrimSpace(strings.Split(p.AllowedIPs, ",")[0])
		addUsedIPStr(existing, p.PublicKey)
	}

	if host, ok := extractHostIP(primaryIP); ok {
		if owner, found := usedIPs[host.String()]; found && owner != pubKey {
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
		if cfgText, err := buildPeerConfig(*wgConfig, updatedPeer, req.PrivateKey, s.config.PublicIP); err == nil {
			s.storeQRConfig(pubKey, cfgText, time.Hour)
		}
	}

	if err := mutateWireGuardConfig(wgConfig, func(c *core.WireGuardConfig) error { return c.UpdatePeer(pubKey, req) }); err != nil {
		http.Error(w, "Failed to update peer: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.saveWireGuardConfig(r.Context(), wgConfig); err != nil {
		http.Error(w, "Failed to persist WireGuard config: "+err.Error(), http.StatusInternalServerError)
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
		if shouldRedactWireGuardReadOnly(r) {
			cfgText = redactWireGuardConfigText(cfgText)
		}
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
		wgConfig, err := s.loadWireGuardConfig(r.Context())
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
		cfgText, err := buildPeerConfig(*wgConfig, *peer, priv, s.config.PublicIP)
		if err != nil {
			http.Error(w, "Failed to build peer config: "+err.Error(), http.StatusBadRequest)
			return
		}
		s.storeQRConfig(pubKey, cfgText, time.Hour)
		if shouldRedactWireGuardReadOnly(r) {
			cfgText = redactWireGuardConfigText(cfgText)
		}
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

	wgConfig, err := s.loadWireGuardConfig(r.Context())
	if err != nil {
		http.Error(w, "Failed to load WireGuard config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	result := make(map[string]map[string]int64)
	redactWG := shouldRedactWireGuardReadOnly(r)
	for i, p := range wgConfig.Peers {
		rx, tx, err := s.store.GetWGTrafficDelta(p.PublicKey, start, end)
		if err != nil {
			http.Error(w, "Failed to read traffic: "+err.Error(), http.StatusInternalServerError)
			return
		}
		key := p.PublicKey
		if redactWG {
			key = fmt.Sprintf("peer_%d", i+1)
		}
		result[key] = map[string]int64{
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

	wgConfig, err := s.loadWireGuardConfig(r.Context())
	if err != nil {
		http.Error(w, "Failed to load WireGuard config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	result := make(map[string][]core.WGSample)
	redactWG := shouldRedactWireGuardReadOnly(r)
	for i, p := range wgConfig.Peers {
		if filterKey != "" && p.PublicKey != filterKey {
			continue
		}
		series, err := s.store.GetWGTrafficSeries(p.PublicKey, start, end, limit)
		if err != nil {
			http.Error(w, "Failed to read traffic series: "+err.Error(), http.StatusInternalServerError)
			return
		}
		key := p.PublicKey
		if redactWG {
			key = fmt.Sprintf("peer_%d", i+1)
			for j := range series {
				if strings.TrimSpace(series[j].PublicKey) != "" {
					series[j].PublicKey = maskedValue
				}
			}
		}
		result[key] = series
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func buildPeerConfig(cfg core.WireGuardConfig, peer core.WireGuardPeer, clientPrivateKey, publicIP string) (string, error) {
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
	address, err := peerClientAddress(firstAllowed, cfg.Interface.Address)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "[Interface]\n")
	fmt.Fprintf(&b, "PrivateKey = %s\n", clientPrivateKey)
	fmt.Fprintf(&b, "Address = %s\n", address)
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
	if ep := detectWireGuardEndpoint(cfg, publicIP); ep != "" {
		fmt.Fprintf(&b, "Endpoint = %s\n", ep)
	}
	fmt.Fprintf(&b, "AllowedIPs = 0.0.0.0/0, ::/0\n")
	fmt.Fprintf(&b, "PersistentKeepalive = 25\n")

	return b.String(), nil
}

// peerClientAddress derives the WireGuard client [Interface] Address by combining
// the peer's IP with the server interface's subnet prefix.
// e.g.: peerAllowedIPs="10.0.0.5/32", serverIfaceAddr="10.0.0.1/24" → "10.0.0.5/24"
func peerClientAddress(peerAllowedIPs, serverIfaceAddr string) (string, error) {
	peerAllowedIPs = strings.TrimSpace(peerAllowedIPs)
	if peerAllowedIPs == "" {
		return "", fmt.Errorf("peer allowed IPs missing")
	}

	var peerIP net.IP
	if strings.Contains(peerAllowedIPs, "/") {
		ip, _, err := net.ParseCIDR(peerAllowedIPs)
		if err != nil || ip == nil {
			return "", fmt.Errorf("invalid peer CIDR: %s", peerAllowedIPs)
		}
		peerIP = ip
	} else {
		peerIP = net.ParseIP(peerAllowedIPs)
		if peerIP == nil {
			return "", fmt.Errorf("invalid peer IP: %s", peerAllowedIPs)
		}
	}

	firstServerAddr := strings.TrimSpace(strings.Split(serverIfaceAddr, ",")[0])
	if firstServerAddr != "" {
		if _, serverNet, err := net.ParseCIDR(firstServerAddr); err == nil {
			ones, _ := serverNet.Mask.Size()
			return fmt.Sprintf("%s/%d", peerIP.String(), ones), nil
		}
	}

	// Fallback when server address is unavailable or unparseable
	if peerIP.To4() != nil {
		return peerIP.String() + "/32", nil
	}
	return peerIP.String() + "/128", nil
}

func detectWireGuardEndpoint(cfg core.WireGuardConfig, publicIP string) string {
	port := cfg.Interface.ListenPort
	if port == 0 {
		port = 51820
	}
	if strings.TrimSpace(publicIP) != "" {
		return fmt.Sprintf("%s:%d", strings.TrimSpace(publicIP), port)
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

func (s *Server) handleBackupWireGuardConfig(w http.ResponseWriter, r *http.Request) {
	if !s.requireWireGuard(w) {
		return
	}
	src := s.config.WireGuardConfigPath
	dst := src + ".bak"
	if err := s.copyConfig(r.Context(), src, dst); err != nil {
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
	dst := s.config.WireGuardConfigPath
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
	w.Header().Set("Content-Type", "text/plain")
	w.Write(content)
}
