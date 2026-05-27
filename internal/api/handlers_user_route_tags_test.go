package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
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

func TestHandleGetUsersIncludesRouteTags(t *testing.T) {
	server, _, store := newSingboxHandlerTestServerWithStore(t, routeTagAPIFixture())
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
	server.handleGetUsers(rec, httptest.NewRequest(http.MethodGet, "/api/users", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}

	var users []UserStatus
	if err := json.NewDecoder(rec.Body).Decode(&users); err != nil {
		t.Fatalf("decode users: %v", err)
	}
	if len(users) != 1 {
		t.Fatalf("users len=%d; want alice only: %#v", len(users), users)
	}
	alice := users[0]
	if alice.Name != "alice" {
		t.Fatalf("user name=%q; want alice", alice.Name)
	}
	if len(alice.InboundTags) != 1 || alice.InboundTags[0] != "test-vless" {
		t.Fatalf("inbound_tags = %#v; want unchanged [test-vless]", alice.InboundTags)
	}
	if len(alice.RouteTags) != 1 || alice.RouteTags[0].Name != "Premium" || !alice.RouteTags[0].Linked || alice.RouteTags[0].Broken {
		t.Fatalf("route_tags = %#v; want healthy Premium only", alice.RouteTags)
	}

	rec = httptest.NewRecorder()
	server.handleGetUserRouteTags(rec, httptest.NewRequest(http.MethodGet, "/api/user-route-tags", nil))
	statuses := decodeRouteTagStatuses(t, rec)
	foundBroken := false
	for _, status := range statuses {
		if status.Name == "Broken" && status.Broken {
			foundBroken = true
		}
	}
	if !foundBroken {
		t.Fatalf("GET /api/user-route-tags did not expose broken tag: %#v", statuses)
	}
}

func TestHandleUpdateUserRouteTags(t *testing.T) {
	server, stub, store := newSingboxHandlerTestServerWithStore(t, routeTagAPIFixture())
	premiumRule := map[string]interface{}{
		"action":    "route",
		"rule_set":  []interface{}{"geoip-premium"},
		"outbound":  "premium",
		"auth_user": []interface{}{"alice"},
		"x-extra":   map[string]interface{}{"preserve": true},
	}
	emptyRule := map[string]interface{}{
		"action":    "route",
		"rule_set":  []interface{}{"geosite-empty"},
		"outbound":  "empty",
		"auth_user": []interface{}{},
	}
	createRouteTagForRule(t, store, premiumRule, "Premium")
	empty := createRouteTagForRule(t, store, emptyRule, "Empty")

	req := httptest.NewRequest(http.MethodPut, "/api/users/alice/route-tags", strings.NewReader(`{"tag_ids":[`+strconv.FormatInt(empty.ID, 10)+`]}`))
	req.SetPathValue("name", "alice")
	rec := httptest.NewRecorder()
	server.handleUpdateUserRouteTags(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}

	var body UpdateUserRouteTagsResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.Success || !body.SingboxPendingChanges {
		t.Fatalf("response = %#v; want success with pending changes", body)
	}
	if len(body.RouteTags) != 1 || body.RouteTags[0].ID != empty.ID {
		t.Fatalf("route_tags = %#v; want Empty only", body.RouteTags)
	}
	if stub.writeCount != 1 {
		t.Fatalf("writeCount=%d; want one batched config write", stub.writeCount)
	}
	if stub.restartCount != 0 {
		t.Fatalf("restartCount=%d; want no RestartService call", stub.restartCount)
	}

	rules := readStoredConfigMap(t, stub)["route"].(map[string]interface{})["rules"].([]interface{})
	premiumAuth, ok := rules[0].(map[string]interface{})["auth_user"].([]interface{})
	if !ok || len(premiumAuth) != 0 {
		t.Fatalf("premium auth_user=%#v; want empty array after removing last user", rules[0].(map[string]interface{})["auth_user"])
	}
	if _, ok := rules[0].(map[string]interface{})["x-extra"]; !ok {
		t.Fatalf("premium x-extra was not preserved")
	}
	emptyAuth, ok := rules[1].(map[string]interface{})["auth_user"].([]interface{})
	if !ok || len(emptyAuth) != 1 || emptyAuth[0] != "alice" {
		t.Fatalf("empty auth_user=%#v; want [alice]", rules[1].(map[string]interface{})["auth_user"])
	}
}

func TestHandleUpdateUserRouteTagsRejectsExternalOnlyUser(t *testing.T) {
	server, stub, store := newSingboxHandlerTestServerWithStore(t, routeTagAPIFixture())
	emptyRule := map[string]interface{}{
		"action":    "route",
		"rule_set":  []interface{}{"geosite-empty"},
		"outbound":  "empty",
		"auth_user": []interface{}{},
	}
	empty := createRouteTagForRule(t, store, emptyRule, "Empty")
	if _, err := store.UpsertExternalProfile(core.ExternalProfile{
		Name:     "external-only",
		Type:     "vless",
		HostIPv4: "external.example.test",
		Port:     443,
		Enabled:  true,
	}); err != nil {
		t.Fatalf("UpsertExternalProfile: %v", err)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/users/external-only/route-tags", strings.NewReader(`{"tag_ids":[`+strconv.FormatInt(empty.ID, 10)+`]}`))
	req.SetPathValue("name", "external-only")
	rec := httptest.NewRecorder()
	server.handleUpdateUserRouteTags(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%q; want 400", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "External-only users cannot use route tags") {
		t.Fatalf("body=%q; want external-only route tag error", rec.Body.String())
	}
	if stub.writeCount != 0 {
		t.Fatalf("writeCount=%d; want no config write", stub.writeCount)
	}
}

func TestHandleUpdateUserRouteTagsReturnsConflictForBrokenChangedTag(t *testing.T) {
	server, stub, store := newSingboxHandlerTestServerWithStore(t, routeTagAPIFixture())
	brokenRule := map[string]interface{}{
		"action":    "route",
		"rule_set":  []interface{}{"missing"},
		"outbound":  "missing",
		"auth_user": []interface{}{},
	}
	broken := createRouteTagForRule(t, store, brokenRule, "Broken")

	req := httptest.NewRequest(http.MethodPut, "/api/users/alice/route-tags", strings.NewReader(`{"tag_ids":[`+strconv.FormatInt(broken.ID, 10)+`]}`))
	req.SetPathValue("name", "alice")
	rec := httptest.NewRecorder()
	server.handleUpdateUserRouteTags(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%q; want 409", rec.Code, rec.Body.String())
	}
	if stub.writeCount != 0 {
		t.Fatalf("writeCount=%d; want no write for broken changed tag", stub.writeCount)
	}
}
