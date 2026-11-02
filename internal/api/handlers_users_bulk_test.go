package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
)

func newBulkActionServer(t *testing.T) (*Server, *core.Store) {
	t.Helper()
	server, _, st := newSingboxHandlerTestServerWithStore(t, `{
		"inbounds": [{
			"type": "vless", "tag": "test-vless", "listen": "0.0.0.0", "listen_port": 443,
			"users": [
				{"name":"ann","uuid":"11111111-1111-1111-1111-111111111111"},
				{"name":"ben","uuid":"22222222-2222-2222-2222-222222222222"},
				{"name":"cy","uuid":"33333333-3333-3333-3333-333333333333"}
			],
			"tls": {"enabled": true, "server_name": "example.com"},
			"transport": {"type": "tcp"}
		}]
	}`)
	for _, name := range []string{"ann", "ben", "cy"} {
		uuid := map[string]string{"ann": "11111111-1111-1111-1111-111111111111", "ben": "22222222-2222-2222-2222-222222222222", "cy": "33333333-3333-3333-3333-333333333333"}[name]
		if err := st.SaveUserMetadata(core.UserMetadata{
			Email: name, QuotaLimit: 10 << 30, QuotaPeriod: "monthly", Enabled: true,
			Credential: uuid, InboundTags: []string{"test-vless"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	return server, st
}

func bulkCall(t *testing.T, s *Server, body bulkUserActionRequest) (*httptest.ResponseRecorder, bulkUserActionResponse) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleBulkUserAction(rec, newJSONRequest(t, http.MethodPost, "/api/users/bulk-action", body))
	var out bulkUserActionResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
	}
	return rec, out
}

func TestExtendUserExpiry(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	day := int64(86400)

	got, err := extendUserExpiry(core.UserExpiry{ExpiresAt: now.Unix() + 5*day}, 10, now)
	if err != nil || got.ExpiresAt != now.Unix()+15*day {
		t.Fatalf("future expiry: %+v err=%v", got, err)
	}
	got, _ = extendUserExpiry(core.UserExpiry{ExpiresAt: now.Unix() - 100*day}, 30, now)
	if got.ExpiresAt != now.Unix()+30*day {
		t.Fatalf("an expired user must restart from now, got %+v", got)
	}
	got, _ = extendUserExpiry(core.UserExpiry{ExpireAfterDays: 7}, 3, now)
	if got.ExpireAfterDays != 10 || got.ExpiresAt != 0 {
		t.Fatalf("first-use user: %+v", got)
	}
	got, _ = extendUserExpiry(core.UserExpiry{ExpireAfterDays: 7}, -20, now)
	if got.ExpireAfterDays != 1 {
		t.Fatalf("days must never drop below 1, got %+v", got)
	}
	got, _ = extendUserExpiry(core.UserExpiry{}, 30, now)
	if got.ExpiresAt != now.Unix()+30*day {
		t.Fatalf("no expiry yet: %+v", got)
	}
	if _, err := extendUserExpiry(core.UserExpiry{}, -5, now); err == nil {
		t.Fatal("shortening a user that has no expiry must fail")
	}
	got, _ = extendUserExpiry(core.UserExpiry{ExpiresAt: now.Unix() + day}, -30, now)
	if got.ExpiresAt != now.Unix() {
		t.Fatalf("removing more days than remain ends the expiry now, got %+v", got)
	}
}

func TestBulkUserAction_ExtendQuotaAndExpiry(t *testing.T) {
	s, st := newBulkActionServer(t)

	rec, out := bulkCall(t, s, bulkUserActionRequest{Names: []string{"ann", "ben", "ann", " "}, Action: "extend_days", Days: 30})
	if rec.Code != http.StatusOK || out.Done != 2 || out.Failed != 0 {
		t.Fatalf("extend: status=%d out=%+v body=%q", rec.Code, out, rec.Body.String())
	}
	for _, name := range []string{"ann", "ben"} {
		if e, _ := st.GetUserExpiry(name); e.ExpiresAt == 0 {
			t.Fatalf("%s did not get an expiry", name)
		}
	}
	if e, _ := st.GetUserExpiry("cy"); e.ExpiresAt != 0 {
		t.Fatal("a user that was not selected changed")
	}

	_, out = bulkCall(t, s, bulkUserActionRequest{Names: []string{"ann"}, Action: "add_quota", QuotaBytes: 5 << 30})
	if out.Done != 1 {
		t.Fatalf("add_quota: %+v", out)
	}
	if m, _ := st.GetUserMetadata("ann"); m.QuotaLimit != 15<<30 {
		t.Fatalf("quota = %d; want 15 GiB", m.QuotaLimit)
	}

	_, out = bulkCall(t, s, bulkUserActionRequest{Names: []string{"ben"}, Action: "set_quota", QuotaBytes: 0})
	if m, _ := st.GetUserMetadata("ben"); out.Done != 1 || m.QuotaLimit != 0 {
		t.Fatalf("set_quota to unlimited: out=%+v quota=%d", out, m.QuotaLimit)
	}
	// Adding to an unlimited quota is refused for that user, not for the batch.
	_, out = bulkCall(t, s, bulkUserActionRequest{Names: []string{"ben", "cy"}, Action: "add_quota", QuotaBytes: 1 << 30})
	if out.Done != 1 || out.Failed != 1 || out.Results[0].OK {
		t.Fatalf("add_quota on unlimited: %+v", out)
	}

	zero := int64(0)
	_, out = bulkCall(t, s, bulkUserActionRequest{Names: []string{"ann"}, Action: "set_expiry", ExpiresAt: &zero})
	if e, _ := st.GetUserExpiry("ann"); out.Done != 1 || e.ExpiresAt != 0 {
		t.Fatalf("clearing the expiry: out=%+v expiry=%+v", out, e)
	}
}

func TestBulkUserAction_ExpiredUsersAreDisabledAndExtendingBringsThemBack(t *testing.T) {
	s, st := newBulkActionServer(t)
	past := time.Now().Add(-time.Hour).Unix()
	if _, out := bulkCall(t, s, bulkUserActionRequest{Names: []string{"cy"}, Action: "set_expiry", ExpiresAt: &past}); out.Done != 1 {
		t.Fatalf("set_expiry: %+v", out)
	}
	if m, _ := st.GetUserMetadata("cy"); m.Enabled {
		t.Fatal("an expired user stayed enabled")
	}
	if _, out := bulkCall(t, s, bulkUserActionRequest{Names: []string{"cy"}, Action: "extend_days", Days: 10}); out.Done != 1 {
		t.Fatalf("extend: %+v", out)
	}
	if m, _ := st.GetUserMetadata("cy"); !m.Enabled {
		t.Fatal("extending the expiry did not bring the user back")
	}
}

func TestBulkUserAction_DeleteAndValidation(t *testing.T) {
	s, st := newBulkActionServer(t)

	_, out := bulkCall(t, s, bulkUserActionRequest{Names: []string{"ben", "ghost"}, Action: "delete"})
	if len(out.Results) != 2 || !out.Results[0].OK {
		t.Fatalf("deleting ben: %+v", out)
	}
	if m, _ := st.GetUserMetadata("ben"); m != nil && m.Email == "ben" {
		t.Fatal("ben still exists after the delete")
	}

	for name, req := range map[string]bulkUserActionRequest{
		"no users":       {Action: "delete"},
		"unknown action": {Names: []string{"ann"}, Action: "explode"},
		"zero days":      {Names: []string{"ann"}, Action: "extend_days"},
		"huge days":      {Names: []string{"ann"}, Action: "extend_days", Days: 99999},
		"zero add":       {Names: []string{"ann"}, Action: "add_quota"},
		"negative quota": {Names: []string{"ann"}, Action: "set_quota", QuotaBytes: -1},
		"missing date":   {Names: []string{"ann"}, Action: "set_expiry"},
	} {
		if rec, _ := bulkCall(t, s, req); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d; want 400", name, rec.Code)
		}
	}
}

func TestBulkUserAction_ResetUsageBringsAnOverQuotaUserBack(t *testing.T) {
	s, st := newBulkActionServer(t)
	now := time.Now()
	if err := st.SaveUserMetadata(core.UserMetadata{
		Email: "ann", QuotaLimit: 1000, QuotaPeriod: "monthly", Enabled: true,
		Credential: "11111111-1111-1111-1111-111111111111", InboundTags: []string{"test-vless"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.AddSample(core.Sample{User: "ann", Timestamp: now.Add(-time.Minute).Unix(), Uplink: 900, Downlink: 900}); err != nil {
		t.Fatal(err)
	}
	if err := st.ReconcileUserQuotaNow("ann", s.config); err != nil {
		t.Fatal(err)
	}
	if m, _ := st.GetUserMetadata("ann"); m.Enabled {
		t.Fatal("precondition: the user should be disabled for being over quota")
	}

	time.Sleep(1100 * time.Millisecond) // the reset time has one-second resolution
	_, out := bulkCall(t, s, bulkUserActionRequest{Names: []string{"ann"}, Action: "reset_usage"})
	if out.Done != 1 {
		t.Fatalf("reset_usage: %+v", out)
	}
	if m, _ := st.GetUserMetadata("ann"); !m.Enabled {
		t.Fatal("resetting the usage did not bring the user back")
	}
}
