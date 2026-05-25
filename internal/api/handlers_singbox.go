package api

import (
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
)

type inboundUpdateResponse struct {
	Warnings []string `json:"warnings"`
}

func validateRawSingboxConfigPayload(content []byte) error {
	if strings.TrimSpace(string(content)) == "" {
		return errors.New("config cannot be empty")
	}

	var js map[string]interface{}
	if err := json.Unmarshal(content, &js); err != nil {
		return fmt.Errorf("invalid json: %w", err)
	}
	return nil
}

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

	if err := validateRawSingboxConfigPayload(body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
		log.Printf("handleGetSingboxOutbounds: config unavailable: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
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
		log.Printf("handleGetSingboxInbounds: config unavailable: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
		return
	}
	inbounds := make([]map[string]interface{}, 0, len(inboundViews))
	for _, inboundView := range inboundViews {
		inbounds = append(inbounds, inboundView.Raw)
	}

	if s.store != nil {
		var meta map[string]core.InboundMeta
		if cached, found := s.cache.Get(cacheKeyAllInboundMeta); found {
			if cachedMeta, ok := cached.(map[string]core.InboundMeta); ok {
				meta = cachedMeta
			}
		}
		if meta == nil {
			if loadedMeta, err := s.store.GetAllInboundMeta(); err == nil {
				meta = loadedMeta
				s.cache.SetWithTTL(cacheKeyAllInboundMeta, meta, 1, 30*time.Second)
			}
		}
		if meta != nil {
			for i := range inboundViews {
				tag := inboundViews[i].Tag
				if tag == "" {
					continue
				}
				if entry, ok := meta[tag]; ok {
					if inbounds[i] == nil {
						inbounds[i] = map[string]interface{}{}
					}
					if entry.ExternalPort > 0 {
						inbounds[i]["external_port"] = entry.ExternalPort
					}
					if entry.OverrideAddress != "" {
						inbounds[i]["override_address"] = entry.OverrideAddress
					}
					switch {
					case entry.LinkAllowInsecure == nil:
						inbounds[i]["link_allow_insecure"] = "auto"
					case *entry.LinkAllowInsecure:
						inbounds[i]["link_allow_insecure"] = "enabled"
					default:
						inbounds[i]["link_allow_insecure"] = "disabled"
					}
				}
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
	if len(inbounds) == 0 && s.store != nil {
		if meta, err := s.store.GetUserMetadata(name); err == nil && meta != nil && len(meta.InboundTags) > 0 && strings.TrimSpace(meta.Credential) != "" {
			for _, tag := range meta.InboundTags {
				inbounds = append(inbounds, core.UserInboundInfo{
					Tag:           tag,
					UUID:          meta.Credential,
					Flow:          meta.Flow,
					VmessSecurity: meta.VmessSecurity,
					VmessAlterID:  meta.VmessAlterID,
				})
			}
		}
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
		if s.store != nil {
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
	}
	if shouldRedactUsersReadOnly(r) {
		for i := range inbounds {
			if strings.TrimSpace(inbounds[i].UUID) != "" {
				inbounds[i].UUID = maskedValue
			}
			if strings.TrimSpace(inbounds[i].Password) != "" {
				inbounds[i].Password = maskedValue
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

func sanitiseInboundFields(inbound map[string]interface{}) {
	core.SanitiseManagedInboundFields(inbound)
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

	inboundView, err := s.config.GetSingboxInboundView(tag)
	if err != nil {
		return "", "", fmt.Errorf("Failed to get inbound config: %w", err)
	}

	inbType := inboundView.Type
	if inbType == "" {
		inbType = "vless"
	}

	if inbType != "hysteria2" && inbType != "trojan" && inbType != "shadowsocks" && inbType != "anytls" && inbType != "naive" && userInfo.UUID == "" {
		return "", "", fmt.Errorf("User credential missing for inbound")
	}

	if inboundView.ListenPort <= 0 {
		return "", "", fmt.Errorf("Inbound listen_port missing")
	}
	port := strconv.Itoa(inboundView.ListenPort)
	originalHost := s.resolvePublicHost(r)
	host := originalHost
	var inboundMeta *core.InboundMeta
	if s.store != nil {
		if meta, err := s.store.GetInboundMeta(tag); err == nil && meta != nil {
			inboundMeta = meta
			if meta.ExternalPort > 0 {
				port = strconv.Itoa(meta.ExternalPort)
			}
			if meta.OverrideAddress != "" {
				host = meta.OverrideAddress
			}
		}
	}

	if host == "" {
		return "", "", fmt.Errorf("Public IP not configured")
	}

	// When override_address replaces the host with an IP, pass the original
	// domain as SNI fallback so clients can complete the TLS handshake.
	sniFallback := ""
	if host != originalHost {
		sniFallback = originalHost
	}

	switch inbType {
	case "vless":
		link, err := buildVlessLink(name, userInfo, inboundView, host, port, inboundMeta, sniFallback)
		return link, inbType, err
	case "vmess":
		userCopy := *userInfo
		if s.store != nil {
			if meta, err := s.store.GetUserMetadata(name); err == nil && meta != nil {
				if meta.VmessSecurity != "" {
					userCopy.VmessSecurity = meta.VmessSecurity
				}
				if userCopy.VmessAlterID == 0 && meta.VmessAlterID != 0 {
					userCopy.VmessAlterID = meta.VmessAlterID
				}
			}
		}
		link, err := buildVmessLink(name, &userCopy, inboundView, host, port, inboundMeta, sniFallback)
		return link, inbType, err
	case "trojan":
		link, err := buildTrojanLink(name, userInfo, inboundView, host, port, inboundMeta, sniFallback)
		return link, inbType, err
	case "hysteria2":
		link, err := buildHysteria2Link(name, userInfo, inboundView, host, port)
		return link, inbType, err
	case "shadowsocks":
		link, err := buildShadowsocksLink(name, userInfo, inboundView, host, port)
		return link, inbType, err
	case "anytls":
		link, err := buildAnyTLSLink(name, userInfo, inboundView, host, port, inboundMeta, sniFallback)
		return link, inbType, err
	case "naive":
		link, err := buildNaiveLink(name, userInfo, inboundView, host, port, inboundMeta, sniFallback)
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
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if parsedHost, parsedPort, err := net.SplitHostPort(host); err == nil && parsedPort != "" {
		return parsedHost
	}
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		return strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	}
	if strings.Count(host, ":") == 1 {
		if rawHost, _, ok := strings.Cut(host, ":"); ok {
			return rawHost
		}
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

func vlessPacketEncoding(transportType string) string {
	if strings.EqualFold(strings.TrimSpace(transportType), "tcp") || strings.TrimSpace(transportType) == "" {
		return "xudp"
	}
	return ""
}

func buildVlessLink(name string, userInfo *core.UserInboundInfo, view *core.SingboxInboundView, host, port string, meta *core.InboundMeta, sniFallback string) (string, error) {
	tls := extractTLSInfo(view)
	alpn := normalizedALPN(tls.ALPN)
	transport := extractTransportInfo(view.Raw)
	typeVal := transport.Type
	if typeVal == "" {
		typeVal = "tcp"
	}
	packetEncoding := vlessPacketEncoding(typeVal)
	var reality *core.RealityConfig
	if view.TLS != nil {
		reality = view.TLS.Reality
	}
	if reality != nil && reality.Enabled {
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
			flowParam = userInfo.Flow
		}

		params := url.Values{}
		params.Set("security", "reality")
		params.Set("encryption", "none")
		params.Set("pbk", pbk)
		params.Set("headerType", "none")
		params.Set("fp", "chrome")
		params.Set("type", typeVal)
		params.Set("sni", sni)
		params.Set("sid", sid)
		if alpn != "" {
			params.Set("alpn", alpn)
		}
		if flowParam != "" {
			params.Set("flow", flowParam)
		}
		if packetEncoding != "" {
			params.Set("packetEncoding", packetEncoding)
		}

		nameTag := url.QueryEscape(name)
		base := fmt.Sprintf("vless://%s@%s:%s", url.QueryEscape(userInfo.UUID), host, port)
		if encoded := params.Encode(); encoded != "" {
			base += "?" + encoded
		}
		base += "#" + nameTag
		return base, nil
	}

	params := url.Values{}
	params.Set("encryption", "none")
	if tls.Enabled {
		params.Set("security", "tls")
	} else {
		params.Set("security", "none")
	}
	if sni := tls.ServerName; sni != "" {
		params.Set("sni", sni)
	} else if sniFallback != "" {
		params.Set("sni", sniFallback)
	}
	if alpn != "" {
		params.Set("alpn", alpn)
	}
	if shouldAllowInsecure(tls, meta) {
		params.Set("allowInsecure", "1")
	}
	if packetEncoding != "" {
		params.Set("packetEncoding", packetEncoding)
	}
	params.Set("type", typeVal)
	if typeVal == "ws" || typeVal == "http" || typeVal == "httpupgrade" {
		if transport.Path != "" {
			params.Set("path", transport.Path)
		}
		if transport.Host != "" {
			params.Set("host", transport.Host)
		}
	}
	if typeVal == "grpc" && transport.ServiceName != "" {
		params.Set("serviceName", transport.ServiceName)
	}

	nameTag := url.QueryEscape(name)
	base := fmt.Sprintf("vless://%s@%s:%s", url.QueryEscape(userInfo.UUID), host, port)
	if encoded := params.Encode(); encoded != "" {
		base += "?" + encoded
	}
	base += "#" + nameTag
	return base, nil
}

func buildTrojanLink(name string, userInfo *core.UserInboundInfo, view *core.SingboxInboundView, host, port string, meta *core.InboundMeta, sniFallback string) (string, error) {
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
	if sni := tls.ServerName; sni != "" {
		params.Set("sni", sni)
	} else if sniFallback != "" {
		params.Set("sni", sniFallback)
	}
	if alpn != "" {
		params.Set("alpn", alpn)
	}
	if shouldAllowInsecure(tls, meta) {
		params.Set("allowInsecure", "1")
	}
	trojanType := transport.Type
	if trojanType == "" {
		trojanType = "tcp"
	}
	params.Set("type", trojanType)
	if trojanType == "ws" || trojanType == "http" || trojanType == "httpupgrade" {
		if transport.Path != "" {
			params.Set("path", transport.Path)
		}
		if transport.Host != "" {
			params.Set("host", transport.Host)
		}
	}
	if trojanType == "grpc" && transport.ServiceName != "" {
		params.Set("serviceName", transport.ServiceName)
	}

	nameTag := url.QueryEscape(name)
	base := fmt.Sprintf("trojan://%s@%s:%s", url.QueryEscape(userInfo.UUID), host, port)
	if encoded := params.Encode(); encoded != "" {
		base += "?" + encoded
	}
	base += "#" + nameTag
	return base, nil
}

func buildHysteria2Link(name string, userInfo *core.UserInboundInfo, view *core.SingboxInboundView, host, port string) (string, error) {
	password := strings.TrimSpace(userInfo.Password)
	if password == "" {
		return "", fmt.Errorf("User password missing for hysteria2 inbound")
	}
	tls := extractTLSInfo(view)
	params := url.Values{}
	if tls.ServerName != "" {
		params.Set("sni", tls.ServerName)
	}
	// Extract obfs from Raw — SingboxInboundView has no typed Obfs field
	if obfsMap, ok := view.Raw["obfs"].(map[string]interface{}); ok {
		obfsType, _ := obfsMap["type"].(string)
		obfsPwd, _ := obfsMap["password"].(string)
		if obfsType != "" && obfsPwd != "" {
			params.Set("obfs", obfsType)
			params.Set("obfs-password", obfsPwd)
		}
	}
	nameTag := url.QueryEscape(name)
	base := fmt.Sprintf("hysteria2://%s@%s:%s",
		url.QueryEscape(password),
		host,
		port,
	)
	if encoded := params.Encode(); encoded != "" {
		base += "?" + encoded
	}
	base += "#" + nameTag
	return base, nil
}

func buildAnyTLSLink(name string, userInfo *core.UserInboundInfo, view *core.SingboxInboundView, host, port string, meta *core.InboundMeta, sniFallback string) (string, error) {
	password := strings.TrimSpace(userInfo.Password)
	if password == "" {
		password = strings.TrimSpace(userInfo.UUID)
	}
	if password == "" {
		return "", fmt.Errorf("User password missing for anytls inbound")
	}

	tls := extractTLSInfo(view)
	params := url.Values{}
	if sni := tls.ServerName; sni != "" {
		params.Set("sni", sni)
	} else if sniFallback != "" {
		params.Set("sni", sniFallback)
	}
	if alpn := normalizedALPN(tls.ALPN); alpn != "" {
		params.Set("alpn", alpn)
	}
	if shouldAllowInsecure(tls, meta) {
		params.Set("insecure", "1")
	}

	nameTag := url.QueryEscape(name)
	base := fmt.Sprintf("anytls://%s@%s:%s", url.QueryEscape(password), host, port)
	if encoded := params.Encode(); encoded != "" {
		base += "?" + encoded
	}
	base += "#" + nameTag
	return base, nil
}

func buildNaiveLink(name string, userInfo *core.UserInboundInfo, view *core.SingboxInboundView, host, port string, meta *core.InboundMeta, sniFallback string) (string, error) {
	password := strings.TrimSpace(userInfo.Password)
	if password == "" {
		password = strings.TrimSpace(userInfo.UUID)
	}
	if password == "" {
		return "", fmt.Errorf("User password missing for naive inbound")
	}

	scheme := "naive+https"
	if network, _ := view.Raw["network"].(string); strings.EqualFold(strings.TrimSpace(network), "udp") {
		scheme = "naive+quic"
	}
	tls := extractTLSInfo(view)
	params := url.Values{}
	if sni := tls.ServerName; sni != "" && sni != host {
		params.Set("sni", sni)
	} else if sniFallback != "" {
		params.Set("sni", sniFallback)
	}
	if shouldAllowInsecure(tls, meta) {
		params.Set("insecure", "1")
	}

	nameTag := url.QueryEscape(name)
	base := fmt.Sprintf("%s://%s:%s@%s:%s", scheme, url.QueryEscape(name), url.QueryEscape(password), host, port)
	if encoded := params.Encode(); encoded != "" {
		base += "?" + encoded
	}
	base += "#" + nameTag
	return base, nil
}

func buildShadowsocksLink(name string, userInfo *core.UserInboundInfo, view *core.SingboxInboundView, host, port string) (string, error) {
	password := strings.TrimSpace(userInfo.Password)
	if password == "" {
		password = strings.TrimSpace(userInfo.UUID)
	}
	if password == "" {
		return "", fmt.Errorf("User password missing for shadowsocks inbound")
	}

	method, _ := view.Raw["method"].(string)
	method = strings.TrimSpace(method)
	if method == "" {
		return "", fmt.Errorf("Shadowsocks inbound method missing")
	}

	// Shadowsocks 2022 multi-user: inbound has a top-level server key that the
	// client must prepend to the user key ("server_key:user_key").
	credential := method + ":"
	if serverKey, _ := view.Raw["password"].(string); strings.TrimSpace(serverKey) != "" {
		credential += strings.TrimSpace(serverKey) + ":" + password
	} else {
		credential += password
	}

	// SIP002 URI: ss://BASE64(method[:server_key]:user_key)@host:port#tag
	nameTag := url.QueryEscape(name)
	return fmt.Sprintf("ss://%s@%s:%s#%s", base64.StdEncoding.EncodeToString([]byte(credential)), host, port, nameTag), nil
}

func buildVmessLink(name string, userInfo *core.UserInboundInfo, view *core.SingboxInboundView, host, port string, meta *core.InboundMeta, sniFallback string) (string, error) {
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
		"ps":   name,
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
		if sni := tls.ServerName; sni != "" {
			payload["sni"] = sni
		} else if sniFallback != "" {
			payload["sni"] = sniFallback
		}
		if alpn != "" {
			payload["alpn"] = alpn
		}
		if shouldAllowInsecure(tls, meta) {
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

func shouldAllowInsecure(tls tlsInfo, meta *core.InboundMeta) bool {
	if meta != nil && meta.LinkAllowInsecure != nil {
		return *meta.LinkAllowInsecure
	}
	if !tls.Enabled {
		return false
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
	sanitiseInboundFields(newInbound)

	externalPort, externalPortSet, linkAllowInsecure, linkAllowInsecureSet, err := popInboundLinkMeta(newInbound)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	overrideAddress, overrideAddressSet := popOverrideAddress(newInbound)
	if s.store == nil && (externalPortSet || linkAllowInsecureSet || overrideAddressSet) {
		http.Error(w, "Inbound metadata store unavailable", http.StatusInternalServerError)
		return
	}

	if err := s.config.AddSingboxInbound(newInbound); err != nil {
		http.Error(w, "Failed to add inbound: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if externalPortSet || linkAllowInsecureSet || overrideAddressSet {
		tag, _ := newInbound["tag"].(string)
		meta := core.InboundMeta{Tag: tag}
		if externalPortSet {
			meta.ExternalPort = externalPort
		}
		if linkAllowInsecureSet {
			meta.LinkAllowInsecure = linkAllowInsecure
		}
		if overrideAddressSet {
			meta.OverrideAddress = overrideAddress
		}
		if err := s.store.SaveInboundMeta(meta); err != nil {
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

	originalInbound, err := s.getSingboxInboundRaw(tag)
	if err != nil {
		http.Error(w, "Failed to load inbound before update: "+err.Error(), http.StatusInternalServerError)
		return
	}
	warnings := buildInboundUpdateWarnings(originalInbound, updatedInbound)
	sanitiseInboundFields(updatedInbound)
	if warnings == nil {
		warnings = []string{}
	}

	externalPort, externalPortSet, linkAllowInsecure, linkAllowInsecureSet, err := popInboundLinkMeta(updatedInbound)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	overrideAddress, overrideAddressSet := popOverrideAddress(updatedInbound)

	newTag, _ := updatedInbound["tag"].(string)
	tagChanged := newTag != "" && newTag != tag
	if s.store == nil && (tagChanged || externalPortSet || linkAllowInsecureSet || overrideAddressSet) {
		http.Error(w, "Inbound metadata store unavailable", http.StatusInternalServerError)
		return
	}

	if err := s.config.UpdateSingboxInbound(tag, updatedInbound); err != nil {
		http.Error(w, "Failed to update inbound: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if tagChanged {
		if err := s.store.RenameInboundReferences(tag, newTag); err != nil {
			if rollbackErr := s.config.UpdateSingboxInbound(newTag, originalInbound); rollbackErr != nil {
				http.Error(w, "Failed to update inbound metadata: "+err.Error()+" (rollback failed: "+rollbackErr.Error()+")", http.StatusInternalServerError)
				return
			}
			http.Error(w, "Failed to update inbound metadata: "+err.Error(), http.StatusInternalServerError)
			return
		}
		tag = newTag
	}
	if externalPortSet || linkAllowInsecureSet || overrideAddressSet {
		meta, err := s.store.GetInboundMeta(tag)
		if err != nil {
			http.Error(w, "Failed to load inbound metadata: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if meta == nil {
			meta = &core.InboundMeta{Tag: tag}
		}
		if externalPortSet {
			meta.ExternalPort = externalPort
		}
		if linkAllowInsecureSet {
			meta.LinkAllowInsecure = linkAllowInsecure
		}
		if overrideAddressSet {
			meta.OverrideAddress = overrideAddress
		}
		if err := s.store.SaveInboundMeta(*meta); err != nil {
			http.Error(w, "Failed to save inbound metadata: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	s.cache.Del(cacheKeyAllInboundMeta)
	if tagChanged {
		s.cache.Del(cacheKeyAllUsers)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(inboundUpdateResponse{Warnings: warnings})
}

func (s *Server) getSingboxInboundRaw(tag string) (map[string]interface{}, error) {
	inbounds, err := s.config.GetSingboxInboundViews()
	if err != nil {
		return nil, err
	}
	for _, inbound := range inbounds {
		if inbound.Tag != tag {
			continue
		}
		cloned := map[string]interface{}{}
		payload, err := json.Marshal(inbound.Raw)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(payload, &cloned); err != nil {
			return nil, err
		}
		return cloned, nil
	}
	return nil, fmt.Errorf("inbound %q not found", tag)
}

func buildInboundUpdateWarnings(beforeInbound, updatedInbound map[string]interface{}) []string {
	if beforeInbound == nil || updatedInbound == nil {
		return []string{}
	}

	inbType, _ := updatedInbound["type"].(string)
	if strings.ToLower(strings.TrimSpace(inbType)) != "vless" {
		return []string{}
	}

	beforeTransport := extractTransportInfo(beforeInbound).Type
	afterTransport := extractTransportInfo(updatedInbound).Type
	if beforeTransport == "ws" || afterTransport != "ws" {
		return []string{}
	}

	affected := make(map[string]struct{})
	collectFlowUsers(beforeInbound["users"], affected)
	collectFlowUsers(updatedInbound["users"], affected)
	if len(affected) == 0 {
		return []string{}
	}

	return []string{
		fmt.Sprintf("Removed VLESS flow from %d user(s) because WebSocket transport does not support it.", len(affected)),
	}
}

func collectFlowUsers(rawUsers interface{}, affected map[string]struct{}) {
	users, ok := rawUsers.([]interface{})
	if !ok {
		return
	}
	for idx, rawUser := range users {
		user, ok := rawUser.(map[string]interface{})
		if !ok || user == nil {
			continue
		}
		flow, _ := user["flow"].(string)
		if strings.TrimSpace(flow) == "" {
			continue
		}
		identifier, _ := user["name"].(string)
		if strings.TrimSpace(identifier) == "" {
			identifier, _ = user["uuid"].(string)
		}
		if strings.TrimSpace(identifier) == "" {
			identifier = fmt.Sprintf("user-%d", idx)
		}
		affected[identifier] = struct{}{}
	}
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

	affectedUsers := usersForDeletedInbound(s.config, tag)

	if err := s.config.DeleteSingboxInbound(tag); err != nil {
		http.Error(w, "Failed to delete inbound: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if s.store != nil {
		_ = s.store.DeleteInboundMeta(tag)
		for _, user := range affectedUsers {
			if err := s.removeUserFromSubscriptionsIfUnassigned(user); err != nil {
				http.Error(w, "Failed to remove user from subscriptions: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}

	s.InvalidateSubCache()
	s.cache.Del(cacheKeyAllInboundMeta)
	s.cache.Del(cacheKeyAllUsers)
	w.WriteHeader(http.StatusOK)
}

func usersForDeletedInbound(cfg *core.Config, tag string) []string {
	if cfg == nil {
		return nil
	}
	view, err := cfg.GetSingboxInboundView(tag)
	if err != nil || view == nil {
		return nil
	}
	users := make([]string, 0, len(view.Users))
	seen := make(map[string]struct{}, len(view.Users))
	for _, user := range view.Users {
		name := strings.TrimSpace(user.Name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		users = append(users, name)
	}
	return users
}

func popInboundLinkMeta(inbound map[string]interface{}) (int, bool, *bool, bool, error) {
	if inbound == nil {
		return 0, false, nil, false, nil
	}

	externalPort := 0
	externalPortSet := false
	if raw, ok := inbound["external_port"]; ok {
		delete(inbound, "external_port")
		externalPortSet = true
		switch v := raw.(type) {
		case nil:
			externalPort = 0
		case float64:
			externalPort = int(v)
		case int:
			externalPort = v
		case int64:
			externalPort = int(v)
		case string:
			trimmed := strings.TrimSpace(v)
			if trimmed == "" {
				externalPort = 0
			} else {
				parsed, err := strconv.Atoi(trimmed)
				if err != nil {
					return 0, false, nil, false, fmt.Errorf("external_port must be a number")
				}
				externalPort = parsed
			}
		default:
			return 0, false, nil, false, fmt.Errorf("external_port must be a number")
		}
	}

	linkAllowInsecure := (*bool)(nil)
	linkAllowInsecureSet := false
	if raw, ok := inbound["link_allow_insecure"]; ok {
		delete(inbound, "link_allow_insecure")
		linkAllowInsecureSet = true
		switch v := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", raw))); v {
		case "", "auto":
			linkAllowInsecure = nil
		case "enabled", "true", "1":
			value := true
			linkAllowInsecure = &value
		case "disabled", "false", "0":
			value := false
			linkAllowInsecure = &value
		default:
			return 0, false, nil, false, fmt.Errorf("link_allow_insecure must be auto, enabled, or disabled")
		}
	}
	return externalPort, externalPortSet, linkAllowInsecure, linkAllowInsecureSet, nil
}

func popOverrideAddress(inbound map[string]interface{}) (string, bool) {
	if inbound == nil {
		return "", false
	}
	raw, ok := inbound["override_address"]
	if !ok {
		return "", false
	}
	delete(inbound, "override_address")
	addr, _ := raw.(string)
	return strings.TrimSpace(addr), true
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
		var restartRequired *core.SingboxRestartRequiredError
		if errors.As(err, &restartRequired) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success":          false,
				"restart_required": true,
				"message":          "Sing-box restart required to apply this configuration",
			})
			return
		}
		http.Error(w, "Failed to apply changes: "+err.Error(), http.StatusInternalServerError)
		return
	}

	s.InvalidateSubCache()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Sing-box configuration applied successfully",
	})
}

func (s *Server) handleGetSingboxRouteRules(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}

	rules, err := s.config.GetSingboxRouteRules()
	if err != nil {
		log.Printf("handleGetSingboxRouteRules: config unavailable: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("[]"))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rules)
}

func (s *Server) handleUpsertSingboxRouteRules(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}

	var rules []map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&rules); err != nil {
		http.Error(w, "Failed to decode rules: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.config.UpsertSingboxRouteRules(rules); err != nil {
		http.Error(w, "Failed to upsert route rules: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
