package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
	"github.com/o1egl/paseto"
)

type LoginRequest struct {
	Username string `json:"username" validate:"required"`
	Password string `json:"password" validate:"required"`
}

type LoginResponse struct {
	Token       string                    `json:"token"`
	Permissions core.PanelUserPermissions `json:"permissions"`
}

func currentPanelUsername(r *http.Request) (string, bool) {
	claims, ok := r.Context().Value(userContextKey).(map[string]interface{})
	if !ok {
		return "", false
	}
	username, ok := claims["sub"].(string)
	if !ok || strings.TrimSpace(username) == "" {
		return "", false
	}
	return username, true
}

func getPasetoKey(secret string) []byte {
	key := make([]byte, 32)
	copy(key, []byte(secret))
	return key
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.config.DisablePasswordLogin {
		writeErr(w, http.StatusForbidden, "Password login is disabled")
		return
	}

	var req LoginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if err := s.validate.Struct(req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	if blocked, retryAfter := s.loginLimiter.isBlocked(req.Username); blocked {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
		writeErr(w, http.StatusTooManyRequests, "Too many failed login attempts, try again later")
		return
	}

	perms, err := s.store.VerifyPanelUser(req.Username, req.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Authentication error")
		return
	}
	if perms == nil {
		s.loginLimiter.recordFailure(req.Username)
		writeErr(w, http.StatusUnauthorized, "Invalid credentials")
		return
	}
	perms.Normalize()

	// Generate PASETO token with permissions embedded
	v2 := paseto.NewV2()
	now := time.Now()
	jsonToken := paseto.JSONToken{
		Subject:    req.Username,
		IssuedAt:   now,
		Expiration: now.Add(24 * time.Hour),
	}
	permsJSON, _ := json.Marshal(perms)
	jsonToken.Set("perms", string(permsJSON))

	tokenString, err := v2.Encrypt(getPasetoKey(s.config.JWTSecret), jsonToken, nil)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to generate token")
		return
	}

	writeJSON(w, http.StatusOK, LoginResponse{Token: tokenString, Permissions: *perms})

	if s.auditStore != nil {
		if err := s.auditStore.InsertAuditLog(r.Context(), core.AuditEntry{
			Ts:       time.Now().Unix(),
			Actor:    req.Username,
			IP:       requestAuditIP(r),
			Action:   "login",
			Domain:   "auth",
			EntityID: req.Username,
		}); err == nil {
			s.invalidateAuditLogCache()
		}
	}
}

type UpdatePasswordRequest struct {
	CurrentPassword string `json:"current_password" validate:"required"`
	NewPassword     string `json:"new_password" validate:"required,min=8"`
}

