package api

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
)

// ntfySettingsRequest is the PUT /api/settings/ntfy and POST
// /api/settings/ntfy/test request body. An empty BearerToken/BasicPass means
// "keep the currently stored secret" — see mergeNtfySecrets.
type ntfySettingsRequest struct {
	ServerURL             string `json:"server_url"`
	Topic                 string `json:"topic"`
	AuthMode              string `json:"auth_mode"`
	BearerToken           string `json:"bearer_token"`
	BasicUser             string `json:"basic_user"`
	BasicPass             string `json:"basic_pass"`
	EnableSingboxDown     bool   `json:"enable_singbox_down"`
	EnableWireguardDown   bool   `json:"enable_wireguard_down"`
	EnableHighTraffic     bool   `json:"enable_high_traffic"`
	EnableConfigErrors    bool   `json:"enable_config_errors"`
	TrafficThresholdBytes int64  `json:"traffic_threshold_bytes"`
}

// ntfySettingsResponse is the GET /api/settings/ntfy response body. It has no
// field for bearer_token or basic_pass — secrets can never be serialized
// into this type, so the HTTP boundary cannot leak them by accident.
type ntfySettingsResponse struct {
	ServerURL             string `json:"server_url"`
	Topic                 string `json:"topic"`
	AuthMode              string `json:"auth_mode"`
	BasicUser             string `json:"basic_user"`
	HasBearerToken        bool   `json:"has_bearer_token"`
	HasBasicPass          bool   `json:"has_basic_pass"`
	EnableSingboxDown     bool   `json:"enable_singbox_down"`
	EnableWireguardDown   bool   `json:"enable_wireguard_down"`
	EnableHighTraffic     bool   `json:"enable_high_traffic"`
	EnableConfigErrors    bool   `json:"enable_config_errors"`
	TrafficThresholdBytes int64  `json:"traffic_threshold_bytes"`
}

func ntfySettingsToResponse(s core.NtfySettings) ntfySettingsResponse {
	return ntfySettingsResponse{
		ServerURL:             s.ServerURL,
		Topic:                 s.Topic,
		AuthMode:              s.AuthMode,
		BasicUser:             s.BasicUser,
		HasBearerToken:        s.BearerToken != "",
		HasBasicPass:          s.BasicPass != "",
		EnableSingboxDown:     s.EnableSingboxDown,
		EnableWireguardDown:   s.EnableWireguardDown,
		EnableHighTraffic:     s.EnableHighTraffic,
		EnableConfigErrors:    s.EnableConfigErrors,
		TrafficThresholdBytes: s.TrafficThresholdBytes,
	}
}

// mergeNtfySecrets loads the currently stored settings and overlays req on
// top of them, preserving the stored secret for the active auth mode when
// the incoming request field is empty. Used by both the PUT handler and the
// test-notification handler so "test without retyping the saved secret"
// and "save without blanking the secret" share one code path.
func (s *Server) mergeNtfySecrets(ctx context.Context, req ntfySettingsRequest) (core.NtfySettings, error) {
	stored, err := s.store.GetNtfySettings(ctx)
	if err != nil {
		return core.NtfySettings{}, err
	}

	out := core.NtfySettings{
		ServerURL:             req.ServerURL,
		Topic:                 req.Topic,
		AuthMode:              req.AuthMode,
		BearerToken:           req.BearerToken,
		BasicUser:             req.BasicUser,
		BasicPass:             req.BasicPass,
		EnableSingboxDown:     req.EnableSingboxDown,
		EnableWireguardDown:   req.EnableWireguardDown,
		EnableHighTraffic:     req.EnableHighTraffic,
		EnableConfigErrors:    req.EnableConfigErrors,
		TrafficThresholdBytes: req.TrafficThresholdBytes,
	}

	if req.AuthMode == "bearer" && req.BearerToken == "" {
		out.BearerToken = stored.BearerToken
	}
	if req.AuthMode == "basic" && req.BasicPass == "" {
		out.BasicPass = stored.BasicPass
	}

	return core.NormalizeNtfySettings(out), nil
}

func (s *Server) handleGetNtfySettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.GetNtfySettings(r.Context())
	if err != nil {
		log.Printf("handleGetNtfySettings: %v", err)
		writeErr(w, http.StatusInternalServerError, "Failed to load ntfy settings")
		return
	}
	writeJSON(w, http.StatusOK, ntfySettingsToResponse(settings))
}

func (s *Server) handleUpdateNtfySettings(w http.ResponseWriter, r *http.Request) {
	var req ntfySettingsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	settings, err := s.mergeNtfySecrets(r.Context(), req)
	if err != nil {
		log.Printf("handleUpdateNtfySettings: load stored settings: %v", err)
		writeErr(w, http.StatusInternalServerError, "Failed to load stored ntfy settings")
		return
	}

	if err := s.store.UpdateNtfySettings(r.Context(), settings); err != nil {
		log.Printf("handleUpdateNtfySettings: save: %v", err)
		writeErr(w, http.StatusInternalServerError, "Failed to save ntfy settings")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// handleTestNtfyNotification publishes one low-priority test message using
// the values currently in the request body (not the saved settings), so the
// operator can validate a config before saving it. It never persists
// anything.
func (s *Server) handleTestNtfyNotification(w http.ResponseWriter, r *http.Request) {
	var req ntfySettingsRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	settings, err := s.mergeNtfySecrets(r.Context(), req)
	if err != nil {
		log.Printf("handleTestNtfyNotification: load stored settings: %v", err)
		writeErr(w, http.StatusInternalServerError, "Failed to load stored ntfy settings")
		return
	}
	if !settings.Configured() {
		writeErr(w, http.StatusBadRequest, "ntfy server URL and topic are required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	// SECURITY: appending err.Error() here is safe only because
	// internal/core/ntfy.go's PublishNtfy is documented (see its
	// "SECURITY:" comment) to never embed s.BearerToken/s.BasicPass into a
	// returned error. If that contract ever changes, this must change too.
	if err := core.PublishNtfy(ctx, settings, core.NtfyTestMessage()); err != nil {
		// Not http.StatusBadGateway: Cloudflare (and other CDN proxies in front
		// of this panel) intercept 502/503/504 from the origin and substitute
		// their own branded error page, hiding this JSON body from the user.
		// 422 is a normal application-level failure (bad ntfy config/URL), not
		// an actual gateway problem, so it passes through untouched.
		writeErr(w, http.StatusUnprocessableEntity, "Test notification failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}
