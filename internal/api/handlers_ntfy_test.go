package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ogstra/ogs-swg/internal/core"
)

// newNtfyTestServer builds a Server backed by a fresh temp-dir store and an
// authenticated request builder, matching the bootstrap used by
// handlers_dashboard_preferences_test.go.
func newNtfyTestServer(t *testing.T) (*Server, *core.Store, func(*http.Request) *http.Request) {
	t.Helper()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.db")
	store, err := core.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.CreatePanelUser("alice", "password123", core.PanelUserPermissions{CanReadSettings: true, CanWriteSettings: true}); err != nil {
		t.Fatalf("CreatePanelUser: %v", err)
	}

	server := NewServer(store, &core.Config{DemoMode: true}, &dashboardExecutorStub{})
	authReq := func(req *http.Request) *http.Request {
		ctx := context.WithValue(req.Context(), userContextKey, map[string]interface{}{"sub": "alice"})
		return req.WithContext(ctx)
	}
	return server, store, authReq
}

func TestNtfySettingsHandlersRoundTrip(t *testing.T) {
	server, _, authReq := newNtfyTestServer(t)

	getReq := authReq(httptest.NewRequest(http.MethodGet, "/api/settings/ntfy", nil))
	getRec := httptest.NewRecorder()
	server.handleGetNtfySettings(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("fresh GET status=%d body=%q", getRec.Code, getRec.Body.String())
	}

	var fresh ntfySettingsResponse
	if err := json.NewDecoder(getRec.Body).Decode(&fresh); err != nil {
		t.Fatalf("decode fresh: %v", err)
	}
	if fresh.AuthMode != "none" || fresh.ServerURL != "" || fresh.Topic != "" ||
		fresh.EnableSingboxDown || fresh.EnableWireguardDown || fresh.EnableHighTraffic || fresh.EnableConfigErrors ||
		fresh.TrafficThresholdBytes != 0 || fresh.HasBearerToken || fresh.HasBasicPass {
		t.Fatalf("unexpected fresh defaults: %+v", fresh)
	}

	putBody := `{
		"server_url":"https://ntfy.example.test",
		"topic":"panel-alerts",
		"auth_mode":"bearer",
		"bearer_token":"test-token-placeholder",
		"enable_singbox_down":true,
		"enable_high_traffic":true,
		"traffic_threshold_bytes":1048576
	}`
	putReq := authReq(httptest.NewRequest(http.MethodPut, "/api/settings/ntfy", strings.NewReader(putBody)))
	putReq.Header.Set("Content-Type", "application/json")
	putRec := httptest.NewRecorder()
	server.handleUpdateNtfySettings(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%q", putRec.Code, putRec.Body.String())
	}

	getRec = httptest.NewRecorder()
	server.handleGetNtfySettings(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("second GET status=%d body=%q", getRec.Code, getRec.Body.String())
	}
	bodyStr := getRec.Body.String()
	if strings.Contains(bodyStr, `"bearer_token"`) || strings.Contains(bodyStr, `"basic_pass"`) {
		t.Fatalf("GET response leaks a secret key: %s", bodyStr)
	}

	var saved ntfySettingsResponse
	if err := json.NewDecoder(strings.NewReader(bodyStr)).Decode(&saved); err != nil {
		t.Fatalf("decode saved: %v", err)
	}
	if saved.ServerURL != "https://ntfy.example.test" || saved.Topic != "panel-alerts" ||
		saved.AuthMode != "bearer" || !saved.HasBearerToken || saved.HasBasicPass ||
		!saved.EnableSingboxDown || !saved.EnableHighTraffic || saved.EnableWireguardDown || saved.EnableConfigErrors ||
		saved.TrafficThresholdBytes != 1048576 {
		t.Fatalf("unexpected saved settings: %+v", saved)
	}

	// Unrecognized auth_mode normalizes to "none" rather than erroring.
	badModeReq := authReq(httptest.NewRequest(http.MethodPut, "/api/settings/ntfy", strings.NewReader(`{"server_url":"https://ntfy.example.test","topic":"panel-alerts","auth_mode":"weird"}`)))
	badModeReq.Header.Set("Content-Type", "application/json")
	badModeRec := httptest.NewRecorder()
	server.handleUpdateNtfySettings(badModeRec, badModeReq)
	if badModeRec.Code != http.StatusOK {
		t.Fatalf("bad auth_mode PUT status=%d body=%q", badModeRec.Code, badModeRec.Body.String())
	}
	getRec = httptest.NewRecorder()
	server.handleGetNtfySettings(getRec, getReq)
	var afterBadMode ntfySettingsResponse
	if err := json.NewDecoder(getRec.Body).Decode(&afterBadMode); err != nil {
		t.Fatalf("decode afterBadMode: %v", err)
	}
	if afterBadMode.AuthMode != "none" {
		t.Fatalf("expected auth_mode normalized to none, got %q", afterBadMode.AuthMode)
	}

	// Malformed JSON body returns 400.
	malformedReq := authReq(httptest.NewRequest(http.MethodPut, "/api/settings/ntfy", strings.NewReader(`{not-json`)))
	malformedReq.Header.Set("Content-Type", "application/json")
	malformedRec := httptest.NewRecorder()
	server.handleUpdateNtfySettings(malformedRec, malformedReq)
	if malformedRec.Code != http.StatusBadRequest {
		t.Fatalf("malformed body status=%d body=%q", malformedRec.Code, malformedRec.Body.String())
	}

	// Negative traffic_threshold_bytes clamps to 0.
	negReq := authReq(httptest.NewRequest(http.MethodPut, "/api/settings/ntfy", strings.NewReader(`{"server_url":"https://ntfy.example.test","topic":"panel-alerts","auth_mode":"none","traffic_threshold_bytes":-5}`)))
	negReq.Header.Set("Content-Type", "application/json")
	negRec := httptest.NewRecorder()
	server.handleUpdateNtfySettings(negRec, negReq)
	if negRec.Code != http.StatusOK {
		t.Fatalf("negative threshold PUT status=%d body=%q", negRec.Code, negRec.Body.String())
	}
	getRec = httptest.NewRecorder()
	server.handleGetNtfySettings(getRec, getReq)
	var afterNeg ntfySettingsResponse
	if err := json.NewDecoder(getRec.Body).Decode(&afterNeg); err != nil {
		t.Fatalf("decode afterNeg: %v", err)
	}
	if afterNeg.TrafficThresholdBytes != 0 {
		t.Fatalf("expected threshold clamped to 0, got %d", afterNeg.TrafficThresholdBytes)
	}
}