func (s *Server) handleUpdatePassword(w http.ResponseWriter, r *http.Request) {
	var req UpdatePasswordRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if err := s.validate.Struct(req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	// Get username from context (set by AuthMiddleware)
	username, ok := currentPanelUsername(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	// Verify current password
	perms, err := s.store.VerifyPanelUser(username, req.CurrentPassword)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Verification error")
		return
	}
	if perms == nil {
		writeErr(w, http.StatusUnauthorized, "Invalid current password")
		return
	}

	// Update password
	if err := s.store.UpdatePanelUserPassword(username, req.NewPassword); err != nil {
		writeErr(w, http.StatusInternalServerError, "Failed to update password")
		return
	}

	w.WriteHeader(http.StatusOK)
}

type UpdateUsernameRequest struct {
	CurrentPassword string `json:"current_password" validate:"required"`
	NewUsername     string `json:"new_username" validate:"required"`
}

func (s *Server) handleUpdateUsername(w http.ResponseWriter, r *http.Request) {
	var req UpdateUsernameRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if err := s.validate.Struct(req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	// Get username from context
	currentUsername, ok := currentPanelUsername(r)
	if !ok {
		writeErr(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	// Verify current password
	perms, err := s.store.VerifyPanelUser(currentUsername, req.CurrentPassword)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "Verification error")
		return
	}
	if perms == nil {
		writeErr(w, http.StatusUnauthorized, "Invalid current password")
		return
	}

	// Update username
	if err := s.store.UpdatePanelUsername(currentUsername, req.NewUsername); err != nil {
		if strings.Contains(err.Error(), "already exists") {
			writeErr(w, http.StatusConflict, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, "Failed to update username: "+err.Error())
		return
	}

	s.insertAuditEntry(r, "auth", "update", currentUsername, "to:"+req.NewUsername)
	w.WriteHeader(http.StatusOK)
}

// contextKey is a private type for context keys in this package.
type contextKey string

const (
	userContextKey        contextKey = "user"
	permissionsContextKey contextKey = "permissions"
)

// AuthMiddleware validates the PASETO token and injects user + permissions into context.
func (s *Server) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow login endpoint without token
		if r.URL.Path == "/api/login" {
			next.ServeHTTP(w, r)
			return
		}

		// Allow public assets
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			next.ServeHTTP(w, r)
			return
		}

		authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
		if authHeader == "" {
			if apiKeyPerms, ok := s.permissionsFromAPIKey(r); ok {
				ctx := context.WithValue(r.Context(), permissionsContextKey, apiKeyPerms)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			writeErr(w, http.StatusUnauthorized, "Missing Authorization header")
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			if apiKeyPerms, ok := s.permissionsFromAPIKey(r); ok {
				ctx := context.WithValue(r.Context(), permissionsContextKey, apiKeyPerms)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			writeErr(w, http.StatusUnauthorized, "Invalid Authorization header format")
			return
		}

		tokenString := parts[1]

		var jsonToken paseto.JSONToken
		v2 := paseto.NewV2()

		if err := v2.Decrypt(tokenString, getPasetoKey(s.config.JWTSecret), &jsonToken, nil); err != nil {
			if apiKeyPerms, ok := s.permissionsFromAPIKey(r); ok {
				ctx := context.WithValue(r.Context(), permissionsContextKey, apiKeyPerms)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			writeErr(w, http.StatusUnauthorized, "Invalid token")
			return
		}

		if time.Now().After(jsonToken.Expiration) {
			if apiKeyPerms, ok := s.permissionsFromAPIKey(r); ok {
				ctx := context.WithValue(r.Context(), permissionsContextKey, apiKeyPerms)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
			writeErr(w, http.StatusUnauthorized, "Token expired")
			return
		}

		// Extract permissions from token
		var perms core.PanelUserPermissions
		if permsStr := jsonToken.Get("perms"); permsStr != "" {
			json.Unmarshal([]byte(permsStr), &perms)
		}
		perms.Normalize()

		ctx := r.Context()
		ctx = context.WithValue(ctx, userContextKey, map[string]interface{}{"sub": jsonToken.Subject})
		ctx = context.WithValue(ctx, permissionsContextKey, &perms)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) permissionsFromAPIKey(r *http.Request) (*core.PanelUserPermissions, bool) {
	if s.config.APIKey == "" || r.Header.Get("X-API-Key") != s.config.APIKey {
		return nil, false
	}

	perms := &core.PanelUserPermissions{
		CanReadUsers:       true,
		CanWriteUsers:      !s.config.APIKeyReadOnly,
		CanReadWireguard:   true,
		CanWriteWireguard:  !s.config.APIKeyReadOnly,
		CanReadConfig:      true,
		CanWriteConfig:     !s.config.APIKeyReadOnly,
		CanReadSettings:    true,
		CanWriteSettings:   !s.config.APIKeyReadOnly,
		CanReadPanelUsers:  !s.config.APIKeyReadOnly,
		CanWritePanelUsers: !s.config.APIKeyReadOnly,
		CanReadLogs:        true,
	}
	perms.Normalize()
	return perms, true
}
