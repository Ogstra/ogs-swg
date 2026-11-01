package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
	"github.com/o1egl/paseto"
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

func loginToken(t *testing.T, srv *Server, username, password string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
	req.RemoteAddr = "192.0.2.10:4000"
	rec := httptest.NewRecorder()
	srv.handleLogin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%q", rec.Code, rec.Body.String())
	}
	var out LoginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.Token
}

func probeWithToken(srv *Server, token string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	srv.AuthMiddleware(authProbeHandler()).ServeHTTP(rec, req)
	return rec
}

func TestAuthMiddleware_TokenFollowsLiveAccountState(t *testing.T) {
	srv, st := newNtfyTestServerWithStore(t)
	srv.config.JWTSecret = "test-secret"
	if err := st.CreatePanelUser("carol", "old-password", core.PanelUserPermissions{CanReadUsers: true, CanWriteConfig: true}); err != nil {
		t.Fatal(err)
	}
	// Tokens carry second precision, so issue the old one in the past.
	token := mintTokenAt(t, srv, "carol", time.Now().Add(-10*time.Second))

	if rec := probeWithToken(srv, token); rec.Code != http.StatusOK {
		t.Fatalf("fresh token status=%d", rec.Code)
	}

	if err := st.UpdatePanelUserPermissions("carol", core.PanelUserPermissions{CanReadUsers: true}); err != nil {
		t.Fatal(err)
	}
	rec := probeWithToken(srv, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("token status after permission change = %d", rec.Code)
	}
	if decodePermissions(t, rec).CanWriteConfig {
		t.Fatal("token kept a permission that was removed")
	}

	if err := st.UpdatePanelUserPassword("carol", "new-password-1"); err != nil {
		t.Fatal(err)
	}
	if rec := probeWithToken(srv, token); rec.Code != http.StatusUnauthorized {
		t.Fatalf("token issued before a password change status=%d; want 401", rec.Code)
	}
	if rec := probeWithToken(srv, loginToken(t, srv, "carol", "new-password-1")); rec.Code != http.StatusOK {
		t.Fatalf("token issued after the password change status=%d", rec.Code)
	}

	if err := st.DeletePanelUser("carol"); err != nil {
		t.Fatal(err)
	}
	if rec := probeWithToken(srv, mintTokenAt(t, srv, "carol", time.Now())); rec.Code != http.StatusUnauthorized {
		t.Fatalf("deleted user's token status=%d; want 401", rec.Code)
	}
}

// mintTokenAt issues a valid token for username as if it had been created at issuedAt.
func mintTokenAt(t *testing.T, srv *Server, username string, issuedAt time.Time) string {
	t.Helper()
	jsonToken := paseto.JSONToken{Subject: username, IssuedAt: issuedAt, Expiration: issuedAt.Add(time.Hour)}
	token, err := paseto.NewV2().Encrypt(getPasetoKey(srv.config.JWTSecret), jsonToken, nil)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func TestHandleLogin_FailuresAreScopedToUsernameAndIP(t *testing.T) {
	srv, st := newNtfyTestServerWithStore(t)
	srv.config.JWTSecret = "test-secret"
	if err := st.CreatePanelUser("dave", "right-password", core.PanelUserPermissions{CanReadUsers: true}); err != nil {
		t.Fatal(err)
	}
	attempt := func(ip, password string) int {
		body, _ := json.Marshal(map[string]string{"username": "dave", "password": password})
		req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
		req.RemoteAddr = ip + ":4000"
		rec := httptest.NewRecorder()
		srv.handleLogin(rec, req)
		return rec.Code
	}

	for i := 0; i < loginMaxFailures; i++ {
		attempt("203.0.113.5", "wrong")
	}
	if got := attempt("203.0.113.5", "right-password"); got != http.StatusTooManyRequests {
		t.Fatalf("attacker address status=%d; want 429", got)
	}
	if got := attempt("198.51.100.7", "right-password"); got != http.StatusOK {
		t.Fatalf("real user from another address status=%d; want 200", got)
	}
}

func TestAuthMiddleware_WrongAPIKeysAreRateLimited(t *testing.T) {
	srv := NewServer(nil, &core.Config{APIKey: "k-demo", JWTSecret: "test-secret"}, nil)
	try := func(key string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
		req.RemoteAddr = "203.0.113.9:5000"
		req.Header.Set("X-API-Key", key)
		srv.AuthMiddleware(authProbeHandler()).ServeHTTP(rec, req)
		return rec.Code
	}
	for i := 0; i < loginMaxFailuresPerIP; i++ {
		if got := try("wrong"); got != http.StatusUnauthorized {
			t.Fatalf("wrong key status=%d", got)
		}
	}
	if got := try("k-demo"); got != http.StatusUnauthorized {
		t.Fatalf("correct key after the limit status=%d; want 401 while blocked", got)
	}
}
