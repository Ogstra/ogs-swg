package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Ogstra/ogs-swg/internal/core"
	"github.com/Ogstra/ogs-swg/internal/core/store"
)

func TestAuditEntityAndDetailExtractsBulkUserNames(t *testing.T) {
	body := []byte(`[
		{"name":"alice@example.test"},
		{"name":"bob@example.test"},
		{"name":"alice@example.test"}
	]`)

	entityID, detail := auditEntityAndDetail(httptest.NewRequest(http.MethodPost, "/api/users/bulk", nil), body, "user", "create")

	if entityID != "users:2" {
		t.Fatalf("entityID=%q want users:2", entityID)
	}
	if detail != "users:2:alice@example.test,bob@example.test" {
		t.Fatalf("detail=%q", detail)
	}
}

func TestAuditEntityAndDetailExtractsRenameDetail(t *testing.T) {
	body := []byte(`{"username":"old-admin","new_username":"new-admin"}`)

	entityID, detail := auditEntityAndDetail(httptest.NewRequest(http.MethodPut, "/api/panel-users/username", nil), body, "panel_user", "update")

	if entityID != "old-admin" {
		t.Fatalf("entityID=%q want old-admin", entityID)
	}
	if detail != "to:new-admin" {
		t.Fatalf("detail=%q want to:new-admin", detail)
	}
}

func TestSubscriptionAuditUsesNameForIDScopedMutations(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServer(t)
	server.auditStore = newTestAuditStore(t)

	subID, err := dataStore.Queries.CreateSubscription(t.Context(), store.CreateSubscriptionParams{
		Token:       "audit-sub-token",
		Name:        "Audit Bundle",
		QuotaLimit:  sql.NullInt64{Int64: 0, Valid: true},
		QuotaPeriod: sql.NullString{String: "monthly", Valid: true},
		ResetDay:    sql.NullInt64{Int64: 1, Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if err := dataStore.Queries.AddUserToSubscription(t.Context(), store.AddUserToSubscriptionParams{
		SubID:    subID,
		UserName: "alice",
	}); err != nil {
		t.Fatalf("AddUserToSubscription: %v", err)
	}

	updateBody := map[string]any{
		"name":         "Audit Bundle Renamed",
		"quota_limit":  int64(0),
		"quota_period": "monthly",
		"users":        []string{"alice"},
	}
	raw, _ := json.Marshal(updateBody)
	subIDStr := strconv.FormatInt(subID, 10)
	updateReq := httptest.NewRequest(http.MethodPut, "/api/subscriptions/"+subIDStr, bytes.NewReader(raw))
	updateReq.SetPathValue("id", subIDStr)
	updateRec := httptest.NewRecorder()
	server.handleUpdateSubscription(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%q", updateRec.Code, updateRec.Body.String())
	}

	regenReq := httptest.NewRequest(http.MethodPost, "/api/subscriptions/"+subIDStr+"/regenerate", nil)
	regenReq.SetPathValue("id", subIDStr)
	regenRec := httptest.NewRecorder()
	server.handleRegenerateSubscriptionToken(regenRec, regenReq)
	if regenRec.Code != http.StatusOK {
		t.Fatalf("regenerate status=%d body=%q", regenRec.Code, regenRec.Body.String())
	}

	entries := queryAuditEntries(t, server.auditStore)
	if len(entries) != 2 {
		t.Fatalf("audit entries=%d want 2: %#v", len(entries), entries)
	}
	updateEntry := findAuditEntry(entries, "subscription", "update")
	if updateEntry == nil || updateEntry.EntityID != "Audit Bundle" || !strings.Contains(updateEntry.Detail, "to:Audit Bundle Renamed") {
		t.Fatalf("update audit entry = %#v", updateEntry)
	}
	regenEntry := findAuditEntry(entries, "subscription", "regenerate")
	if regenEntry == nil || regenEntry.EntityID != "Audit Bundle Renamed" {
		t.Fatalf("regenerate audit entry = %#v", regenEntry)
	}
}

func TestProtectionCreateAuditUsesReturnedID(t *testing.T) {
	server, dataStore := newPublicSubscriptionTestServer(t)
	server.auditStore = newTestAuditStore(t)

	req := httptest.NewRequest(http.MethodPost, "/api/settings/protection-rules", strings.NewReader(`{"rule_type":"ip_allow","value":"203.0.113.10","note":"test"}`))
	rec := httptest.NewRecorder()
	server.handleCreateProtectionRule(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}

	rules, err := dataStore.Queries.GetAllProtectionRules(t.Context())
	if err != nil {
		t.Fatalf("GetAllProtectionRules: %v", err)
	}
	entries := queryAuditEntries(t, server.auditStore)
	if len(entries) != 1 {
		t.Fatalf("audit entries=%d want 1", len(entries))
	}
	if entries[0].EntityID != strconv.FormatInt(rules[0].ID, 10) || entries[0].Detail != "ip_allow" {
		t.Fatalf("audit entry=%#v rule=%#v", entries[0], rules[0])
	}
}

func newTestAuditStore(t *testing.T) *core.AuditStore {
	t.Helper()
	auditStore, err := core.NewAuditStore(filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatalf("NewAuditStore: %v", err)
	}
	t.Cleanup(auditStore.Close)
	return auditStore
}

func queryAuditEntries(t *testing.T, auditStore *core.AuditStore) []core.AuditEntry {
	t.Helper()
	page, err := auditStore.QueryAuditLog(context.Background(), 20, 0, "", "")
	if err != nil {
		t.Fatalf("QueryAuditLog: %v", err)
	}
	return page.Items
}

func findAuditEntry(entries []core.AuditEntry, domain, action string) *core.AuditEntry {
	for i := range entries {
		if entries[i].Domain == domain && entries[i].Action == action {
			return &entries[i]
		}
	}
	return nil
}
