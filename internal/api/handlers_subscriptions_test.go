package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

type subscriptionMutationRequest struct {
	Name                       string   `json:"name"`
	QuotaLimit                 int64    `json:"quota_limit"`
	QuotaPeriod                string   `json:"quota_period"`
	Users                      []string `json:"users"`
	ExpiresAt                  *int64   `json:"expires_at,omitempty"`
	ProfileUpdateIntervalHours *int64   `json:"profile_update_interval_hours,omitempty"`
	UpdateAlways               *bool    `json:"update_always,omitempty"`
}

type subscriptionCreateResponse struct {
	ID    int64  `json:"id"`
	Token string `json:"token"`
}

type subscriptionDetailResponse struct {
	ID                         int64    `json:"id"`
	Token                      string   `json:"token"`
	Name                       string   `json:"name"`
	QuotaLimit                 int64    `json:"quota_limit"`
	QuotaPeriod                string   `json:"quota_period"`
	UsedBytes                  int64    `json:"used_bytes"`
	Users                      []string `json:"users"`
	ExpiresAt                  *int64   `json:"expires_at"`
	ProfileUpdateIntervalHours *int64   `json:"profile_update_interval_hours"`
	UpdateAlways               bool     `json:"update_always"`
	CreatedAt                  int64    `json:"created_at"`
	UpdatedAt                  int64    `json:"updated_at"`
}

func TestSubscriptionCreateAndGetRefreshPolicyRoundTrip(t *testing.T) {
	server, _ := newPublicSubscriptionTestServer(t)

	interval := int64(6)
	expiresAt := int64(1798790400)
	updateAlways := true
	createReq := subscriptionMutationRequest{
		ExpiresAt:                  &expiresAt,
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
	if got.ExpiresAt == nil || *got.ExpiresAt != expiresAt {
		t.Fatalf("expires_at=%v want %d", got.ExpiresAt, expiresAt)
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

	if got.ExpiresAt != nil {
		t.Fatalf("expires_at=%v want nil", *got.ExpiresAt)
	}
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
	initialExpiresAt := int64(1798790400)
	initialUpdateAlways := true
	created := createSubscriptionForTest(t, server, subscriptionMutationRequest{
		ExpiresAt:                  &initialExpiresAt,
		Name:                       "Mutable Bundle",
		QuotaLimit:                 0,
		QuotaPeriod:                "monthly",
		Users:                      []string{"alice"},
		ProfileUpdateIntervalHours: &initialInterval,
		UpdateAlways:               &initialUpdateAlways,
	})

	updatedExpiresAt := int64(1801382400)
	updatedInterval := int64(12)
	updatedUpdateAlways := false
	updateSubscriptionForTest(t, server, created.ID, subscriptionMutationRequest{
		ExpiresAt:                  &updatedExpiresAt,
		Name:                       "Mutable Bundle",
		QuotaLimit:                 1024,
		QuotaPeriod:                "monthly",
		Users:                      []string{"alice"},
		ProfileUpdateIntervalHours: &updatedInterval,
		UpdateAlways:               &updatedUpdateAlways,
	})

	got := getSubscriptionForTest(t, server, created.ID)
	if got.ExpiresAt == nil || *got.ExpiresAt != updatedExpiresAt {
		t.Fatalf("after explicit update expires_at=%v want %d", got.ExpiresAt, updatedExpiresAt)
	}
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
	if got.ExpiresAt == nil || *got.ExpiresAt != updatedExpiresAt {
		t.Fatalf("after omitted update expires_at=%v want %d", got.ExpiresAt, updatedExpiresAt)
	}
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
	initialExpiresAt := int64(1798790400)
	initialUpdateAlways := true
	created := createSubscriptionForTest(t, server, subscriptionMutationRequest{
		ExpiresAt:                  &initialExpiresAt,
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
		"expires_at":                    nil,
		"profile_update_interval_hours": nil,
	})

	got := getSubscriptionForTest(t, server, created.ID)
	if got.ExpiresAt != nil {
		t.Fatalf("after explicit null expires_at=%v want nil", *got.ExpiresAt)
	}
	if got.ProfileUpdateIntervalHours != nil {
		t.Fatalf("after explicit null profile_update_interval_hours=%v want nil", *got.ProfileUpdateIntervalHours)
	}
	if got.UpdateAlways != initialUpdateAlways {
		t.Fatalf("after explicit null update_always=%v want %v", got.UpdateAlways, initialUpdateAlways)
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