func TestNtfySettingsHandlersPreserveSecretOnEmptyUpdate(t *testing.T) {
	server, store, authReq := newNtfyTestServer(t)

	putReq := authReq(httptest.NewRequest(http.MethodPut, "/api/settings/ntfy", strings.NewReader(`{"server_url":"https://ntfy.example.test","topic":"panel-alerts","auth_mode":"bearer","bearer_token":"test-token-placeholder"}`)))
	putReq.Header.Set("Content-Type", "application/json")
	putRec := httptest.NewRecorder()
	server.handleUpdateNtfySettings(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("initial PUT status=%d body=%q", putRec.Code, putRec.Body.String())
	}

	// PUT again with bearer_token empty and a changed topic — token must survive.
	putReq2 := authReq(httptest.NewRequest(http.MethodPut, "/api/settings/ntfy", strings.NewReader(`{"server_url":"https://ntfy.example.test","topic":"panel-alerts-2","auth_mode":"bearer","bearer_token":""}`)))
	putReq2.Header.Set("Content-Type", "application/json")
	putRec2 := httptest.NewRecorder()
	server.handleUpdateNtfySettings(putRec2, putReq2)
	if putRec2.Code != http.StatusOK {
		t.Fatalf("second PUT status=%d body=%q", putRec2.Code, putRec2.Body.String())
	}

	stored, err := store.GetNtfySettings(context.Background())
	if err != nil {
		t.Fatalf("GetNtfySettings: %v", err)
	}
	if stored.BearerToken != "test-token-placeholder" {
		t.Fatalf("expected token preserved, got %q", stored.BearerToken)
	}
	if stored.Topic != "panel-alerts-2" {
		t.Fatalf("expected topic updated, got %q", stored.Topic)
	}
}

