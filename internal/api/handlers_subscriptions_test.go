package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/Ogstra/ogs-swg/internal/core"
)

type subscriptionMutationRequest struct {
	Name                       string   `json:"name"`
	QuotaLimit                 int64    `json:"quota_limit"`
	QuotaPeriod                string   `json:"quota_period"`
	Users                      []string `json:"users"`
	ProfileUpdateIntervalHours *int64   `json:"profile_update_interval_hours,omitempty"`
	UpdateAlways               *bool    `json:"update_always,omitempty"`
}

type subscriptionCreateResponse struct {
	ID    int64  `json:"id"`
	Token string `json:"token"`
}

type subscriptionDetailResponse struct {
	ID                         int64    `json:"id"`
	Token                      *string  `json:"token"`
	Name                       string   `json:"name"`
	QuotaLimit                 int64    `json:"quota_limit"`
	QuotaPeriod                string   `json:"quota_period"`
	UsedBytes                  int64    `json:"used_bytes"`
	Users                      []string `json:"users"`
	ProfileUpdateIntervalHours *int64   `json:"profile_update_interval_hours"`
	UpdateAlways               bool     `json:"update_always"`
	LastRequestAt              *int64   `json:"last_request_at"`
	CreatedAt                  int64    `json:"created_at"`
	UpdatedAt                  int64    `json:"updated_at"`
}

type subscriptionDefaultsResponse struct {
	ProfileUpdateIntervalHours *int64   `json:"profile_update_interval_hours"`
	UpdateAlways               bool     `json:"update_always"`
	Destinations               []string `json:"destinations"`
}

type subscriptionDefaultDestinationsResponse struct {
	Destinations []string `json:"destinations"`
}

type loginTestResponse struct {
	Token string `json:"token"`
}

func withSubscriptionPerms(r *http.Request, perms *core.PanelUserPermissions) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), permissionsContextKey, perms))
}

func TestHandleGetSubscriptionDefaults_ReturnsProductDefaultsWhenUserHasNoSavedDefaults(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServer(t)
	createSubscriptionPanelUserForTest(t, dataStore, "alice-panel", "secret", core.PanelUserPermissions{CanReadUsers: true, CanWriteUsers: true})

	rec := performSubscriptionDefaultsRequest(t, server, http.MethodGet, "/api/subscriptions/defaults", nil, subscriptionAuthHeadersForUser(t, server, "alice-panel", "secret"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}

	var got subscriptionDefaultsResponse
	decodeJSONResponse(t, rec, &got)
	if got.ProfileUpdateIntervalHours != nil {
		t.Fatalf("profile_update_interval_hours=%v want nil", *got.ProfileUpdateIntervalHours)
	}
	if got.UpdateAlways {
		t.Fatalf("update_always=%v want false", got.UpdateAlways)
	}
	if len(got.Destinations) != 0 {
		t.Fatalf("destinations=%v want empty", got.Destinations)
	}
}

func TestHandleUpdateSubscriptionDefaults_PersistsForAuthenticatedPanelUser(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServer(t)
	createSubscriptionPanelUserForTest(t, dataStore, "alice-panel", "secret", core.PanelUserPermissions{CanReadUsers: true, CanWriteUsers: true})

	interval := int64(24)
	updateRec := performSubscriptionDefaultsRequest(t, server, http.MethodPut, "/api/subscriptions/defaults", subscriptionDefaultsResponse{
		ProfileUpdateIntervalHours: &interval,
		UpdateAlways:               true,
		Destinations:               []string{"edge.example.com:443", "dns.example.net:853"},
	}, subscriptionAuthHeadersForUser(t, server, "alice-panel", "secret"))
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%q", updateRec.Code, updateRec.Body.String())
	}

	getRec := performSubscriptionDefaultsRequest(t, server, http.MethodGet, "/api/subscriptions/defaults", nil, subscriptionAuthHeadersForUser(t, server, "alice-panel", "secret"))
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%q", getRec.Code, getRec.Body.String())
	}

	var got subscriptionDefaultsResponse
	decodeJSONResponse(t, getRec, &got)
	if got.ProfileUpdateIntervalHours == nil || *got.ProfileUpdateIntervalHours != interval {
		t.Fatalf("profile_update_interval_hours=%v want %d", got.ProfileUpdateIntervalHours, interval)
	}
	if !got.UpdateAlways {
		t.Fatalf("update_always=%v want true", got.UpdateAlways)
	}
	if len(got.Destinations) != 2 || got.Destinations[0] != "edge.example.com:443" || got.Destinations[1] != "dns.example.net:853" {
		t.Fatalf("destinations=%v want persisted ordered values", got.Destinations)
	}
}

