package api

import (
	"crypto/ecdh"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(inbounds)
}

func (s *Server) handleGetUserVLESSLink(w http.ResponseWriter, r *http.Request) {
	if !s.requireSingbox(w) {
		return
	}

	name := r.PathValue("name")
	tag := strings.TrimSpace(r.URL.Query().Get("inbound"))
	if name == "" || tag == "" {
		http.Error(w, "Name and inbound tag are required", http.StatusBadRequest)
		return
	}

	userInbounds, err := s.config.GetUserInbounds(name)
	if err != nil {
		http.Error(w, "Failed to get user inbounds: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var userInfo *core.UserInboundInfo
	for i := range userInbounds {
		if userInbounds[i].Tag == tag {
			userInfo = &userInbounds[i]
			break
		}
	}
	if userInfo == nil {
		http.Error(w, "User not found in selected inbound", http.StatusNotFound)
		return
	}
	if userInfo.UUID == "" {
		http.Error(w, "User UUID missing for inbound", http.StatusBadRequest)
		return
	}

	inbounds, err := s.config.GetSingboxInbounds()
	if err != nil {
		http.Error(w, "Failed to get inbounds: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var inbound map[string]interface{}
	for _, inb := range inbounds {
		if inbTag, ok := inb["tag"].(string); ok && inbTag == tag {
			inbound = inb
			break
		}
	}
	if inbound == nil {
		http.Error(w, "Inbound config not found", http.StatusNotFound)
		return
	}

	if inbType, _ := inbound["type"].(string); inbType != "" && inbType != "vless" {
		http.Error(w, "Inbound type is not VLESS", http.StatusBadRequest)
		return
	}

	port := ""
	switch v := inbound["listen_port"].(type) {
	case float64:
		port = fmt.Sprintf("%.0f", v)
	case int:
		port = fmt.Sprintf("%d", v)
	case int64:
		port = fmt.Sprintf("%d", v)
	case string:
		port = v
	}
	if port == "" {
		http.Error(w, "Inbound listen_port missing", http.StatusBadRequest)
		return
	}

	ip := strings.TrimSpace(s.config.PublicIP)
	if ip == "" {
		host := r.Host
		if strings.Contains(host, ":") {
			host = strings.Split(host, ":")[0]
		}
		ip = host
	}
	if ip == "" {
		http.Error(w, "Public IP not configured", http.StatusBadRequest)
		return
	}

	tls, _ := inbound["tls"].(map[string]interface{})
	reality, _ := tls["reality"].(map[string]interface{})
	if reality == nil {
		http.Error(w, "Inbound is missing Reality configuration", http.StatusBadRequest)
		return
	}
	pbk, _ := reality["public_key"].(string)
	if pbk == "" {
		if priv, _ := reality["private_key"].(string); strings.TrimSpace(priv) != "" {
			derived, err := deriveRealityPublicKey(priv)
			if err != nil {
				http.Error(w, "Reality private_key invalid: "+err.Error(), http.StatusBadRequest)
				return
			}
			pbk = derived
		}
	}
	if pbk == "" {
		http.Error(w, "Reality public_key missing", http.StatusBadRequest)
		return
	}
	handshake, _ := reality["handshake"].(map[string]interface{})
	sni, _ := handshake["server"].(string)
	if sni == "" {
		http.Error(w, "Reality handshake server missing", http.StatusBadRequest)
		return
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
		http.Error(w, "Reality short_id missing", http.StatusBadRequest)
		return
	}

	transportType := "tcp"
	if transport, ok := inbound["transport"].(map[string]interface{}); ok {
		if t, ok := transport["type"].(string); ok && t != "" {
			transportType = t
		}
	}

	flowParam := ""
	if userInfo.Flow != "" {
		flowParam = "&flow=" + url.QueryEscape(userInfo.Flow)
	}

	nameTag := url.QueryEscape("VLESS-" + name)
	link := fmt.Sprintf("vless://%s@%s:%s?security=reality&encryption=none&pbk=%s&headerType=none&fp=chrome&type=%s%s&sni=%s&sid=%s#%s",
		url.QueryEscape(userInfo.UUID),
		ip,
		port,
		url.QueryEscape(pbk),
		url.QueryEscape(transportType),
		flowParam,
		url.QueryEscape(sni),
		url.QueryEscape(sid),
		nameTag,
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"link": link})
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

	if err := s.config.AddSingboxInbound(newInbound); err != nil {
		http.Error(w, "Failed to add inbound: "+err.Error(), http.StatusInternalServerError)
		return
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

	if err := s.config.UpdateSingboxInbound(tag, updatedInbound); err != nil {
		http.Error(w, "Failed to update inbound: "+err.Error(), http.StatusInternalServerError)
		return
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

	w.WriteHeader(http.StatusOK)
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
