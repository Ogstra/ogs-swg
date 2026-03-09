package api

import (
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Ogstra/ogs-swg/internal/core"
)

func (s *Server) handleGetSingboxConfig(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}

	content, err := s.config.GetSingboxConfig()
	if err != nil {
		http.Error(w, "Failed to read config: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if shouldRedactConfigReadOnly(r) {
		content = redactSingboxJSON(content)
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(content))
}

func (s *Server) handleGetSingboxDNS(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}

	dns, err := s.config.GetSingboxDNS()
	if err != nil {
		http.Error(w, "Failed to get dns config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(dns)
}

func (s *Server) handleUpdateSingboxConfig(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.config.UpdateSingboxConfig(string(body)); err != nil {
		http.Error(w, "Failed to update config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleUpdateSingboxDNS(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}

	var dns map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&dns); err != nil {
		http.Error(w, "Failed to decode dns config: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.config.UpdateSingboxDNS(dns); err != nil {
		http.Error(w, "Failed to update dns config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleGetSingboxOutbounds(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}

	outbounds, err := s.config.GetSingboxOutboundViews()
	if err != nil {
		http.Error(w, "Failed to get outbounds: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(outbounds)
}

func (s *Server) handleUpdateSingboxOutboundDomainStrategies(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}

	var updates []core.SingboxOutboundDomainStrategyUpdate
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		http.Error(w, "Failed to decode outbound updates: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.config.UpdateSingboxOutboundDomainStrategies(updates); err != nil {
		http.Error(w, "Failed to update outbound domain_strategy: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleGetSingboxInbounds(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}

	inboundViews, err := s.config.GetSingboxInboundViews()
	if err != nil {
		http.Error(w, "Failed to get inbounds: "+err.Error(), http.StatusInternalServerError)
		return
	}
	inbounds := make([]map[string]interface{}, 0, len(inboundViews))
	for _, inboundView := range inboundViews {
		inbounds = append(inbounds, inboundView.Raw)
	}

	if meta, err := s.store.GetAllInboundMeta(); err == nil {
		for i := range inboundViews {
			tag := inboundViews[i].Tag
			if tag == "" {
				continue
			}
			if entry, ok := meta[tag]; ok && entry.ExternalPort > 0 {
				if inbounds[i] == nil {
					inbounds[i] = map[string]interface{}{}
				}
				inbounds[i]["external_port"] = entry.ExternalPort
			}
		}
	}
	if shouldRedactConfigReadOnly(r) {
		for i, inbound := range inbounds {
			if redacted, ok := redactJSONValue(inbound).(map[string]any); ok {
				inbounds[i] = redacted
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(inbounds)
}

func (s *Server) handleGetUserInbounds(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}

	name := r.PathValue("name")
	if name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}

	inbounds, err := s.config.GetUserInbounds(name)
	if err != nil {
		http.Error(w, "Failed to get user inbounds: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if len(inbounds) > 0 {
		tagTypes := map[string]string{}
		if allInboundViews, err := s.config.GetSingboxInboundViews(); err == nil {
			for _, inboundView := range allInboundViews {
				if inboundView.Tag == "" {
					continue
				}
				tagTypes[inboundView.Tag] = inboundView.Type
			}
		}
		if meta, err := s.store.GetUserMetadata(name); err == nil && meta != nil {
			for i := range inbounds {
				if tagTypes[inbounds[i].Tag] == "vmess" {
					if inbounds[i].VmessSecurity == "" && meta.VmessSecurity != "" {
						inbounds[i].VmessSecurity = meta.VmessSecurity
					}
					if inbounds[i].VmessAlterID == 0 && meta.VmessAlterID != 0 {
						inbounds[i].VmessAlterID = meta.VmessAlterID
					}
				}
			}
		}
	}
	if shouldRedactUsersReadOnly(r) {
		for i := range inbounds {
			if strings.TrimSpace(inbounds[i].UUID) != "" {
				inbounds[i].UUID = maskedValue
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(inbounds)
}

func (s *Server) handleGetUserVLESSLink(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}
	if shouldRedactUsersReadOnly(r) || shouldRedactConfigReadOnly(r) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	link, linkType, err := s.buildUserLink(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if linkType != "vless" {
		http.Error(w, "Inbound type is not VLESS", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"link": link})
}

func (s *Server) handleGetUserLink(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}
	if shouldRedactUsersReadOnly(r) || shouldRedactConfigReadOnly(r) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	link, linkType, err := s.buildUserLink(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"link": link, "type": linkType})
}

func (s *Server) buildUserLink(r *http.Request) (string, string, error) {
	name := r.PathValue("name")
	tag := strings.TrimSpace(r.URL.Query().Get("inbound"))
	if name == "" || tag == "" {
		return "", "", fmt.Errorf("Name and inbound tag are required")
	}

	userInbounds, err := s.config.GetUserInbounds(name)
	if err != nil {
		return "", "", fmt.Errorf("Failed to get user inbounds: %w", err)
	}

	var userInfo *core.UserInboundInfo
	for i := range userInbounds {
		if userInbounds[i].Tag == tag {
			userInfo = &userInbounds[i]
			break
		}
	}
	if userInfo == nil {
		return "", "", fmt.Errorf("User not found in selected inbound")
	}
	if userInfo.UUID == "" {
		return "", "", fmt.Errorf("User credential missing for inbound")
	}

	inboundView, err := s.config.GetSingboxInboundView(tag)
	if err != nil {
		return "", "", fmt.Errorf("Failed to get inbound config: %w", err)
	}

	inbType := inboundView.Type
	if inbType == "" {
		inbType = "vless"
	}

	if inboundView.ListenPort <= 0 {
		return "", "", fmt.Errorf("Inbound listen_port missing")
	}
	port := strconv.Itoa(inboundView.ListenPort)

	if meta, err := s.store.GetInboundMeta(tag); err == nil && meta != nil && meta.ExternalPort > 0 {
		port = strconv.Itoa(meta.ExternalPort)
	}

	host := s.resolvePublicHost(r)
	if host == "" {
		return "", "", fmt.Errorf("Public IP not configured")
	}

	switch inbType {
	case "vless":
		link, err := buildVlessLink(name, userInfo, inboundView, host, port)
		return link, inbType, err
	case "vmess":
		userCopy := *userInfo
		if meta, err := s.store.GetUserMetadata(name); err == nil && meta != nil {
			if meta.VmessSecurity != "" {
				userCopy.VmessSecurity = meta.VmessSecurity
			}
			if userCopy.VmessAlterID == 0 && meta.VmessAlterID != 0 {
				userCopy.VmessAlterID = meta.VmessAlterID
			}
		}
		link, err := buildVmessLink(name, &userCopy, inboundView, host, port)
		return link, inbType, err
	case "trojan":
		link, err := buildTrojanLink(name, userInfo, inboundView, host, port)
		return link, inbType, err
	default:
		return "", "", fmt.Errorf("Inbound type is not supported")
	}
}

func (s *Server) resolvePublicHost(r *http.Request) string {
	ip := strings.TrimSpace(s.config.PublicIP)
	if ip != "" {
		return ip
	}
	if isTrustedProxy(r.RemoteAddr) {
		if host := firstHeaderToken(r.Header.Get("X-Forwarded-Host")); host != "" {
			return stripPort(host)
		}
		if host := firstHeaderToken(r.Header.Get("X-Real-IP")); host != "" {
			return stripPort(host)
		}
		if host := firstHeaderToken(r.Header.Get("X-Forwarded-For")); host != "" {
			return stripPort(host)
		}
	}
	return stripPort(r.Host)
}

func isTrustedProxy(remoteAddr string) bool {
	host := strings.TrimSpace(remoteAddr)
	if host == "" {
		return false
	}
	if splitHost, _, err := net.SplitHostPort(host); err == nil {
		host = splitHost
	}
	ip := net.ParseIP(strings.TrimSpace(host))
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() {
		return true
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	return false
}

func stripPort(host string) string {
	if strings.Contains(host, ":") {
		return strings.Split(host, ":")[0]
	}
	return host
}

func firstHeaderToken(value string) string {
	if value == "" {
		return ""
	}
	parts := strings.Split(value, ",")
	if len(parts) == 0 {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(parts[0])
}

type transportInfo struct {
	Type        string
	Path        string
	Host        string
	ServiceName string
}

func extractTransportInfo(inbound map[string]interface{}) transportInfo {
	info := transportInfo{Type: "tcp"}
	transport, ok := inbound["transport"].(map[string]interface{})
	if !ok || transport == nil {
		return info
	}

	if t, ok := transport["type"].(string); ok && t != "" {
		info.Type = t
	}
	if path, ok := transport["path"].(string); ok {
		info.Path = path
	}
	if host, ok := transport["host"].(string); ok {
		info.Host = host
	}
	if headers, ok := transport["headers"].(map[string]interface{}); ok {
		if host, ok := headers["Host"].(string); ok && host != "" {
			info.Host = host
		}
	}
	if svc, ok := transport["service_name"].(string); ok {
		info.ServiceName = svc
	}
	return info
}

type tlsInfo struct {
	Enabled    bool
	ServerName string
	ALPN       []string
	CertPath   string
}

func normalizedALPN(values []string) string {
	if len(values) == 0 {
		return ""
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, v := range values {
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return strings.Join(out, ",")
}

// extractTLSInfo returns TLS parameters from the typed SingboxInboundView.TLS field.
// Falls back gracefully when TLS is nil (no tls block in inbound).
func extractTLSInfo(view *core.SingboxInboundView) tlsInfo {
	if view.TLS == nil {
		return tlsInfo{}
	}
	return tlsInfo{
		Enabled:    view.TLS.Enabled,
		ServerName: view.TLS.ServerName,
		ALPN:       append([]string(nil), view.TLS.ALPN...),
		CertPath:   view.TLS.CertificatePath,
	}
}

func buildVlessLink(name string, userInfo *core.UserInboundInfo, view *core.SingboxInboundView, host, port string) (string, error) {
	tls := extractTLSInfo(view)
	alpn := normalizedALPN(tls.ALPN)
	transport := extractTransportInfo(view.Raw)
	var reality *core.RealityConfig
	if view.TLS != nil {
		reality = view.TLS.Reality
	}
	if reality != nil {
		pbk := reality.PublicKey
		if pbk == "" {
			if priv := reality.PrivateKey; strings.TrimSpace(priv) != "" {
				derived, err := deriveRealityPublicKey(priv)
				if err != nil {
					return "", fmt.Errorf("Reality private_key invalid: %w", err)
				}
				pbk = derived
			}
		}
		if pbk == "" {
			return "", fmt.Errorf("Reality public_key missing")
		}
		handshakeSNI := reality.Handshake.Server

		// Prefer tls.server_name as client SNI; fall back to handshake.server
		sni := view.TLS.ServerName
		if sni == "" {
			sni = handshakeSNI
		}
		if sni == "" {
			return "", fmt.Errorf("Reality handshake server missing")
		}

		sid := ""
		if len(reality.ShortIDs) > 0 {
			sid = reality.ShortIDs[0]
		}
		if sid == "" {
			return "", fmt.Errorf("Reality short_id missing")
		}

		flowParam := ""
		if userInfo.Flow != "" {
			flowParam = "&flow=" + url.QueryEscape(userInfo.Flow)
		}

		nameTag := url.QueryEscape("VLESS-" + name)
		udpParam := ""
		if strings.EqualFold(userInfo.Flow, "xtls-rprx-vision") {
			udpParam = "&udp=0"
		}
		alpnParam := ""
		if alpn != "" {
			alpnParam = "&alpn=" + url.QueryEscape(alpn)
		}
		link := fmt.Sprintf("vless://%s@%s:%s?security=reality&encryption=none&pbk=%s&headerType=none&fp=chrome&type=%s%s&sni=%s&sid=%s%s#%s",
			url.QueryEscape(userInfo.UUID),
			host,
			port,
			url.QueryEscape(pbk),
			url.QueryEscape(transport.Type),
			flowParam,
			url.QueryEscape(sni),
			url.QueryEscape(sid),
			alpnParam,
			nameTag,
		)
		if udpParam != "" {
			link = strings.Replace(link, "#"+nameTag, udpParam+"#"+nameTag, 1)
		}
		return link, nil
	}

	params := url.Values{}
	params.Set("encryption", "none")
	if tls.Enabled {
		params.Set("security", "tls")
	} else {
		params.Set("security", "none")
	}
	if tls.ServerName != "" {
		params.Set("sni", tls.ServerName)
	}
	if alpn != "" {
		params.Set("alpn", alpn)
	}
	if shouldAllowInsecure(tls) {
		params.Set("allowInsecure", "1")
	}
	if strings.EqualFold(userInfo.Flow, "xtls-rprx-vision") {
		params.Set("udp", "0")
	}
	if transport.Type != "" && transport.Type != "tcp" {
		params.Set("type", transport.Type)
	}
	if transport.Type == "ws" || transport.Type == "http" || transport.Type == "httpupgrade" {
		if transport.Path != "" {
			params.Set("path", transport.Path)
		}
		if transport.Host != "" {
			params.Set("host", transport.Host)
		}
	}
	if transport.Type == "grpc" && transport.ServiceName != "" {
		params.Set("serviceName", transport.ServiceName)
	}

	nameTag := url.QueryEscape("VLESS-" + name)
	base := fmt.Sprintf("vless://%s@%s:%s", url.QueryEscape(userInfo.UUID), host, port)
	if encoded := params.Encode(); encoded != "" {
		base += "?" + encoded
	}
	base += "#" + nameTag
	return base, nil
}

func buildTrojanLink(name string, userInfo *core.UserInboundInfo, view *core.SingboxInboundView, host, port string) (string, error) {
	if strings.TrimSpace(userInfo.UUID) == "" {
		return "", fmt.Errorf("User password missing for inbound")
	}
	transport := extractTransportInfo(view.Raw)
	tls := extractTLSInfo(view)
	alpn := normalizedALPN(tls.ALPN)

	params := url.Values{}
	if tls.Enabled {
		params.Set("security", "tls")
	}
	if tls.ServerName != "" {
		params.Set("sni", tls.ServerName)
	}
	if alpn != "" {
		params.Set("alpn", alpn)
	}
	if shouldAllowInsecure(tls) {
		params.Set("allowInsecure", "1")
	}
	if transport.Type != "" && transport.Type != "tcp" {
		params.Set("type", transport.Type)
		if transport.Type == "ws" || transport.Type == "http" || transport.Type == "httpupgrade" {
			if transport.Path != "" {
				params.Set("path", transport.Path)
			}
			if transport.Host != "" {
				params.Set("host", transport.Host)
			}
		}
		if transport.Type == "grpc" && transport.ServiceName != "" {
			params.Set("serviceName", transport.ServiceName)
		}
	}

	nameTag := url.QueryEscape("TROJAN-" + name)
	base := fmt.Sprintf("trojan://%s@%s:%s", url.QueryEscape(userInfo.UUID), host, port)
	if encoded := params.Encode(); encoded != "" {
		base += "?" + encoded
	}
	base += "#" + nameTag
	return base, nil
}

func buildVmessLink(name string, userInfo *core.UserInboundInfo, view *core.SingboxInboundView, host, port string) (string, error) {
	if strings.TrimSpace(userInfo.UUID) == "" {
		return "", fmt.Errorf("User UUID missing for inbound")
	}
	transport := extractTransportInfo(view.Raw)
	tls := extractTLSInfo(view)
	alpn := normalizedALPN(tls.ALPN)

	alterID := userInfo.VmessAlterID
	security := strings.TrimSpace(userInfo.VmessSecurity)
	if security == "" {
		security = "auto"
	}
	net := transport.Type
	// VMess share links expect "h2" to represent HTTP/2 transport.
	if strings.EqualFold(net, "http") {
		net = "h2"
	}

	payload := map[string]string{
		"v":    "2",
		"ps":   "VMESS-" + name,
		"add":  host,
		"port": port,
		"id":   userInfo.UUID,
		"aid":  strconv.Itoa(alterID),
		"net":  net,
		"type": "none",
	}
	if security != "" {
		payload["scy"] = security
	}
	if transport.Type == "ws" || transport.Type == "http" || transport.Type == "httpupgrade" {
		if transport.Path != "" {
			payload["path"] = transport.Path
		}
		if transport.Host != "" {
			payload["host"] = transport.Host
		}
	}
	if transport.Type == "grpc" && transport.ServiceName != "" {
		payload["path"] = transport.ServiceName
	}
	if tls.Enabled {
		payload["tls"] = "tls"
		if tls.ServerName != "" {
			payload["sni"] = tls.ServerName
		}
		if alpn != "" {
			payload["alpn"] = alpn
		}
		if shouldAllowInsecure(tls) {
			payload["allowInsecure"] = "1"
		}
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	encoded := base64.StdEncoding.EncodeToString(raw)
	return "vmess://" + encoded, nil
}

func shouldAllowInsecure(tls tlsInfo) bool {
	if !tls.Enabled {
		return false
	}
	if strings.TrimSpace(tls.ServerName) == "" {
		return true
	}
	cert := strings.ToLower(tls.CertPath)
	return strings.Contains(cert, "selfsigned") || strings.Contains(cert, "self-signed")
}

func (s *Server) handleAddSingboxInbound(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}

	var newInbound map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&newInbound); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := normalizeInboundMultiplex(newInbound); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	externalPort, externalPortSet, err := popExternalPort(newInbound)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.config.AddSingboxInbound(newInbound); err != nil {
		http.Error(w, "Failed to add inbound: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if externalPortSet {
		tag, _ := newInbound["tag"].(string)
		if err := s.store.SaveInboundMeta(tag, externalPort); err != nil {
			http.Error(w, "Failed to save inbound metadata: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusCreated)
}

func deriveRealityPublicKey(privateKey string) (string, error) {
	raw := strings.TrimSpace(privateKey)
	if raw == "" {
		return "", fmt.Errorf("private_key empty")
	}

	decoders := []func(string) ([]byte, error){
		base64.RawURLEncoding.DecodeString,
		base64.RawStdEncoding.DecodeString,
		base64.URLEncoding.DecodeString,
		base64.StdEncoding.DecodeString,
	}

	var keyBytes []byte
	var lastErr error
	for _, dec := range decoders {
		decoded, err := dec(raw)
		if err == nil {
			keyBytes = decoded
			break
		}
		lastErr = err
	}
	if keyBytes == nil {
		return "", lastErr
	}

	curve := ecdh.X25519()
	privKey, err := curve.NewPrivateKey(keyBytes)
	if err != nil {
		return "", err
	}
	pubKey := privKey.PublicKey()
	return base64.RawURLEncoding.EncodeToString(pubKey.Bytes()), nil
}

func (s *Server) handleUpdateSingboxInbound(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}

	tag := r.URL.Query().Get("tag")
	if tag == "" {
		http.Error(w, "Missing tag query parameter", http.StatusBadRequest)
		return
	}

	var updatedInbound map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updatedInbound); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := normalizeInboundMultiplex(updatedInbound); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	externalPort, externalPortSet, err := popExternalPort(updatedInbound)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	newTag, _ := updatedInbound["tag"].(string)
	tagChanged := newTag != "" && newTag != tag

	if err := s.config.UpdateSingboxInbound(tag, updatedInbound); err != nil {
		http.Error(w, "Failed to update inbound: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if tagChanged {
		if err := s.store.RenameInboundMeta(tag, newTag); err != nil {
			http.Error(w, "Failed to update inbound metadata: "+err.Error(), http.StatusInternalServerError)
			return
		}
		tag = newTag
	}
	if externalPortSet {
		if err := s.store.SaveInboundMeta(tag, externalPort); err != nil {
			http.Error(w, "Failed to save inbound metadata: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleDeleteSingboxInbound(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}

	tag := r.URL.Query().Get("tag")
	if tag == "" {
		http.Error(w, "Missing tag query parameter", http.StatusBadRequest)
		return
	}

	if err := s.config.DeleteSingboxInbound(tag); err != nil {
		http.Error(w, "Failed to delete inbound: "+err.Error(), http.StatusInternalServerError)
		return
	}

	_ = s.store.DeleteInboundMeta(tag)

	w.WriteHeader(http.StatusOK)
}

func popExternalPort(inbound map[string]interface{}) (int, bool, error) {
	if inbound == nil {
		return 0, false, nil
	}
	raw, ok := inbound["external_port"]
	if !ok {
		return 0, false, nil
	}
	delete(inbound, "external_port")

	switch v := raw.(type) {
	case nil:
		return 0, true, nil
	case float64:
		return int(v), true, nil
	case int:
		return v, true, nil
	case int64:
		return int(v), true, nil
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return 0, true, nil
		}
		parsed, err := strconv.Atoi(trimmed)
		if err != nil {
			return 0, true, fmt.Errorf("external_port must be a number")
		}
		return parsed, true, nil
	default:
		return 0, true, fmt.Errorf("external_port must be a number")
	}
}

func parseBooleanField(value interface{}, fieldName string) (bool, error) {
	switch v := value.(type) {
	case nil:
		return false, nil
	case bool:
		return v, nil
	case string:
		trimmed := strings.ToLower(strings.TrimSpace(v))
		if trimmed == "" {
			return false, nil
		}
		if trimmed == "true" {
			return true, nil
		}
		if trimmed == "false" {
			return false, nil
		}
		return false, fmt.Errorf("%s must be a boolean", fieldName)
	default:
		return false, fmt.Errorf("%s must be a boolean", fieldName)
	}
}

func parsePositiveIntField(value interface{}, fieldName string) (int, error) {
	var n int64
	switch v := value.(type) {
	case float64:
		n = int64(v)
	case int:
		n = int64(v)
	case int64:
		n = v
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return 0, fmt.Errorf("%s is required", fieldName)
		}
		parsed, err := strconv.Atoi(trimmed)
		if err != nil || parsed <= 0 {
			return 0, fmt.Errorf("%s must be a positive integer", fieldName)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("%s must be a positive integer", fieldName)
	}
	if n <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", fieldName)
	}
	return int(n), nil
}

func normalizeInboundMultiplex(inbound map[string]interface{}) error {
	if inbound == nil {
		return nil
	}
	rawMultiplex, hasMultiplex := inbound["multiplex"]
	if !hasMultiplex || rawMultiplex == nil {
		delete(inbound, "multiplex")
		return nil
	}

	multiplexMap, ok := rawMultiplex.(map[string]interface{})
	if !ok {
		return fmt.Errorf("multiplex must be an object")
	}

	enabled, err := parseBooleanField(multiplexMap["enabled"], "multiplex.enabled")
	if err != nil {
		return err
	}
	if !enabled {
		delete(inbound, "multiplex")
		return nil
	}

	normalized := map[string]interface{}{"enabled": true}
	if rawPadding, ok := multiplexMap["padding"]; ok {
		padding, err := parseBooleanField(rawPadding, "multiplex.padding")
		if err != nil {
			return err
		}
		normalized["padding"] = padding
	}

	rawBrutal := multiplexMap["brutal"]
	if rawBrutal != nil {
		brutalMap, ok := rawBrutal.(map[string]interface{})
		if !ok {
			return fmt.Errorf("multiplex.brutal must be an object")
		}
		brutalEnabled, err := parseBooleanField(brutalMap["enabled"], "multiplex.brutal.enabled")
		if err != nil {
			return err
		}
		if brutalEnabled {
			upMbps, err := parsePositiveIntField(brutalMap["up_mbps"], "multiplex.brutal.up_mbps")
			if err != nil {
				return err
			}
			downMbps, err := parsePositiveIntField(brutalMap["down_mbps"], "multiplex.brutal.down_mbps")
			if err != nil {
				return err
			}
			normalized["brutal"] = map[string]interface{}{
				"enabled":   true,
				"up_mbps":   upMbps,
				"down_mbps": downMbps,
			}
		}
	}

	inbound["multiplex"] = normalized
	return nil
}

// handleApplySingboxChanges applies pending Sing-box configuration changes
func (s *Server) handleApplySingboxChanges(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}

	if err := s.config.ApplySingboxChanges(); err != nil {
		http.Error(w, "Failed to apply changes: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Sing-box configuration applied successfully",
	})
}
