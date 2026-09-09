package api

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Ogstra/ogs-swg/internal/core"
)

func (s *Server) handleListExternalProfiles(w http.ResponseWriter, _ *http.Request) {
	if s.store == nil {
		writeErr(w, http.StatusInternalServerError, "store unavailable")
		return
	}
	profiles, err := s.store.ListExternalProfiles()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to list external profiles: "+err.Error())
		return
	}
	if profiles == nil {
		profiles = []core.ExternalProfile{}
	}
	writeJSON(w, http.StatusOK, profiles)
}

func (s *Server) handleUpsertExternalProfile(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeErr(w, http.StatusInternalServerError, "store unavailable")
		return
	}
	var p core.ExternalProfile
	if err := decodeJSON(r, &p); err != nil {
		writeErr(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if strings.TrimSpace(p.Name) == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	if p.Type != "vless" && p.Type != "shadowsocks" {
		writeErr(w, http.StatusBadRequest, "type must be \"vless\" or \"shadowsocks\"")
		return
	}
	if p.Port <= 0 {
		writeErr(w, http.StatusBadRequest, "port must be greater than 0")
		return
	}
	if p.Type == "shadowsocks" {
		if strings.TrimSpace(p.Password) == "" {
			writeErr(w, http.StatusBadRequest, "user password is required for Shadowsocks profiles")
			return
		}
		if strings.TrimSpace(p.SSServerKey) == "" {
			writeErr(w, http.StatusBadRequest, "server password is required for Shadowsocks profiles")
			return
		}
	}
	id, err := s.store.UpsertExternalProfile(p)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to upsert external profile: "+err.Error())
		return
	}
	s.InvalidateSubCache()
	s.cache.Del(cacheKeyAllUsers)
	s.cache.Del(cacheKeyAllSubscriptions)
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

func (s *Server) handleDeleteExternalProfile(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeErr(w, http.StatusInternalServerError, "store unavailable")
		return
	}
	idStr := strings.TrimSpace(r.PathValue("id"))
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid external profile id")
		return
	}
	if err := s.store.DeleteExternalProfile(id); err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to delete external profile: "+err.Error())
		return
	}
	s.InvalidateSubCache()
	s.cache.Del(cacheKeyAllUsers)
	s.cache.Del(cacheKeyAllSubscriptions)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleUpdateUserExternalProfiles(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeErr(w, http.StatusInternalServerError, "store unavailable")
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	var req struct {
		ProfileIDs []int64 `json:"profile_ids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.ProfileIDs == nil {
		req.ProfileIDs = []int64{}
	}
	if err := s.store.SetUserExternalProfiles(name, req.ProfileIDs); err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to update user external profiles: "+err.Error())
		return
	}
	s.cache.Del(cacheKeyAllUsers)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// buildExternalVlessLink builds a vless:// Reality share link from an ExternalProfile.
// The host is resolved: if p.HostIPv6File is set, reads the IPv6 from the file;
// otherwise falls back to p.HostIPv4.
func buildExternalVlessLink(displayName string, p core.ExternalProfile) (string, error) {
	host := core.ReadExternalIPv6(p.HostIPv6File)
	if host == "" {
		host = strings.TrimSpace(p.HostIPv4)
	}
	if host == "" {
		return "", fmt.Errorf("external profile %q has no host configured", p.Name)
	}
	if strings.TrimSpace(p.UUID) == "" {
		return "", fmt.Errorf("external VLESS profile %q missing uuid", p.Name)
	}
	if p.Port <= 0 {
		return "", fmt.Errorf("external profile %q missing port", p.Name)
	}
	if strings.TrimSpace(p.PublicKey) == "" {
		return "", fmt.Errorf("external VLESS profile %q missing public_key", p.Name)
	}
	if strings.TrimSpace(p.ShortID) == "" {
		return "", fmt.Errorf("external VLESS profile %q missing short_id", p.Name)
	}
	if strings.TrimSpace(p.ServerName) == "" {
		return "", fmt.Errorf("external VLESS profile %q missing server_name (SNI)", p.Name)
	}
	alpn := normalizedALPN(strings.Split(p.ALPN, ","))

	hostInURI := host
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		hostInURI = "[" + host + "]"
	}

	params := url.Values{}
	params.Set("security", "reality")
	params.Set("encryption", "none")
	params.Set("pbk", strings.TrimSpace(p.PublicKey))
	params.Set("fp", "chrome")
	params.Set("type", "tcp")
	params.Set("sni", strings.TrimSpace(p.ServerName))
	params.Set("sid", strings.TrimSpace(p.ShortID))
	if alpn != "" {
		params.Set("alpn", alpn)
	}
	if strings.TrimSpace(p.Flow) != "" {
		params.Set("flow", strings.TrimSpace(p.Flow))
	}
	params.Set("packetEncoding", "xudp")

	nameTag := url.PathEscape(displayName)
	return fmt.Sprintf("vless://%s@%s:%d?%s#%s",
		url.QueryEscape(p.UUID),
		hostInURI,
		p.Port,
		params.Encode(),
		nameTag,
	), nil
}

// buildExternalShadowsocksLink builds an ss:// SIP002 URI from an ExternalProfile.
// Uses the same base64 credential encoding as the existing buildShadowsocksLink.
func buildExternalShadowsocksLink(displayName string, p core.ExternalProfile) (string, error) {
	host := core.ReadExternalIPv6(p.HostIPv6File)
	if host == "" {
		host = strings.TrimSpace(p.HostIPv4)
	}
	if host == "" {
		return "", fmt.Errorf("external profile %q has no host configured", p.Name)
	}
	password := strings.TrimSpace(p.Password)
	if password == "" {
		return "", fmt.Errorf("external Shadowsocks profile %q missing password", p.Name)
	}
	method := strings.TrimSpace(p.SSMethod)
	if method == "" {
		return "", fmt.Errorf("external Shadowsocks profile %q missing ss_method", p.Name)
	}
	if p.Port <= 0 {
		return "", fmt.Errorf("external profile %q missing port", p.Name)
	}

	hostInURI := host
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		hostInURI = "[" + host + "]"
	}

	serverPassword := strings.TrimSpace(p.SSServerKey)
	if serverPassword == "" {
		return "", fmt.Errorf("external Shadowsocks profile %q missing server password", p.Name)
	}
	credential := method + ":" + serverPassword + ":" + password

	nameTag := url.PathEscape(displayName)
	return fmt.Sprintf("ss://%s@%s:%d#%s",
		base64.StdEncoding.EncodeToString([]byte(credential)),
		hostInURI,
		p.Port,
		nameTag,
	), nil
}

func (s *Server) handleGetExternalProfileLink(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "Invalid profile id")
		return
	}
	displayName := strings.TrimSpace(r.URL.Query().Get("name"))
	if displayName == "" {
		displayName = "External"
	}
	if s.store == nil {
		writeErr(w, http.StatusInternalServerError, "Store unavailable")
		return
	}
	p, err := s.store.GetExternalProfile(id)
	if err != nil || p == nil {
		writeErr(w, http.StatusNotFound, "External profile not found")
		return
	}
	if flag := strings.TrimSpace(p.Flag); flag != "" {
		displayName = flag + displayName
	}
	var link string
	var linkType string
	switch p.Type {
	case "vless":
		link, err = buildExternalVlessLink(displayName, *p)
		linkType = "vless"
	case "shadowsocks":
		link, err = buildExternalShadowsocksLink(displayName, *p)
		linkType = "shadowsocks"
	default:
		writeErr(w, http.StatusBadRequest, "Unsupported external profile type")
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"link": link, "type": linkType})
}
