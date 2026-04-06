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

func withSubscriptionPerms(r *http.Request, perms *core.PanelUserPermissions) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), permissionsContextKey, perms))
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
