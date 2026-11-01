package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
)

func twoFactorRequest(t *testing.T, srv *Server, handler http.HandlerFunc, method, path, username string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.RemoteAddr = "192.0.2.20:4000"
	ctx := context.WithValue(req.Context(), userContextKey, map[string]interface{}{"sub": username})
	rec := httptest.NewRecorder()
	handler(rec, req.WithContext(ctx))
	return rec
}

func loginWithOTP(srv *Server, username, password, otp string) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(map[string]string{"username": username, "password": password, "otp": otp})
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(raw))
	req.RemoteAddr = "192.0.2.20:4000"
	rec := httptest.NewRecorder()
	srv.handleLogin(rec, req)
	return rec
}

func enableTwoFactorForTest(t *testing.T, srv *Server, username string) (secret string, recovery []string) {
	t.Helper()
	rec := twoFactorRequest(t, srv, srv.handleTwoFactorSetup, http.MethodPost, "/api/auth/2fa/setup", username, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("setup status=%d body=%q", rec.Code, rec.Body.String())
	}
	var setup twoFactorSetupResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &setup); err != nil {
		t.Fatal(err)
	}
	code, _ := core.TOTPCodeAt(setup.Secret, time.Now())
	rec = twoFactorRequest(t, srv, srv.handleTwoFactorEnable, http.MethodPost, "/api/auth/2fa/enable", username, twoFactorCodeRequest{Code: code})
	if rec.Code != http.StatusOK {
		t.Fatalf("enable status=%d body=%q", rec.Code, rec.Body.String())
	}
	var enabled twoFactorEnableResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &enabled); err != nil {
		t.Fatal(err)
	}
	if len(enabled.RecoveryCodes) != recoveryCodeCount {
		t.Fatalf("recovery codes = %d", len(enabled.RecoveryCodes))
	}
	return setup.Secret, enabled.RecoveryCodes
}

func TestLogin_TwoFactorFlow(t *testing.T) {
	srv, st := newNtfyTestServerWithStore(t)
	srv.config.JWTSecret = "test-secret"
	if err := st.CreatePanelUser("erin", "pw-erin-123", core.PanelUserPermissions{CanReadUsers: true}); err != nil {
		t.Fatal(err)
	}

	if rec := loginWithOTP(srv, "erin", "pw-erin-123", ""); rec.Code != http.StatusOK {
		t.Fatalf("login before enabling 2FA status=%d", rec.Code)
	}

	secret, recovery := enableTwoFactorForTest(t, srv, "erin")

	rec := loginWithOTP(srv, "erin", "pw-erin-123", "")
	if rec.Code != http.StatusUnauthorized || rec.Header().Get(otpRequiredHeader) != "true" {
		t.Fatalf("login without code status=%d header=%q", rec.Code, rec.Header().Get(otpRequiredHeader))
	}
	if rec := loginWithOTP(srv, "erin", "pw-erin-123", "000000"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("login with a wrong code status=%d", rec.Code)
	}

	// The enable step already used the current time step, so a code for the
	// next one stands in for the user's next login.
	next, _ := core.TOTPCodeAt(secret, time.Now().Add(30*time.Second))
	if rec := loginWithOTP(srv, "erin", "pw-erin-123", next); rec.Code != http.StatusOK {
		t.Fatalf("login with a valid code status=%d body=%q", rec.Code, rec.Body.String())
	}
	if rec := loginWithOTP(srv, "erin", "pw-erin-123", next); rec.Code != http.StatusUnauthorized {
		t.Fatalf("replayed code status=%d; want 401", rec.Code)
	}

	if rec := loginWithOTP(srv, "erin", "pw-erin-123", recovery[0]); rec.Code != http.StatusOK {
		t.Fatalf("recovery code status=%d", rec.Code)
	}
	if rec := loginWithOTP(srv, "erin", "pw-erin-123", recovery[0]); rec.Code != http.StatusUnauthorized {
		t.Fatalf("reused recovery code status=%d; want 401", rec.Code)
	}
	if rec := loginWithOTP(srv, "erin", "wrong-password", recovery[1]); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password with a recovery code status=%d", rec.Code)
	}
}

