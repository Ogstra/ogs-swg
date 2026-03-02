package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Ogstra/ogs-swg/internal/core"
)

func TestAuthMiddleware_APIKeyPermissionsRespectReadOnlyFlag(t *testing.T) {
	t.Run("read-write api key can manage panel users", func(t *testing.T) {
		srv := NewServer(nil, &core.Config{
			APIKey:         "k-demo",
			APIKeyReadOnly: false,
			JWTSecret:      "test-secret",
		}, nil)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		req.Header.Set("X-API-Key", "k-demo")
		srv.AuthMiddleware(authProbeHandler()).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
		}
		perms := decodePermissions(t, rec)
		if !perms.CanWritePanelUsers {
			t.Fatalf("expected CanWritePanelUsers=true, got false")
		}
	})

	t.Run("read-only api key cannot manage panel users", func(t *testing.T) {
		srv := NewServer(nil, &core.Config{
			APIKey:         "k-demo",
			APIKeyReadOnly: true,
			JWTSecret:      "test-secret",
		}, nil)

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		req.Header.Set("X-API-Key", "k-demo")
		srv.AuthMiddleware(authProbeHandler()).ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
		}
		perms := decodePermissions(t, rec)
		if perms.CanWritePanelUsers {
			t.Fatalf("expected CanWritePanelUsers=false, got true")
		}
	})
}

func TestAuthMiddleware_FallsBackToAPIKeyWhenBearerInvalid(t *testing.T) {
	srv := NewServer(nil, &core.Config{
		APIKey:         "k-demo",
		APIKeyReadOnly: false,
		JWTSecret:      "test-secret",
	}, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.Header.Set("Authorization", "Bearer invalid.token.value")
	req.Header.Set("X-API-Key", "k-demo")
	srv.AuthMiddleware(authProbeHandler()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
	perms := decodePermissions(t, rec)
	if !perms.CanWriteConfig {
		t.Fatalf("expected API-key permissions to be applied")
	}
}

func TestHandleLogin_DisabledReturnsForbidden(t *testing.T) {
	srv := NewServer(nil, &core.Config{
		DisablePasswordLogin: true,
		JWTSecret:            "test-secret",
	}, nil)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/login",
		bytes.NewBufferString(`{"username":"demo","password":"demo"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	srv.handleLogin(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "disabled") {
		t.Fatalf("expected disabled error, got body=%q", rec.Body.String())
	}
}

func authProbeHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		perms := getPermissions(r)
		if perms == nil {
			http.Error(w, "permissions missing", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(perms)
	})
}

func decodePermissions(t *testing.T, rec *httptest.ResponseRecorder) core.PanelUserPermissions {
	t.Helper()
	var perms core.PanelUserPermissions
	if err := json.NewDecoder(rec.Body).Decode(&perms); err != nil {
		t.Fatalf("decode perms: %v body=%q", err, rec.Body.String())
	}
	return perms
}
