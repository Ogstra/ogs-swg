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

func routeTagAPIFixture() string {
	return `{
		"inbounds": [
			{"type":"vless","tag":"test-vless","users":[{"name":"alice","uuid":"11111111-1111-1111-1111-111111111111"}]}
		],
		"route": {
			"rules": [
				{"action":"route","rule_set":["geoip-premium"],"outbound":"premium","auth_user":["alice"],"x-extra":{"preserve":true}},
				{"action":"route","rule_set":["geosite-empty"],"outbound":"empty","auth_user":[]},
				{"action":"route","rule_set":["geosite-unmanaged"],"outbound":"unmanaged"},
				{"action":"route","rule_set":["geosite-fallback"],"outbound":"fallback","auth_user":["bob"]}
			]
		}
	}`
}

func createRouteTagForRule(t *testing.T, store *core.Store, rule map[string]interface{}, name string) core.UserRouteTag {
	t.Helper()

	matchJSON, err := core.CanonicalRouteTagRuleMatch(rule)
	if err != nil {
		t.Fatalf("CanonicalRouteTagRuleMatch: %v", err)
	}
	tag, err := store.CreateUserRouteTag(name, "", "", matchJSON)
	if err != nil {
		t.Fatalf("CreateUserRouteTag: %v", err)
	}
	return tag
}

func decodeRouteTagStatuses(t *testing.T, rec *httptest.ResponseRecorder) []UserRouteTagStatus {
	t.Helper()

	var statuses []UserRouteTagStatus
	if err := json.NewDecoder(rec.Body).Decode(&statuses); err != nil {
		t.Fatalf("decode statuses: %v body=%q", err, rec.Body.String())
	}
	return statuses
}

func TestHandleUserRouteTagsDefinitionEndpoints(t *testing.T) {
	server, stub, store := newSingboxHandlerTestServerWithStore(t, routeTagAPIFixture())

	premiumRule := map[string]interface{}{
		"action":    "route",
		"rule_set":  []interface{}{"geoip-premium"},
		"outbound":  "premium",
		"auth_user": []interface{}{"alice"},
		"x-extra":   map[string]interface{}{"preserve": true},
	}
	brokenRule := map[string]interface{}{
		"action":    "route",
		"rule_set":  []interface{}{"missing"},
		"outbound":  "missing",
		"auth_user": []interface{}{},
	}
	createRouteTagForRule(t, store, premiumRule, "Premium")
	createRouteTagForRule(t, store, brokenRule, "Broken")

	rec := httptest.NewRecorder()
	server.handleGetUserRouteTags(rec, httptest.NewRequest(http.MethodGet, "/api/user-route-tags", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%q", rec.Code, rec.Body.String())
	}
	statuses := decodeRouteTagStatuses(t, rec)
	if len(statuses) != 2 {
		t.Fatalf("statuses len=%d; want 2: %#v", len(statuses), statuses)
	}
	byName := map[string]UserRouteTagStatus{}
	for _, status := range statuses {
		byName[status.Name] = status
	}
	premium := byName["Premium"]
	broken := byName["Broken"]
	if !premium.Linked || premium.Broken || len(premium.AuthUsers) != 1 || premium.AuthUsers[0] != "alice" {
		t.Fatalf("premium status = %#v; want linked auth_users [alice]", premium)
	}
	if broken.Linked || !broken.Broken || broken.BrokenReason == "" {
		t.Fatalf("broken status = %#v; want visible broken state", broken)
	}

	postBody := strings.NewReader(`{"name":"Empty","color":"#0088ff","description":"zero users","rule_index":1}`)
	rec = httptest.NewRecorder()
	server.handleCreateUserRouteTag(rec, httptest.NewRequest(http.MethodPost, "/api/user-route-tags", postBody))
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST status=%d body=%q", rec.Code, rec.Body.String())
	}
	var created UserRouteTagStatus
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode created: %v", err)
	}
	if !created.Linked || created.Broken || len(created.AuthUsers) != 0 {
		t.Fatalf("created = %#v; want linked zero-assignee tag", created)
	}
	rules := readStoredConfigMap(t, stub)["route"].(map[string]interface{})["rules"].([]interface{})
	if got := rules[1].(map[string]interface{})["auth_user"].([]interface{}); len(got) != 0 {
		t.Fatalf("POST seeded membership: %#v", got)
	}

	renameBody := strings.NewReader(`{"name":"Empty Renamed","color":"#00aa00","description":"renamed"}`)
	req := httptest.NewRequest(http.MethodPut, "/api/user-route-tags/3", renameBody)
	req.SetPathValue("id", "3")
	rec = httptest.NewRecorder()
	server.handleUpdateUserRouteTag(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT rename status=%d body=%q", rec.Code, rec.Body.String())
	}
	var renamed UserRouteTagStatus
	if err := json.NewDecoder(rec.Body).Decode(&renamed); err != nil {
		t.Fatalf("decode renamed: %v", err)
	}
	if renamed.Name != "Empty Renamed" || renamed.RuleMatchJSON != created.RuleMatchJSON {
		t.Fatalf("renamed = %#v; want visible rename with preserved rule match", renamed)
	}

	relinkBody := strings.NewReader(`{"name":"Empty Renamed","color":"#00aa00","description":"relinked","rule_index":3}`)
	req = httptest.NewRequest(http.MethodPut, "/api/user-route-tags/3", relinkBody)
	req.SetPathValue("id", "3")
	rec = httptest.NewRecorder()
	server.handleUpdateUserRouteTag(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT relink status=%d body=%q", rec.Code, rec.Body.String())
	}
	var relinked UserRouteTagStatus
	if err := json.NewDecoder(rec.Body).Decode(&relinked); err != nil {
		t.Fatalf("decode relinked: %v", err)
	}
	if relinked.RuleMatchJSON == created.RuleMatchJSON || len(relinked.AuthUsers) != 1 || relinked.AuthUsers[0] != "bob" {
		t.Fatalf("relinked = %#v; want fallback auth_users [bob]", relinked)
	}

	req = httptest.NewRequest(http.MethodDelete, "/api/user-route-tags/3", nil)
	req.SetPathValue("id", "3")
	rec = httptest.NewRecorder()
	server.handleDeleteUserRouteTag(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE status=%d body=%q", rec.Code, rec.Body.String())
	}
	rules = readStoredConfigMap(t, stub)["route"].(map[string]interface{})["rules"].([]interface{})
	if got := rules[3].(map[string]interface{})["auth_user"].([]interface{}); len(got) != 1 || got[0] != "bob" {
		t.Fatalf("DELETE mutated route auth_user: %#v", got)
	}
}

