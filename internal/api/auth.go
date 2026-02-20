package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/o1egl/paseto"
)

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token string `json:"token"`
}

func getPasetoKey(secret string) []byte {
	key := make([]byte, 32)
	copy(key, []byte(secret))
	return key
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	valid, err := s.store.VerifyAdmin(req.Username, req.Password)
	if err != nil {
		http.Error(w, "Authentication error", http.StatusInternalServerError)
		return
	}

	if !valid {
		// Fallback to legacy config for migration if DB is empty (should be handled by EnsureDefaultAdmin, but safe to check)
		// Actually, EnsureDefaultAdmin handles creation, so we should strictly enforce DB auth.
		// However, if the user explicitly provided credentials in Config that differ from DB, DB wins.
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	// Generate PASETO token
	v2 := paseto.NewV2()
	now := time.Now()
	jsonToken := paseto.JSONToken{
		Subject:    req.Username,
		IssuedAt:   now,
		Expiration: now.Add(24 * time.Hour),
	}

	tokenString, err := v2.Encrypt(getPasetoKey(s.config.JWTSecret), jsonToken, nil)
	if err != nil {
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(LoginResponse{Token: tokenString})
}

type UpdatePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (s *Server) handleUpdatePassword(w http.ResponseWriter, r *http.Request) {
	var req UpdatePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if len(req.NewPassword) < 8 {
		http.Error(w, "Password must be at least 8 characters", http.StatusBadRequest)
		return
	}

	// Get username from context (set by AuthMiddleware)
	claims, ok := r.Context().Value("user").(map[string]interface{})
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	username, ok := claims["sub"].(string)
	if !ok {
		http.Error(w, "Invalid token claims", http.StatusUnauthorized)
		return
	}

	// Verify current password
	valid, err := s.store.VerifyAdmin(username, req.CurrentPassword)
	if err != nil {
		http.Error(w, "Verification error", http.StatusInternalServerError)
		return
	}
	if !valid {
		http.Error(w, "Invalid current password", http.StatusUnauthorized)
		return
	}

	// Update password
	if err := s.store.UpdateAdminPassword(username, req.NewPassword); err != nil {
		http.Error(w, "Failed to update password", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

type UpdateUsernameRequest struct {
	CurrentPassword string `json:"current_password"`
	NewUsername     string `json:"new_username"`
}

func (s *Server) handleUpdateUsername(w http.ResponseWriter, r *http.Request) {
	var req UpdateUsernameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.NewUsername == "" {
		http.Error(w, "New username is required", http.StatusBadRequest)
		return
	}

	// Get username from context
	claims, ok := r.Context().Value("user").(map[string]interface{})
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	currentUsername, ok := claims["sub"].(string)
	if !ok {
		http.Error(w, "Invalid token claims", http.StatusUnauthorized)
		return
	}

	// Verify current password
	valid, err := s.store.VerifyAdmin(currentUsername, req.CurrentPassword)
	if err != nil {
		http.Error(w, "Verification error", http.StatusInternalServerError)
		return
	}
	if !valid {
		http.Error(w, "Invalid current password", http.StatusUnauthorized)
		return
	}

	// Update username
	if err := s.store.UpdateAdminUsername(currentUsername, req.NewUsername); err != nil {
		if strings.Contains(err.Error(), "already exists") {
			http.Error(w, err.Error(), http.StatusConflict) // 409 Conflict
			return
		}
		http.Error(w, "Failed to update username: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// AuthMiddleware validates the PASETO token
func (s *Server) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow login endpoint without token
		if r.URL.Path == "/api/login" {
			next.ServeHTTP(w, r)
			return
		}

		// Allow public assets if needed (though usually served by static handler)
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			next.ServeHTTP(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			// Fallback to API Key for legacy/script compatibility
			if s.config.APIKey != "" && r.Header.Get("X-API-Key") == s.config.APIKey {
				next.ServeHTTP(w, r)
				return
			}
			http.Error(w, "Missing Authorization header", http.StatusUnauthorized)
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			http.Error(w, "Invalid Authorization header format", http.StatusUnauthorized)
			return
		}

		tokenString := parts[1]

		var jsonToken paseto.JSONToken
		v2 := paseto.NewV2()

		if err := v2.Decrypt(tokenString, getPasetoKey(s.config.JWTSecret), &jsonToken, nil); err != nil {
			http.Error(w, "Invalid token", http.StatusUnauthorized)
			return
		}

		if time.Now().After(jsonToken.Expiration) {
			http.Error(w, "Token expired", http.StatusUnauthorized)
			return
		}

		// Token is valid, proceed
		ctx := context.WithValue(r.Context(), "user", map[string]interface{}{"sub": jsonToken.Subject})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