func TestSubscriptionDefaultsArePerPanelUser(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServer(t)
	perms := core.PanelUserPermissions{CanReadUsers: true, CanWriteUsers: true}
	createSubscriptionPanelUserForTest(t, dataStore, "alice-panel", "secret", perms)
	createSubscriptionPanelUserForTest(t, dataStore, "bob-panel", "secret", perms)

	aliceInterval := int64(12)
	aliceHeaders := subscriptionAuthHeadersForUser(t, server, "alice-panel", "secret")
	bobHeaders := subscriptionAuthHeadersForUser(t, server, "bob-panel", "secret")

	aliceUpdate := performSubscriptionDefaultsRequest(t, server, http.MethodPut, "/api/subscriptions/defaults", subscriptionDefaultsResponse{
		ProfileUpdateIntervalHours: &aliceInterval,
		UpdateAlways:               true,
		Destinations:               []string{"alpha.example.com:443"},
	}, aliceHeaders)
	if aliceUpdate.Code != http.StatusOK {
		t.Fatalf("alice update status=%d body=%q", aliceUpdate.Code, aliceUpdate.Body.String())
	}

	bobGet := performSubscriptionDefaultsRequest(t, server, http.MethodGet, "/api/subscriptions/defaults", nil, bobHeaders)
	if bobGet.Code != http.StatusOK {
		t.Fatalf("bob get status=%d body=%q", bobGet.Code, bobGet.Body.String())
	}

	var bobDefaults subscriptionDefaultsResponse
	decodeJSONResponse(t, bobGet, &bobDefaults)
	if bobDefaults.ProfileUpdateIntervalHours != nil || bobDefaults.UpdateAlways || len(bobDefaults.Destinations) != 0 {
		t.Fatalf("bob defaults=%+v want untouched product defaults", bobDefaults)
	}

	bobInterval := int64(48)
	bobUpdate := performSubscriptionDefaultsRequest(t, server, http.MethodPut, "/api/subscriptions/defaults", subscriptionDefaultsResponse{
		ProfileUpdateIntervalHours: &bobInterval,
		UpdateAlways:               false,
		Destinations:               []string{"beta.example.com:8443"},
	}, bobHeaders)
	if bobUpdate.Code != http.StatusOK {
		t.Fatalf("bob update status=%d body=%q", bobUpdate.Code, bobUpdate.Body.String())
	}

	aliceGet := performSubscriptionDefaultsRequest(t, server, http.MethodGet, "/api/subscriptions/defaults", nil, aliceHeaders)
	if aliceGet.Code != http.StatusOK {
		t.Fatalf("alice get status=%d body=%q", aliceGet.Code, aliceGet.Body.String())
	}

	var aliceDefaults subscriptionDefaultsResponse
	decodeJSONResponse(t, aliceGet, &aliceDefaults)
	if aliceDefaults.ProfileUpdateIntervalHours == nil || *aliceDefaults.ProfileUpdateIntervalHours != aliceInterval {
		t.Fatalf("alice profile_update_interval_hours=%v want %d", aliceDefaults.ProfileUpdateIntervalHours, aliceInterval)
	}
	if !aliceDefaults.UpdateAlways {
		t.Fatalf("alice update_always=%v want true", aliceDefaults.UpdateAlways)
	}
	if len(aliceDefaults.Destinations) != 1 || aliceDefaults.Destinations[0] != "alpha.example.com:443" {
		t.Fatalf("alice destinations=%v want [alpha.example.com:443]", aliceDefaults.Destinations)
	}
}

