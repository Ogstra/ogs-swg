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

	"github.com/Ogstra/ogs-swg/core"
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

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(content))
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

func (s *Server) handleGetSingboxInbounds(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}

	inbounds, err := s.config.GetSingboxInbounds()
	if err != nil {
		http.Error(w, "Failed to get inbounds: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if meta, err := s.store.GetAllInboundMeta(); err == nil {
		for _, inbound := range inbounds {
			tag, _ := inbound["tag"].(string)
			if tag == "" {
				continue
			}
			if entry, ok := meta[tag]; ok && entry.ExternalPort > 0 {
				inbound["external_port"] = entry.ExternalPort
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
		if allInbounds, err := s.config.GetSingboxInbounds(); err == nil {
			for _, inbound := range allInbounds {
				tag, _ := inbound["tag"].(string)
				if tag == "" {
					continue
				}
				if t, ok := inbound["type"].(string); ok {
					tagTypes[tag] = strings.ToLower(strings.TrimSpace(t))
				}
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(inbounds)
}

func (s *Server) handleGetUserVLESSLink(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
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

	inbounds, err := s.config.GetSingboxInbounds()
	if err != nil {
		return "", "", fmt.Errorf("Failed to get inbounds: %w", err)
	}

	var inbound map[string]interface{}
	for _, inb := range inbounds {
		if inbTag, ok := inb["tag"].(string); ok && inbTag == tag {
			inbound = inb
			break
		}
	}
	if inbound == nil {
		return "", "", fmt.Errorf("Inbound config not found")
	}

	inbType := ""
	if rawType, ok := inbound["type"].(string); ok {
		inbType = strings.ToLower(strings.TrimSpace(rawType))
	}
	if inbType == "" {
		inbType = "vless"
	}

	port, err := extractInboundPort(inbound)
	if err != nil {
		return "", "", err
	}

	if meta, err := s.store.GetInboundMeta(tag); err == nil && meta != nil && meta.ExternalPort > 0 {
		port = strconv.Itoa(meta.ExternalPort)
	}

	host := s.resolvePublicHost(r)
	if host == "" {
		return "", "", fmt.Errorf("Public IP not configured")
	}

	switch inbType {
	case "vless":
		link, err := buildVlessLink(name, userInfo, inbound, host, port)
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
		link, err := buildVmessLink(name, &userCopy, inbound, host, port)
		return link, inbType, err
	case "trojan":
		link, err := buildTrojanLink(name, userInfo, inbound, host, port)
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

func extractInboundPort(inbound map[string]interface{}) (string, error) {
	switch v := inbound["listen_port"].(type) {
	case float64:
		return fmt.Sprintf("%.0f", v), nil
	case int:
		return fmt.Sprintf("%d", v), nil
	case int64:
		return fmt.Sprintf("%d", v), nil
	case string:
		if strings.TrimSpace(v) != "" {
			return v, nil
		}
	}
	return "", fmt.Errorf("Inbound listen_port missing")
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
	CertPath   string
}

func extractTLSInfo(inbound map[string]interface{}) tlsInfo {
	tls, ok := inbound["tls"].(map[string]interface{})
	if !ok || tls == nil {
		return tlsInfo{}
	}
	enabled, _ := tls["enabled"].(bool)
	serverName, _ := tls["server_name"].(string)
	certPath, _ := tls["certificate_path"].(string)
	return tlsInfo{Enabled: enabled, ServerName: serverName, CertPath: certPath}
}

func buildVlessLink(name string, userInfo *core.UserInboundInfo, inbound map[string]interface{}, host, port string) (string, error) {
	tls, _ := inbound["tls"].(map[string]interface{})
	reality, _ := tls["reality"].(map[string]interface{})
	if reality == nil {
		return "", fmt.Errorf("Inbound is missing Reality configuration")
	}
	pbk, _ := reality["public_key"].(string)
	if pbk == "" {
		if priv, _ := reality["private_key"].(string); strings.TrimSpace(priv) != "" {
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
	handshake, _ := reality["handshake"].(map[string]interface{})
	sni, _ := handshake["server"].(string)
	if sni == "" {
		return "", fmt.Errorf("Reality handshake server missing")
	}

	var sid string
	switch v := reality["short_id"].(type) {
	case []interface{}:
		if len(v) > 0 {
			if s, ok := v[0].(string); ok {
				sid = s
			}
		}
	case []string:
		if len(v) > 0 {
			sid = v[0]
		}
	case string:
		sid = v
	}
	if sid == "" {
		return "", fmt.Errorf("Reality short_id missing")
	}

	transport := extractTransportInfo(inbound)
	flowParam := ""
	if userInfo.Flow != "" {
		flowParam = "&flow=" + url.QueryEscape(userInfo.Flow)
	}

	nameTag := url.QueryEscape("VLESS-" + name)
	link := fmt.Sprintf("vless://%s@%s:%s?security=reality&encryption=none&pbk=%s&headerType=none&fp=chrome&type=%s%s&sni=%s&sid=%s#%s",
		url.QueryEscape(userInfo.UUID),
		host,
		port,
		url.QueryEscape(pbk),
		url.QueryEscape(transport.Type),
		flowParam,
		url.QueryEscape(sni),
		url.QueryEscape(sid),
		nameTag,
	)
	return link, nil
}

func buildTrojanLink(name string, userInfo *core.UserInboundInfo, inbound map[string]interface{}, host, port string) (string, error) {
	if strings.TrimSpace(userInfo.UUID) == "" {
		return "", fmt.Errorf("User password missing for inbound")
	}
	transport := extractTransportInfo(inbound)
	tls := extractTLSInfo(inbound)

	params := url.Values{}
	if tls.Enabled {
		params.Set("security", "tls")
	}
	if tls.ServerName != "" {
		params.Set("sni", tls.ServerName)
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

func buildVmessLink(name string, userInfo *core.UserInboundInfo, inbound map[string]interface{}, host, port string) (string, error) {
	if strings.TrimSpace(userInfo.UUID) == "" {
		return "", fmt.Errorf("User UUID missing for inbound")
	}
	transport := extractTransportInfo(inbound)
	tls := extractTLSInfo(inbound)

	alterID := userInfo.VmessAlterID
	security := strings.TrimSpace(userInfo.VmessSecurity)
	if security == "" {
		security = "auto"
	}

	payload := map[string]string{
		"v":    "2",
		"ps":   "VMESS-" + name,
		"add":  host,
		"port": port,
		"id":   userInfo.UUID,
		"aid":  strconv.Itoa(alterID),
		"net":  transport.Type,
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