func TestHandleCompatibleUserRouteRules(t *testing.T) {
	server, _, store := newSingboxHandlerTestServerWithStore(t, routeTagAPIFixture())
	rule := map[string]interface{}{
		"action":    "route",
		"rule_set":  []interface{}{"geoip-premium"},
		"outbound":  "premium",
		"auth_user": []interface{}{"alice"},
		"x-extra":   map[string]interface{}{"preserve": true},
	}
	createRouteTagForRule(t, store, rule, "Premium")

	rec := httptest.NewRecorder()
	server.handleGetCompatibleUserRouteRules(rec, httptest.NewRequest(http.MethodGet, "/api/user-route-tags/compatible-rules", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}

	var rules []CompatibleUserRouteRule
	if err := json.NewDecoder(rec.Body).Decode(&rules); err != nil {
		t.Fatalf("decode rules: %v", err)
	}
	if len(rules) != 3 {
		t.Fatalf("rules len=%d; want compatible rules with auth_user only: %#v", len(rules), rules)
	}
	if rules[0].Index != 0 || !rules[0].AlreadyLinked || len(rules[0].AuthUsers) != 1 || rules[0].AuthUsers[0] != "alice" {
		t.Fatalf("premium rule = %#v; want already linked auth_users [alice]", rules[0])
	}
	if rules[1].Index != 1 || len(rules[1].AuthUsers) != 0 {
		t.Fatalf("empty auth_user rule = %#v; want included with zero users", rules[1])
	}
	if rules[2].Index != 3 {
		t.Fatalf("third compatible index=%d; want rule without auth_user excluded", rules[2].Index)
	}
}

func TestHandleUserRouteTagRejectsInvalidCreate(t *testing.T) {
	server, _, _ := newSingboxHandlerTestServerWithStore(t, routeTagAPIFixture())

	rec := httptest.NewRecorder()
	server.handleCreateUserRouteTag(rec, httptest.NewRequest(http.MethodPost, "/api/user-route-tags", bytes.NewReader([]byte(`{"name":"","rule_index":1}`))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%q; want 400", rec.Code, rec.Body.String())
	}
}