func TestHandleSubscriptionDefaults_RequirePanelUserToken(t *testing.T) {
	server, _ := newPublicSubscriptionTestServer(t)
	server.config.APIKey = "test-api-key"

	t.Run("missing token", func(t *testing.T) {
		rec := performSubscriptionDefaultsRequest(t, server, http.MethodGet, "/api/subscriptions/defaults", nil, nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d body=%q want %d", rec.Code, rec.Body.String(), http.StatusUnauthorized)
		}
	})

	t.Run("api key auth has no panel identity", func(t *testing.T) {
		rec := performSubscriptionDefaultsRequest(t, server, http.MethodGet, "/api/subscriptions/defaults", nil, map[string]string{
			"X-API-Key": "test-api-key",
		})
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d body=%q want %d", rec.Code, rec.Body.String(), http.StatusUnauthorized)
		}
	})
}

func TestHandleGetSubscriptionDefaultDestinations(t *testing.T) {
	t.Run("returns normalized deduplicated recent destinations", func(t *testing.T) {
		server, _ := newLogsTestServer(t, []string{
			"2026/04/07 10:00:00 [OGS] inbound connection to edge.example.com:443",
			"2026/04/07 10:01:00 [OGS] inbound packet connection to resolver.example.net:53",
			"2026/04/07 10:02:00 [OGS] inbound connection to EDGE.EXAMPLE.COM:443",
			"2026/04/07 10:03:00 [OGS] inbound connection to 127.0.0.1:8080",
			"2026/04/07 10:04:00 outbound/direct connection to ignored.example.org:443",
			"2026/04/07 10:05:00 [OGS] inbound connection to bad-destination",
		})
		server.config.JWTSecret = "subscription-default-destinations-secret"
		server.store.CreatePanelUser("alice-panel", "secret", core.PanelUserPermissions{CanReadUsers: true, CanWriteUsers: true})

		rec := performSubscriptionDefaultsRequest(t, server, http.MethodGet, "/api/subscriptions/default-destinations", nil, subscriptionAuthHeadersForUser(t, server, "alice-panel", "secret"))
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
		}

		var got subscriptionDefaultDestinationsResponse
		decodeJSONResponse(t, rec, &got)
		want := []string{"edge.example.com:443", "resolver.example.net:53"}
		if len(got.Destinations) != len(want) {
			t.Fatalf("destinations=%v want %v", got.Destinations, want)
		}
		for i := range want {
			if got.Destinations[i] != want[i] {
				t.Fatalf("destinations[%d]=%q want %q (full=%v)", i, got.Destinations[i], want[i], got.Destinations)
			}
		}
	})

	t.Run("returns empty list when logs unavailable", func(t *testing.T) {
		server, _ := newLogsTestServer(t, nil)
		server.config.LogSource = "file"
		server.config.AccessLogPath = "/nonexistent/subscription-defaults.log"
		server.config.JWTSecret = "subscription-default-destinations-secret"
		server.store.CreatePanelUser("alice-panel", "secret", core.PanelUserPermissions{CanReadUsers: true, CanWriteUsers: true})

		rec := performSubscriptionDefaultsRequest(t, server, http.MethodGet, "/api/subscriptions/default-destinations", nil, subscriptionAuthHeadersForUser(t, server, "alice-panel", "secret"))
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
		}

		var got subscriptionDefaultDestinationsResponse
		decodeJSONResponse(t, rec, &got)
		if len(got.Destinations) != 0 {
			t.Fatalf("destinations=%v want empty", got.Destinations)
		}
	})
}