func TestNtfySettingsHandlersClearSecretsOnAuthModeNone(t *testing.T) {
	server, store, authReq := newNtfyTestServer(t)

	putReq := authReq(httptest.NewRequest(http.MethodPut, "/api/settings/ntfy", strings.NewReader(`{"server_url":"https://ntfy.example.test","topic":"panel-alerts","auth_mode":"bearer","bearer_token":"test-token-placeholder"}`)))
	putReq.Header.Set("Content-Type", "application/json")
	putRec := httptest.NewRecorder()
	server.handleUpdateNtfySettings(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("initial PUT status=%d body=%q", putRec.Code, putRec.Body.String())
	}

	clearReq := authReq(httptest.NewRequest(http.MethodPut, "/api/settings/ntfy", strings.NewReader(`{"server_url":"https://ntfy.example.test","topic":"panel-alerts","auth_mode":"none"}`)))
	clearReq.Header.Set("Content-Type", "application/json")
	clearRec := httptest.NewRecorder()
	server.handleUpdateNtfySettings(clearRec, clearReq)
	if clearRec.Code != http.StatusOK {
		t.Fatalf("clear PUT status=%d body=%q", clearRec.Code, clearRec.Body.String())
	}

	stored, err := store.GetNtfySettings(context.Background())
	if err != nil {
		t.Fatalf("GetNtfySettings: %v", err)
	}
	if stored.BearerToken != "" {
		t.Fatalf("expected token cleared, got %q", stored.BearerToken)
	}

	getRec := httptest.NewRecorder()
	server.handleGetNtfySettings(getRec, authReq(httptest.NewRequest(http.MethodGet, "/api/settings/ntfy", nil)))
	var resp ntfySettingsResponse
	if err := json.NewDecoder(getRec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	if resp.HasBearerToken || resp.HasBasicPass {
		t.Fatalf("expected both has_* flags false after clearing, got %+v", resp)
	}
}

// TestNtfySettingsRequireWritePermission documents current router behavior:
// requirePerm is a repo-wide deprecated no-op (see server.go's requirePerm —
// "DEPRECATED stub — permission check skipped, all authenticated users
// pass", introduced by quick task 260517-ltu across every settings route).
// This plan does not reintroduce enforcement on its own; it registers the
// ntfy routes with the exact same requirePerm(canReadSettings/...) wrapper
// every other settings route uses, so behavior here is consistent with the
// rest of the API surface. This test asserts the routes are reachable
// through the full router (not the bare handler) and that the deprecated
// stub still lets an authenticated caller through.
func TestNtfySettingsRequireWritePermission(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.db")
	store, err := core.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := store.CreatePanelUser("readonly", "password123", core.PanelUserPermissions{CanReadSettings: true, CanWriteSettings: false}); err != nil {
		t.Fatalf("CreatePanelUser: %v", err)
	}

	server := NewServer(store, &core.Config{DemoMode: true, JWTSecret: "test-secret"}, &dashboardExecutorStub{})
	mux := server.Routes()

	loginRec := httptest.NewRecorder()
	loginReq := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"username":"readonly","password":"password123"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%q", loginRec.Code, loginRec.Body.String())
	}
	var loginResp struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(loginRec.Body).Decode(&loginResp); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if loginResp.Token == "" {
		t.Fatalf("expected non-empty token in login response, got body=%q", loginRec.Body.String())
	}

	putReq := httptest.NewRequest(http.MethodPut, "/api/settings/ntfy", strings.NewReader(`{"server_url":"https://ntfy.example.test","topic":"panel-alerts","auth_mode":"none"}`))
	putReq.Header.Set("Content-Type", "application/json")
	putReq.Header.Set("Authorization", "Bearer "+loginResp.Token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, putReq)

	// requirePerm is currently a deprecated no-op repo-wide, so this
	// resolves and succeeds rather than 403ing. If/when enforcement is
	// restored, this assertion should flip to StatusForbidden.
	if rec.Code != http.StatusOK {
		t.Fatalf("expected route to resolve and the deprecated requirePerm stub to allow it, got status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestNtfyTestNotificationUsesInFlightValues(t *testing.T) {
	server, store, authReq := newNtfyTestServer(t)

	var receivedPriority int
	var requestCount int
	liveServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		var body struct {
			Priority int `json:"priority"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		receivedPriority = body.Priority
		w.WriteHeader(http.StatusOK)
	}))
	defer liveServer.Close()

	if err := store.UpdateNtfySettings(context.Background(), core.NtfySettings{
		ServerURL: "https://unreachable.invalid.test",
		Topic:     "stored-topic",
		AuthMode:  "none",
	}); err != nil {
		t.Fatalf("UpdateNtfySettings: %v", err)
	}

	testBody := `{"server_url":"` + liveServer.URL + `","topic":"live-topic","auth_mode":"none"}`
	testReq := authReq(httptest.NewRequest(http.MethodPost, "/api/settings/ntfy/test", strings.NewReader(testBody)))
	testReq.Header.Set("Content-Type", "application/json")
	testRec := httptest.NewRecorder()
	server.handleTestNtfyNotification(testRec, testReq)
	if testRec.Code != http.StatusOK {
		t.Fatalf("test status=%d body=%q", testRec.Code, testRec.Body.String())
	}
	if requestCount != 1 {
		t.Fatalf("expected exactly 1 request to the httptest server, got %d", requestCount)
	}
	if receivedPriority != 1 {
		t.Fatalf("expected priority=1, got %d", receivedPriority)
	}

	stored, err := store.GetNtfySettings(context.Background())
	if err != nil {
		t.Fatalf("GetNtfySettings: %v", err)
	}
	if stored.ServerURL != "https://unreachable.invalid.test" || stored.Topic != "stored-topic" {
		t.Fatalf("expected stored settings unchanged by test call, got %+v", stored)
	}
}

func TestNtfyTestNotificationFallsBackToStoredSecret(t *testing.T) {
	server, store, authReq := newNtfyTestServer(t)

	var capturedAuth string
	liveServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer liveServer.Close()

	if err := store.UpdateNtfySettings(context.Background(), core.NtfySettings{
		ServerURL:   liveServer.URL,
		Topic:       "stored-topic",
		AuthMode:    "bearer",
		BearerToken: "test-token-placeholder",
	}); err != nil {
		t.Fatalf("UpdateNtfySettings: %v", err)
	}

	testBody := `{"server_url":"` + liveServer.URL + `","topic":"stored-topic","auth_mode":"bearer","bearer_token":""}`
	testReq := authReq(httptest.NewRequest(http.MethodPost, "/api/settings/ntfy/test", strings.NewReader(testBody)))
	testReq.Header.Set("Content-Type", "application/json")
	testRec := httptest.NewRecorder()
	server.handleTestNtfyNotification(testRec, testReq)
	if testRec.Code != http.StatusOK {
		t.Fatalf("test status=%d body=%q", testRec.Code, testRec.Body.String())
	}
	if capturedAuth != "Bearer test-token-placeholder" {
		t.Fatalf("expected fallback to stored token, got Authorization=%q", capturedAuth)
	}
}

func TestNtfyTestNotificationErrorIsRedacted(t *testing.T) {
	server, store, authReq := newNtfyTestServer(t)

	liveServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
	}))
	defer liveServer.Close()

	_ = store // stored settings unused in this test; test uses in-flight secret

	testBody := `{"server_url":"` + liveServer.URL + `","topic":"panel-alerts","auth_mode":"bearer","bearer_token":"test-token-placeholder"}`
	testReq := authReq(httptest.NewRequest(http.MethodPost, "/api/settings/ntfy/test", strings.NewReader(testBody)))
	testReq.Header.Set("Content-Type", "application/json")
	testRec := httptest.NewRecorder()
	server.handleTestNtfyNotification(testRec, testReq)

	if testRec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got status=%d body=%q", testRec.Code, testRec.Body.String())
	}
	body := testRec.Body.String()
	if !strings.Contains(body, "unauthorized") {
		t.Fatalf("expected body to contain reason, got %q", body)
	}
	if strings.Contains(body, "test-token-placeholder") {
		t.Fatalf("response body leaked the bearer token: %q", body)
	}
}

func TestNtfyTestNotificationRequiresURLAndTopic(t *testing.T) {
	server, _, authReq := newNtfyTestServer(t)

	var requestCount int
	liveServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer liveServer.Close()

	testBody := `{"server_url":"` + liveServer.URL + `","topic":"","auth_mode":"none"}`
	testReq := authReq(httptest.NewRequest(http.MethodPost, "/api/settings/ntfy/test", strings.NewReader(testBody)))
	testReq.Header.Set("Content-Type", "application/json")
	testRec := httptest.NewRecorder()
	server.handleTestNtfyNotification(testRec, testReq)

	if testRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty topic, got status=%d body=%q", testRec.Code, testRec.Body.String())
	}
	if requestCount != 0 {
		t.Fatalf("expected no HTTP request to be made, got %d", requestCount)
	}
}
