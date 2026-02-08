package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token string `json:"token"`
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

	// Generate JWT
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": req.Username,
		"exp": time.Now().Add(24 * time.Hour).Unix(),
	})

	tokenString, err := token.SignedString([]byte(s.config.JWTSecret))
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
	claims, ok := r.Context().Value("user").(jwt.MapClaims)
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
	claims, ok := r.Context().Value("user").(jwt.MapClaims)
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

// AuthMiddleware validates the JWT token
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
		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return []byte(s.config.JWTSecret), nil
		})

		if err != nil || !token.Valid {
			http.Error(w, "Invalid or expired token", http.StatusUnauthorized)
			return
		}

		// Token is valid, proceed
		ctx := context.WithValue(r.Context(), "user", token.Claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