func TestSubscriptionCreateAndGetRefreshPolicyRoundTrip(t *testing.T) {
	server, _ := newPublicSubscriptionTestServer(t)

	interval := int64(6)
	updateAlways := true
	createReq := subscriptionMutationRequest{
		Name:                       "Alpha Bundle",
		QuotaLimit:                 0,
		QuotaPeriod:                "monthly",
		Users:                      []string{"alice"},
		ProfileUpdateIntervalHours: &interval,
		UpdateAlways:               &updateAlways,
	}

	created := createSubscriptionForTest(t, server, createReq)
	got := getSubscriptionForTest(t, server, created.ID)

	if got.ProfileUpdateIntervalHours == nil {
		t.Fatalf("profile_update_interval_hours=nil want %d", interval)
	}
	if *got.ProfileUpdateIntervalHours != interval {
		t.Fatalf("profile_update_interval_hours=%d want %d", *got.ProfileUpdateIntervalHours, interval)
	}
	if !got.UpdateAlways {
		t.Fatalf("update_always=%v want true", got.UpdateAlways)
	}
}

func TestSubscriptionCreateDefaultsRefreshPolicyWhenOmitted(t *testing.T) {
	server, _ := newPublicSubscriptionTestServer(t)

	created := createSubscriptionForTest(t, server, subscriptionMutationRequest{
		Name:        "Default Bundle",
		QuotaLimit:  0,
		QuotaPeriod: "monthly",
		Users:       []string{"alice"},
	})
	got := getSubscriptionForTest(t, server, created.ID)

	if got.ProfileUpdateIntervalHours != nil {
		t.Fatalf("profile_update_interval_hours=%v want nil", *got.ProfileUpdateIntervalHours)
	}
	if got.UpdateAlways {
		t.Fatalf("update_always=%v want false", got.UpdateAlways)
	}
}

func TestSubscriptionUpdateRefreshPolicyPreservesExistingValuesWhenOmitted(t *testing.T) {
	server, _ := newPublicSubscriptionTestServer(t)

	initialInterval := int64(6)
	initialUpdateAlways := true
	created := createSubscriptionForTest(t, server, subscriptionMutationRequest{
		Name:                       "Mutable Bundle",
		QuotaLimit:                 0,
		QuotaPeriod:                "monthly",
		Users:                      []string{"alice"},
		ProfileUpdateIntervalHours: &initialInterval,
		UpdateAlways:               &initialUpdateAlways,
	})

	updatedInterval := int64(12)
	updatedUpdateAlways := false
	updateSubscriptionForTest(t, server, created.ID, subscriptionMutationRequest{
		Name:                       "Mutable Bundle",
		QuotaLimit:                 1024,
		QuotaPeriod:                "monthly",
		Users:                      []string{"alice"},
		ProfileUpdateIntervalHours: &updatedInterval,
		UpdateAlways:               &updatedUpdateAlways,
	})

	got := getSubscriptionForTest(t, server, created.ID)
	if got.ProfileUpdateIntervalHours == nil || *got.ProfileUpdateIntervalHours != updatedInterval {
		t.Fatalf("after explicit update profile_update_interval_hours=%v want %d", got.ProfileUpdateIntervalHours, updatedInterval)
	}
	if got.UpdateAlways != updatedUpdateAlways {
		t.Fatalf("after explicit update update_always=%v want %v", got.UpdateAlways, updatedUpdateAlways)
	}

	updateSubscriptionForTest(t, server, created.ID, subscriptionMutationRequest{
		Name:        "Mutable Bundle Renamed",
		QuotaLimit:  2048,
		QuotaPeriod: "monthly",
		Users:       []string{"alice"},
	})

	got = getSubscriptionForTest(t, server, created.ID)
	if got.ProfileUpdateIntervalHours == nil || *got.ProfileUpdateIntervalHours != updatedInterval {
		t.Fatalf("after omitted update profile_update_interval_hours=%v want %d", got.ProfileUpdateIntervalHours, updatedInterval)
	}
	if got.UpdateAlways != updatedUpdateAlways {
		t.Fatalf("after omitted update update_always=%v want %v", got.UpdateAlways, updatedUpdateAlways)
	}
}