func TestTwoFactor_DisableNeedsPasswordAndCode(t *testing.T) {
	srv, st := newNtfyTestServerWithStore(t)
	srv.config.JWTSecret = "test-secret"
	if err := st.CreatePanelUser("frank", "pw-frank-123", core.PanelUserPermissions{CanReadUsers: true}); err != nil {
		t.Fatal(err)
	}
	secret, _ := enableTwoFactorForTest(t, srv, "frank")
	disable := func(password, code string) int {
		return twoFactorRequest(t, srv, srv.handleTwoFactorDisable, http.MethodPost, "/api/auth/2fa/disable", "frank",
			twoFactorDisableRequest{CurrentPassword: password, Code: code}).Code
	}

	next, _ := core.TOTPCodeAt(secret, time.Now().Add(30*time.Second))
	if got := disable("wrong", next); got != http.StatusForbidden {
		t.Fatalf("disable with a wrong password status=%d", got)
	}
	if got := disable("pw-frank-123", "000000"); got != http.StatusBadRequest {
		t.Fatalf("disable with a wrong code status=%d", got)
	}
	if got := disable("pw-frank-123", next); got != http.StatusNoContent {
		t.Fatalf("disable status=%d", got)
	}
	if rec := loginWithOTP(srv, "frank", "pw-frank-123", ""); rec.Code != http.StatusOK {
		t.Fatalf("login after disabling status=%d", rec.Code)
	}
}

func TestTwoFactor_AdminReset(t *testing.T) {
	srv, st := newNtfyTestServerWithStore(t)
	srv.config.JWTSecret = "test-secret"
	if err := st.CreatePanelUser("gina", "pw-gina-123", core.PanelUserPermissions{CanReadUsers: true}); err != nil {
		t.Fatal(err)
	}
	enableTwoFactorForTest(t, srv, "gina")

	req := httptest.NewRequest(http.MethodDelete, "/api/panel-users/2fa?username=gina", nil)
	rec := httptest.NewRecorder()
	srv.handleResetPanelUserTwoFactor(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("reset status=%d", rec.Code)
	}
	if rec := loginWithOTP(srv, "gina", "pw-gina-123", ""); rec.Code != http.StatusOK {
		t.Fatalf("login after an admin reset status=%d", rec.Code)
	}
}

func TestTwoFactor_NewRecoveryCodesReplaceTheOldOnes(t *testing.T) {
	srv, st := newNtfyTestServerWithStore(t)
	srv.config.JWTSecret = "test-secret"
	if err := st.CreatePanelUser("june", "pw-june-123", core.PanelUserPermissions{CanReadUsers: true}); err != nil {
		t.Fatal(err)
	}
	secret, oldCodes := enableTwoFactorForTest(t, srv, "june")

	next, _ := core.TOTPCodeAt(secret, time.Now().Add(30*time.Second))
	rec := twoFactorRequest(t, srv, srv.handleTwoFactorNewRecoveryCodes, http.MethodPost, "/api/auth/2fa/recovery-codes", "june",
		twoFactorDisableRequest{CurrentPassword: "pw-june-123", Code: next})
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
	var fresh twoFactorEnableResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &fresh); err != nil {
		t.Fatal(err)
	}
	if len(fresh.RecoveryCodes) != recoveryCodeCount {
		t.Fatalf("got %d codes", len(fresh.RecoveryCodes))
	}
	if rec := loginWithOTP(srv, "june", "pw-june-123", oldCodes[0]); rec.Code != http.StatusUnauthorized {
		t.Fatalf("old recovery code status=%d; want 401", rec.Code)
	}
	if rec := loginWithOTP(srv, "june", "pw-june-123", fresh.RecoveryCodes[0]); rec.Code != http.StatusOK {
		t.Fatalf("new recovery code status=%d; want 200", rec.Code)
	}
}

func TestSignOutEverywhere_RevokesExistingTokens(t *testing.T) {
	srv, st := newNtfyTestServerWithStore(t)
	srv.config.JWTSecret = "test-secret"
	if err := st.CreatePanelUser("kira", "pw-kira-123", core.PanelUserPermissions{CanReadUsers: true}); err != nil {
		t.Fatal(err)
	}
	old := mintTokenAt(t, srv, "kira", time.Now().Add(-time.Minute))
	if rec := probeWithToken(srv, old); rec.Code != http.StatusOK {
		t.Fatalf("token before sign-out status=%d", rec.Code)
	}
	rec := twoFactorRequest(t, srv, srv.handleSignOutEverywhere, http.MethodPost, "/api/auth/sign-out-everywhere", "kira", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("sign-out status=%d", rec.Code)
	}
	if rec := probeWithToken(srv, old); rec.Code != http.StatusUnauthorized {
		t.Fatalf("token after sign-out status=%d; want 401", rec.Code)
	}
}