func TestSubscriptionUpdateRefreshPolicyClearsIntervalWhenExplicitNull(t *testing.T) {
	server, _ := newPublicSubscriptionTestServer(t)

	initialInterval := int64(8)
	initialUpdateAlways := true
	created := createSubscriptionForTest(t, server, subscriptionMutationRequest{
		Name:                       "Clearable Bundle",
		QuotaLimit:                 0,
		QuotaPeriod:                "monthly",
		Users:                      []string{"alice"},
		ProfileUpdateIntervalHours: &initialInterval,
		UpdateAlways:               &initialUpdateAlways,
	})

	updateSubscriptionForTestBody(t, server, created.ID, map[string]any{
		"name":                          "Clearable Bundle",
		"quota_limit":                   int64(0),
		"quota_period":                  "monthly",
		"users":                         []string{"alice"},
		"profile_update_interval_hours": nil,
	})

	got := getSubscriptionForTest(t, server, created.ID)
	if got.ProfileUpdateIntervalHours != nil {
		t.Fatalf("after explicit null profile_update_interval_hours=%v want nil", *got.ProfileUpdateIntervalHours)
	}
	if got.UpdateAlways != initialUpdateAlways {
		t.Fatalf("after explicit null update_always=%v want %v", got.UpdateAlways, initialUpdateAlways)
	}
}

func TestGetSubscription_HidesTokenForReadOnlyCallers(t *testing.T) {
	server, _ := newPublicSubscriptionTestServer(t)
	created := createSubscriptionForTest(t, server, subscriptionMutationRequest{
		Name:        "Read Only Bundle",
		QuotaLimit:  0,
		QuotaPeriod: "monthly",
		Users:       []string{"alice"},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/subscriptions/"+strconv.FormatInt(created.ID, 10), nil)
	req.SetPathValue("id", strconv.FormatInt(created.ID, 10))
	req = withSubscriptionPerms(req, &core.PanelUserPermissions{CanReadUsers: true})
	server.handleGetSubscription(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%q", rec.Code, rec.Body.String())
	}

	var got subscriptionDetailResponse
	decodeJSONResponse(t, rec, &got)
	if got.Token != nil {
		t.Fatalf("token=%q want nil for read-only caller", *got.Token)
	}
}

func TestGetSubscription_IncludesTokenForWriters(t *testing.T) {
	server, _ := newPublicSubscriptionTestServer(t)
	created := createSubscriptionForTest(t, server, subscriptionMutationRequest{
		Name:        "Writable Bundle",
		QuotaLimit:  0,
		QuotaPeriod: "monthly",
		Users:       []string{"alice"},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/subscriptions/"+strconv.FormatInt(created.ID, 10), nil)
	req.SetPathValue("id", strconv.FormatInt(created.ID, 10))
	req = withSubscriptionPerms(req, &core.PanelUserPermissions{CanReadUsers: true, CanWriteUsers: true})
	server.handleGetSubscription(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%q", rec.Code, rec.Body.String())
	}

	var got subscriptionDetailResponse
	decodeJSONResponse(t, rec, &got)
	if got.Token == nil || *got.Token == "" {
		t.Fatalf("token=%v want non-empty for writer", got.Token)
	}
}

func createSubscriptionForTest(t *testing.T, server *Server, body subscriptionMutationRequest) subscriptionCreateResponse {
	t.Helper()

	rec := httptest.NewRecorder()
	req := newJSONRequest(t, http.MethodPost, "/api/subscriptions", body)
	server.handleCreateSubscription(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%q", rec.Code, rec.Body.String())
	}

	var created subscriptionCreateResponse
	decodeJSONResponse(t, rec, &created)
	if created.ID == 0 {
		t.Fatalf("create id=%d want non-zero", created.ID)
	}
	if created.Token == "" {
		t.Fatalf("create token empty")
	}
	return created
}

func getSubscriptionForTest(t *testing.T, server *Server, id int64) subscriptionDetailResponse {
	t.Helper()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/subscriptions/"+strconv.FormatInt(id, 10), nil)
	req.SetPathValue("id", strconv.FormatInt(id, 10))
	server.handleGetSubscription(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%q", rec.Code, rec.Body.String())
	}

	var got subscriptionDetailResponse
	decodeJSONResponse(t, rec, &got)
	return got
}

func updateSubscriptionForTest(t *testing.T, server *Server, id int64, body subscriptionMutationRequest) {
	t.Helper()
	updateSubscriptionForTestBody(t, server, id, body)
}

func updateSubscriptionForTestBody(t *testing.T, server *Server, id int64, body any) {
	t.Helper()

	rec := httptest.NewRecorder()
	req := newJSONRequest(t, http.MethodPut, "/api/subscriptions/"+strconv.FormatInt(id, 10), body)
	req.SetPathValue("id", strconv.FormatInt(id, 10))
	server.handleUpdateSubscription(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestUpdateSubscription_ReplacesAssignedUsers(t *testing.T) {
	server, _ := newPublicSubscriptionTestServerWithConfig(t, `{
		"inbounds": [
			{
				"type": "vless",
				"tag": "test-vless",
				"listen": "0.0.0.0",
				"listen_port": 443,
				"users": [
					{"name": "alice", "uuid": "11111111-1111-1111-1111-111111111111"},
					{"name": "bob", "uuid": "22222222-2222-2222-2222-222222222222"}
				]
			}
		]
	}`, []string{"test-vless"})

	created := createSubscriptionForTest(t, server, subscriptionMutationRequest{
		Name:        "Replace Users Bundle",
		QuotaLimit:  0,
		QuotaPeriod: "monthly",
		Users:       []string{"alice", "bob"},
	})

	updateSubscriptionForTest(t, server, created.ID, subscriptionMutationRequest{
		Name:        "Replace Users Bundle",
		QuotaLimit:  0,
		QuotaPeriod: "monthly",
		Users:       []string{"alice"},
	})

	got := getSubscriptionForTest(t, server, created.ID)
	if len(got.Users) != 1 || got.Users[0] != "alice" {
		t.Fatalf("users=%v want [alice]", got.Users)
	}
}

func newJSONRequest(t *testing.T, method, target string, body any) *http.Request {
	t.Helper()

	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	req := httptest.NewRequest(method, target, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func decodeJSONResponse(t *testing.T, rec *httptest.ResponseRecorder, out any) {
	t.Helper()

	if err := json.NewDecoder(rec.Body).Decode(out); err != nil {
		t.Fatalf("decode response: %v body=%q", err, rec.Body.String())
	}
}

func createSubscriptionPanelUserForTest(t *testing.T, dataStore *core.Store, username, password string, perms core.PanelUserPermissions) {
	t.Helper()
	if err := dataStore.CreatePanelUser(username, password, perms); err != nil {
		t.Fatalf("CreatePanelUser(%s): %v", username, err)
	}
}

func subscriptionAuthHeadersForUser(t *testing.T, server *Server, username, password string) map[string]string {
	t.Helper()

	rec := httptest.NewRecorder()
	req := newJSONRequest(t, http.MethodPost, "/api/login", LoginRequest{
		Username: username,
		Password: password,
	})
	server.handleLogin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%q", rec.Code, rec.Body.String())
	}

	var login loginTestResponse
	decodeJSONResponse(t, rec, &login)
	if login.Token == "" {
		t.Fatalf("login token empty")
	}

	return map[string]string{
		"Authorization": "Bearer " + login.Token,
	}
}

func performSubscriptionDefaultsRequest(t *testing.T, server *Server, method, target string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	var req *http.Request
	if body == nil {
		req = httptest.NewRequest(method, target, nil)
	} else {
		req = newJSONRequest(t, method, target, body)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	rec := httptest.NewRecorder()
	server.AuthMiddleware(server.Routes()).ServeHTTP(rec, req)
	return rec
}
