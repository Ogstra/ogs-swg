package core

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type reloadTrackingExecutor struct {
	*stubExecutor
	restartCalled bool
}

func (r *reloadTrackingExecutor) RestartService(_ context.Context, _ string) error {
	r.restartCalled = true
	return nil
}

func readInboundByTagFromStub(t *testing.T, stub *stubExecutor, tag string) map[string]interface{} {
	t.Helper()

	var top map[string]interface{}
	if err := json.Unmarshal(stub.data, &top); err != nil {
		t.Fatalf("unmarshal stub config: %v", err)
	}

	inbounds, ok := top["inbounds"].([]interface{})
	if !ok {
		t.Fatalf("inbounds missing or wrong type: %#v", top["inbounds"])
	}
	for _, rawInbound := range inbounds {
		inbound, ok := rawInbound.(map[string]interface{})
		if !ok {
			continue
		}
		if currentTag, _ := inbound["tag"].(string); currentTag == tag {
			return inbound
		}
	}

	t.Fatalf("inbound %q not found", tag)
	return nil
}

func TestUpdateSingboxInbound_SanitizedWriteRemovesGhostFields(t *testing.T) {
	fixtureJSON := `{
		"inbounds": [
			{
				"type": "vless",
				"tag": "test-vless",
				"listen": "0.0.0.0",
				"listen_port": 443,
				"users": [
					{"name":"alice","uuid":"11111111-1111-1111-1111-111111111111","flow":"xtls-rprx-vision"}
				],
				"tls": {
					"enabled": true,
					"server_name": "example.com",
					"alpn": ["h2", "http/1.1"],
					"reality": {
						"enabled": true,
						"public_key": "public-key",
						"short_id": ["abcd"],
						"handshake": {"server": "hs.example.com"}
					}
				},
				"transport": {"type": "tcp"}
			}
		]
	}`

	cfg, stub := newTestConfig(t, fixtureJSON)

	updatedInbound := map[string]interface{}{
		"type":        "vless",
		"tag":         "test-vless",
		"listen":      "0.0.0.0",
		"listen_port": 443,
		"users": []interface{}{
			map[string]interface{}{
				"name": "alice",
				"uuid": "11111111-1111-1111-1111-111111111111",
				"flow": "xtls-rprx-vision",
			},
		},
		"tls": map[string]interface{}{
			"enabled":     true,
			"server_name": "example.com",
			"alpn":        []interface{}{"h2", "http/1.1"},
			"reality": map[string]interface{}{
				"enabled":    true,
				"public_key": "public-key",
				"short_id":   []interface{}{"abcd"},
				"handshake": map[string]interface{}{
					"server": "hs.example.com",
				},
			},
		},
		"transport": map[string]interface{}{
			"type": "ws",
			"path": "/ws",
		},
	}

	if err := cfg.UpdateSingboxInbound("test-vless", updatedInbound); err != nil {
		t.Fatalf("UpdateSingboxInbound: %v", err)
	}

	inbound := readInboundByTagFromStub(t, stub, "test-vless")
	tls, ok := inbound["tls"].(map[string]interface{})
	if !ok {
		t.Fatalf("tls missing or wrong type: %#v", inbound["tls"])
	}
	if got := tls["alpn"]; got != nil {
		t.Fatalf("stored tls.alpn = %#v; want removed", got)
	}
	if got := tls["reality"]; got != nil {
		t.Fatalf("stored tls.reality = %#v; want removed", got)
	}

	users, ok := inbound["users"].([]interface{})
	if !ok || len(users) != 1 {
		t.Fatalf("users missing or wrong type: %#v", inbound["users"])
	}
	user, ok := users[0].(map[string]interface{})
	if !ok {
		t.Fatalf("user wrong type: %#v", users[0])
	}
	if got := user["flow"]; got != nil {
		t.Fatalf("stored user.flow = %#v; want removed", got)
	}
}

func TestUpdateSingboxInbound_PreservesUnknownFieldsWhileDroppingModeledGhostFields(t *testing.T) {
	fixtureJSON := `{
		"inbounds": [
			{
				"type": "vless",
				"tag": "test-vless",
				"listen": "0.0.0.0",
				"listen_port": 443,
				"users": [
					{"name":"alice","uuid":"11111111-1111-1111-1111-111111111111","flow":"xtls-rprx-vision"}
				],
				"tls": {
					"enabled": true,
					"server_name": "example.com",
					"alpn": ["h2", "http/1.1"],
					"reality": {
						"enabled": true,
						"public_key": "public-key",
						"short_id": ["abcd"],
						"handshake": {"server": "hs.example.com"}
					}
				},
				"transport": {"type": "tcp"},
				"x-meta": {"custom":"value"}
			}
		]
	}`

	cfg, stub := newTestConfig(t, fixtureJSON)

	updatedInbound := map[string]interface{}{
		"type":        "vless",
		"tag":         "test-vless",
		"listen":      "0.0.0.0",
		"listen_port": 443,
		"users": []interface{}{
			map[string]interface{}{
				"name": "alice",
				"uuid": "11111111-1111-1111-1111-111111111111",
				"flow": "xtls-rprx-vision",
			},
		},
		"tls": map[string]interface{}{
			"enabled":     true,
			"server_name": "example.com",
			"alpn":        []interface{}{"h2", "http/1.1"},
			"reality": map[string]interface{}{
				"enabled":    true,
				"public_key": "public-key",
				"short_id":   []interface{}{"abcd"},
				"handshake": map[string]interface{}{
					"server": "hs.example.com",
				},
			},
		},
		"transport": map[string]interface{}{
			"type": "ws",
			"path": "/ws",
		},
	}

	if err := cfg.UpdateSingboxInbound("test-vless", updatedInbound); err != nil {
		t.Fatalf("UpdateSingboxInbound: %v", err)
	}

	inbound := readInboundByTagFromStub(t, stub, "test-vless")
	if got := inbound["x-meta"]; got == nil {
		t.Fatalf("x-meta missing after update")
	}
	tls, ok := inbound["tls"].(map[string]interface{})
	if !ok {
		t.Fatalf("tls missing or wrong type: %#v", inbound["tls"])
	}
	if got := tls["alpn"]; got != nil {
		t.Fatalf("stored tls.alpn = %#v; want removed", got)
	}
	if got := tls["reality"]; got != nil {
		t.Fatalf("stored tls.reality = %#v; want removed", got)
	}
}

// TestRoundTrip_NonOwnedSectionsPreserved verifies that log, dns, route, and
// outbounds sections are byte-identical (semantically) after an AddUser call.
// These sections are stored as json.RawMessage and must not be modified.
func TestRoundTrip_NonOwnedSectionsPreserved(t *testing.T) {
	fixtureJSON := `{
		"log": {"level": "info", "output": "stdout"},
		"dns": {"servers": [{"tag": "google", "address": "8.8.8.8"}]},
		"route": {"rules": [{"outbound": "direct"}]},
		"outbounds": [{"type": "direct", "tag": "direct"}],
		"inbounds": [
			{
				"type": "vless",
				"tag": "test-vless",
				"listen": "0.0.0.0",
				"listen_port": 1080,
				"users": []
			}
		]
	}`

	cfg, stub := newTestConfig(t, fixtureJSON)

	if err := cfg.AddUser("alice", "11111111-1111-1111-1111-111111111111", "", "test-vless", "", 0); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	// Read result back from stub
	resultBytes := stub.data

	// Parse original fixture for comparison
	var originalMap map[string]json.RawMessage
	if err := json.Unmarshal([]byte(fixtureJSON), &originalMap); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}

	// Parse result
	var resultMap map[string]json.RawMessage
	if err := json.Unmarshal(resultBytes, &resultMap); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	sections := []string{"log", "dns", "route", "outbounds"}
	for _, section := range sections {
		origVal, origOk := originalMap[section]
		resultVal, resultOk := resultMap[section]

		if origOk != resultOk {
			t.Errorf("section %q: presence changed (orig=%v, result=%v)", section, origOk, resultOk)
			continue
		}
		if !origOk {
			continue // not present in either; fine
		}
		if !jsonSemanticallyEqual(origVal, resultVal) {
			t.Errorf("section %q changed after AddUser:\n  original: %s\n  result:   %s", section, origVal, resultVal)
		}
	}
}

func TestReloadSingbox_UsesClashAPIWhenConfigured(t *testing.T) {
	requestReceived := make(chan *http.Request, 1)
	requestBody := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		requestReceived <- r
		requestBody <- body
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	fixtureJSON := `{
		"experimental": {
			"clash_api": {
				"external_controller": "` + strings.TrimPrefix(server.URL, "http://") + `"
			}
		}
	}`

	cfg, stub := newTestConfig(t, fixtureJSON)
	cfg.EnableSingbox = true
	tracker := &reloadTrackingExecutor{stubExecutor: stub}
	cfg.SetExecutor(tracker)

	if err := cfg.ReloadSingbox(); err != nil {
		t.Fatalf("ReloadSingbox: %v", err)
	}

	var req *http.Request
	select {
	case req = <-requestReceived:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for Clash API request")
	}

	var body []byte
	select {
	case body = <-requestBody:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for Clash API body")
	}

	if req.Method != http.MethodPut {
		t.Fatalf("method = %q, want %q", req.Method, http.MethodPut)
	}
	if req.URL.RequestURI() != "/configs?force=false" {
		t.Fatalf("request uri = %q, want %q", req.URL.RequestURI(), "/configs?force=false")
	}
	if got := req.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("content-type = %q, want application/json", got)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("authorization = %q, want empty", got)
	}
	if string(body) != `{"path":"/test/config.json"}` {
		t.Fatalf("body = %s, want %s", body, `{"path":"/test/config.json"}`)
	}
	if tracker.restartCalled {
		t.Fatalf("RestartService called during Clash API reload")
	}
}

func TestApplySingboxChanges_UsesClashAPIWhenConfigured(t *testing.T) {
	requestReceived := make(chan *http.Request, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestReceived <- r
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	fixtureJSON := `{
		"experimental": {
			"clash_api": {
				"external_controller": "` + strings.TrimPrefix(server.URL, "http://") + `"
			}
		}
	}`

	cfg, stub := newTestConfig(t, fixtureJSON)
	cfg.EnableSingbox = true
	cfg.MarkSingboxPending()
	tracker := &reloadTrackingExecutor{stubExecutor: stub}
	cfg.SetExecutor(tracker)

	if err := cfg.ApplySingboxChanges(); err != nil {
		t.Fatalf("ApplySingboxChanges: %v", err)
	}

	select {
	case <-requestReceived:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for Clash API request")
	}
	if cfg.GetSingboxPendingChanges() {
		t.Fatalf("GetSingboxPendingChanges() = true, want false after successful Clash API apply")
	}
	if tracker.restartCalled {
		t.Fatalf("RestartService called during Clash API apply")
	}
}

func TestApplySingboxChanges_RequiresRestartWhenNoClashAPI(t *testing.T) {
	cfg, stub := newTestConfig(t, `{}`)
	cfg.EnableSingbox = true
	cfg.MarkSingboxPending()
	tracker := &reloadTrackingExecutor{stubExecutor: stub}
	cfg.SetExecutor(tracker)

	err := cfg.ApplySingboxChanges()
	var restartRequired *SingboxRestartRequiredError
	if !errors.As(err, &restartRequired) {
		t.Fatalf("ApplySingboxChanges error = %v, want SingboxRestartRequiredError", err)
	}
	if restartRequired.Reason != "clash_api_not_configured" {
		t.Fatalf("restart reason = %q, want clash_api_not_configured", restartRequired.Reason)
	}
	if tracker.restartCalled {
		t.Fatalf("RestartService called before explicit confirmation")
	}
	if !cfg.GetSingboxPendingChanges() {
		t.Fatalf("GetSingboxPendingChanges() = false, want true until confirmed restart")
	}
}

func TestApplySingboxChanges_RequiresRestartWhenClashAPIFails(t *testing.T) {
	fixtureJSON := `{
		"experimental": {
			"clash_api": {
				"external_controller": "127.0.0.1:1"
			}
		}
	}`

	cfg, stub := newTestConfig(t, fixtureJSON)
	cfg.EnableSingbox = true
	cfg.MarkSingboxPending()
	tracker := &reloadTrackingExecutor{stubExecutor: stub}
	cfg.SetExecutor(tracker)

	err := cfg.ApplySingboxChanges()
	var restartRequired *SingboxRestartRequiredError
	if !errors.As(err, &restartRequired) {
		t.Fatalf("ApplySingboxChanges error = %v, want SingboxRestartRequiredError", err)
	}
	if restartRequired.Reason != "clash_api_reload_failed" {
		t.Fatalf("restart reason = %q, want clash_api_reload_failed", restartRequired.Reason)
	}
	if tracker.restartCalled {
		t.Fatalf("RestartService called before explicit confirmation")
	}
	if !cfg.GetSingboxPendingChanges() {
		t.Fatalf("GetSingboxPendingChanges() = false, want true until confirmed restart")
	}
}

func TestAddUser_ClearsPendingChangesWhenClashAPIReloadSucceeds(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	fixtureJSON := `{
		"inbounds": [
			{"type":"vless","tag":"test-vless","listen":"0.0.0.0","listen_port":10001,"users":[]}
		],
		"experimental": {
			"clash_api": {
				"external_controller": "` + strings.TrimPrefix(server.URL, "http://") + `"
			}
		}
	}`

	cfg, _ := newTestConfig(t, fixtureJSON)
	if err := cfg.AddUser("alice", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", "", "test-vless", "", 0); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	if cfg.GetSingboxPendingChanges() {
		t.Fatalf("GetSingboxPendingChanges() = true, want false after successful Clash API reload")
	}
}

func TestAddUser_KeepsPendingChangesWhenClashAPIReloadFails(t *testing.T) {
	fixtureJSON := `{
		"inbounds": [
			{"type":"vless","tag":"test-vless","listen":"0.0.0.0","listen_port":10001,"users":[]}
		],
		"experimental": {
			"clash_api": {
				"external_controller": "127.0.0.1:1"
			}
		}
	}`

	cfg, _ := newTestConfig(t, fixtureJSON)
	if err := cfg.AddUser("alice", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", "", "test-vless", "", 0); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	if !cfg.GetSingboxPendingChanges() {
		t.Fatalf("GetSingboxPendingChanges() = false, want true after failed Clash API reload")
	}
}

func TestReloadSingbox_IncludesSecretHeader(t *testing.T) {
	authHeader := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader <- r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	fixtureJSON := `{
		"experimental": {
			"clash_api": {
				"external_controller": "` + strings.TrimPrefix(server.URL, "http://") + `",
				"secret": "test-secret"
			}
		}
	}`

	cfg, _ := newTestConfig(t, fixtureJSON)
	cfg.EnableSingbox = true

	if err := cfg.ReloadSingbox(); err != nil {
		t.Fatalf("ReloadSingbox: %v", err)
	}

	var got string
	select {
	case got = <-authHeader:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for Clash API request")
	}
	if got != "Bearer test-secret" {
		t.Fatalf("authorization = %q, want %q", got, "Bearer test-secret")
	}
}

func TestReloadSingbox_NoSecretHeader(t *testing.T) {
	authHeader := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader <- r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	fixtureJSON := `{
		"experimental": {
			"clash_api": {
				"external_controller": "` + strings.TrimPrefix(server.URL, "http://") + `",
				"secret": ""
			}
		}
	}`

	cfg, _ := newTestConfig(t, fixtureJSON)
	cfg.EnableSingbox = true

	if err := cfg.ReloadSingbox(); err != nil {
		t.Fatalf("ReloadSingbox: %v", err)
	}

	var got string
	select {
	case got = <-authHeader:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for Clash API request")
	}
	if got != "" {
		t.Fatalf("authorization = %q, want empty", got)
	}
}

func TestReloadSingbox_FallsBackToExecutorWhenNoClashAPI(t *testing.T) {
	fixtureJSON := `{
		"experimental": {
			"v2ray_api": {
				"listen": "127.0.0.1:19001"
			}
		}
	}`

	cfg, stub := newTestConfig(t, fixtureJSON)
	cfg.EnableSingbox = true
	tracker := &reloadTrackingExecutor{stubExecutor: stub}
	cfg.SetExecutor(tracker)

	if err := cfg.ReloadSingbox(); err != nil {
		t.Fatalf("ReloadSingbox: %v", err)
	}

	if !tracker.restartCalled {
		t.Fatalf("RestartService was not called")
	}
}

func TestReloadSingbox_FallsBackWhenEmptyController(t *testing.T) {
	fixtureJSON := `{
		"experimental": {
			"clash_api": {
				"external_controller": ""
			}
		}
	}`

	cfg, stub := newTestConfig(t, fixtureJSON)
	cfg.EnableSingbox = true
	tracker := &reloadTrackingExecutor{stubExecutor: stub}
	cfg.SetExecutor(tracker)

	if err := cfg.ReloadSingbox(); err != nil {
		t.Fatalf("ReloadSingbox: %v", err)
	}

	if !tracker.restartCalled {
		t.Fatalf("RestartService was not called")
	}
}

func TestReloadSingbox_ReturnsErrorOnBadStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	fixtureJSON := `{
		"experimental": {
			"clash_api": {
				"external_controller": "` + strings.TrimPrefix(server.URL, "http://") + `"
			}
		}
	}`

	cfg, _ := newTestConfig(t, fixtureJSON)
	cfg.EnableSingbox = true

	err := cfg.ReloadSingbox()
	if err == nil {
		t.Fatalf("ReloadSingbox returned nil error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("error = %q, want status code", err)
	}
}

// TestRoundTrip_UnknownInboundFieldPreserved verifies that unknown keys inside
// a managed inbound (e.g. "x-meta") survive a round-trip through ModifySingboxConfig.
func TestRoundTrip_UnknownInboundFieldPreserved(t *testing.T) {
	fixtureJSON := `{
		"inbounds": [
			{
				"type": "vless",
				"tag": "test-vless",
				"listen": "0.0.0.0",
				"listen_port": 1080,
				"users": [],
				"x-meta": {"custom": "value"}
			}
		]
	}`

	cfg, stub := newTestConfig(t, fixtureJSON)

	if err := cfg.AddUser("alice", "11111111-1111-1111-1111-111111111111", "", "test-vless", "", 0); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	// Unmarshal result inbounds
	var resultTop map[string]json.RawMessage
	if err := json.Unmarshal(stub.data, &resultTop); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	var inboundList []map[string]json.RawMessage
	if err := json.Unmarshal(resultTop["inbounds"], &inboundList); err != nil {
		t.Fatalf("unmarshal inbounds: %v", err)
	}

	// Find the vless inbound by tag
	found := false
	for _, inbound := range inboundList {
		tagRaw, ok := inbound["tag"]
		if !ok {
			continue
		}
		var tag string
		if err := json.Unmarshal(tagRaw, &tag); err != nil || tag != "test-vless" {
			continue
		}

		found = true
		if _, ok := inbound["x-meta"]; !ok {
			t.Errorf("x-meta key missing from vless inbound after AddUser round-trip")
		}
		break
	}

	if !found {
		t.Fatal("test-vless inbound not found in result")
	}
}

// TestRoundTrip_NonManagedInboundPreserved verifies that a non-managed (non-user-type)
// inbound such as type=direct is not modified or removed after an AddUser call.
func TestRoundTrip_NonManagedInboundPreserved(t *testing.T) {
	fixtureJSON := `{
		"inbounds": [
			{
				"type": "vless",
				"tag": "test-vless",
				"listen": "0.0.0.0",
				"listen_port": 1080,
				"users": []
			},
			{
				"type": "direct",
				"tag": "unmanaged-direct",
				"listen": "0.0.0.0",
				"listen_port": 5353,
				"override_address": "8.8.8.8",
				"override_port": 53
			}
		]
	}`

	cfg, stub := newTestConfig(t, fixtureJSON)

	if err := cfg.AddUser("alice", "11111111-1111-1111-1111-111111111111", "", "test-vless", "", 0); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	// Parse original and result inbounds
	var origTop map[string]json.RawMessage
	if err := json.Unmarshal([]byte(fixtureJSON), &origTop); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	var origInbounds []json.RawMessage
	if err := json.Unmarshal(origTop["inbounds"], &origInbounds); err != nil {
		t.Fatalf("unmarshal orig inbounds: %v", err)
	}

	var resultTop map[string]json.RawMessage
	if err := json.Unmarshal(stub.data, &resultTop); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	var resultInbounds []json.RawMessage
	if err := json.Unmarshal(resultTop["inbounds"], &resultInbounds); err != nil {
		t.Fatalf("unmarshal result inbounds: %v", err)
	}

	// Find the direct inbound in original and result
	findByTag := func(list []json.RawMessage, tag string) (json.RawMessage, bool) {
		for _, raw := range list {
			var m map[string]json.RawMessage
			if err := json.Unmarshal(raw, &m); err != nil {
				continue
			}
			var t string
			if err := json.Unmarshal(m["tag"], &t); err != nil || t != tag {
				continue
			}
			return raw, true
		}
		return nil, false
	}

	origDirect, origFound := findByTag(origInbounds, "unmanaged-direct")
	resultDirect, resultFound := findByTag(resultInbounds, "unmanaged-direct")

	if !origFound {
		t.Fatal("unmanaged-direct not in fixture (test setup error)")
	}
	if !resultFound {
		t.Fatal("unmanaged-direct missing from result after AddUser")
	}
	if !jsonSemanticallyEqual(origDirect, resultDirect) {
		t.Errorf("unmanaged-direct inbound changed after AddUser:\n  original: %s\n  result:   %s", origDirect, resultDirect)
	}
}

// TestRoundTrip_ExperimentalUnknownKeyPreserved verifies that known and unknown
// sub-keys inside experimental (e.g. clash_api) survive an AddUser mutation.
func TestRoundTrip_ExperimentalUnknownKeyPreserved(t *testing.T) {
	fixtureJSON := `{
		"inbounds": [
			{
				"type": "vless",
				"tag": "test-vless",
				"listen": "0.0.0.0",
				"listen_port": 1080,
				"users": []
			}
		],
		"experimental": {
			"clash_api": {"external_controller": "0.0.0.0:9090"}
		}
	}`

	cfg, stub := newTestConfig(t, fixtureJSON)

	if err := cfg.AddUser("alice", "11111111-1111-1111-1111-111111111111", "", "test-vless", "", 0); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	// Parse original experimental
	var origTop map[string]json.RawMessage
	if err := json.Unmarshal([]byte(fixtureJSON), &origTop); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	var origExp Experimental
	if err := json.Unmarshal(origTop["experimental"], &origExp); err != nil {
		t.Fatalf("unmarshal orig experimental: %v", err)
	}

	// Parse result experimental
	var resultTop map[string]json.RawMessage
	if err := json.Unmarshal(stub.data, &resultTop); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	expRaw, ok := resultTop["experimental"]
	if !ok {
		t.Fatal("experimental section missing from result")
	}
	var resultExp Experimental
	if err := json.Unmarshal(expRaw, &resultExp); err != nil {
		t.Fatalf("unmarshal result experimental: %v", err)
	}

	// clash_api must be preserved
	if resultExp.ClashAPI == nil {
		t.Error("clash_api missing from experimental after AddUser")
	}
	origClash, err := json.Marshal(origExp.ClashAPI)
	if err != nil {
		t.Fatalf("marshal orig clash_api: %v", err)
	}
	resultClash, err := json.Marshal(resultExp.ClashAPI)
	if err != nil {
		t.Fatalf("marshal result clash_api: %v", err)
	}
	if !jsonSemanticallyEqual(origClash, resultClash) {
		t.Errorf("clash_api changed after AddUser:\n  original: %s\n  result:   %s", origClash, resultClash)
	}
}

func TestAddUser_Shadowsocks_PreservesExplicitEmptyClashAPISecret(t *testing.T) {
	fixtureJSON := `{
		"inbounds": [
			{
				"type": "shadowsocks",
				"tag": "test-ss",
				"listen": "0.0.0.0",
				"listen_port": 10005,
				"method": "2022-blake3-aes-128-gcm",
				"users": []
			}
		],
		"experimental": {
			"clash_api": {
				"external_controller": "127.0.0.1:9090",
				"secret": ""
			}
		}
	}`

	cfg, stub := newTestConfig(t, fixtureJSON)
	cfg.ManagedInbounds = []string{"test-ss"}
	cfg.StatsInbounds = []string{"test-ss"}

	if err := cfg.AddUser("dora", "shadow-secret", "", "test-ss", "", 0); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	var result map[string]json.RawMessage
	if err := json.Unmarshal(stub.data, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	var experimental map[string]json.RawMessage
	if err := json.Unmarshal(result["experimental"], &experimental); err != nil {
		t.Fatalf("unmarshal experimental: %v", err)
	}

	var clash map[string]json.RawMessage
	if err := json.Unmarshal(experimental["clash_api"], &clash); err != nil {
		t.Fatalf("unmarshal clash_api: %v", err)
	}

	secretRaw, ok := clash["secret"]
	if !ok {
		t.Fatal("clash_api.secret missing after AddUser")
	}
	if string(secretRaw) != `""` {
		t.Fatalf("clash_api.secret = %s, want empty string", secretRaw)
	}
}

func TestAddUser_Shadowsocks_ClashAPIKeysOutOfAlphabeticalOrder(t *testing.T) {
	// clash_api keys in non-alphabetical order: "secret" before "external_controller".
	// ClashAPI.MarshalJSON uses a map so Go's json.Marshal sorts keys alphabetically.
	// assertExperimentalAllowedChanges must compare semantically, not byte-by-byte.
	fixtureJSON := `{
		"inbounds": [
			{
				"type": "shadowsocks",
				"tag": "test-ss",
				"listen": "0.0.0.0",
				"listen_port": 10005,
				"method": "2022-blake3-aes-128-gcm",
				"users": []
			}
		],
		"experimental": {
			"clash_api": {
				"secret": "",
				"external_controller": "127.0.0.1:9090"
			}
		}
	}`

	cfg, _ := newTestConfig(t, fixtureJSON)
	cfg.ManagedInbounds = []string{"test-ss"}
	cfg.StatsInbounds = []string{"test-ss"}

	if err := cfg.AddUser("dora", "shadow-secret", "", "test-ss", "", 0); err != nil {
		t.Fatalf("AddUser failed with out-of-order clash_api keys: %v", err)
	}
}

func TestClashAPI_MarshalUnmarshalRoundTrip(t *testing.T) {
	orig := ClashAPI{ExternalController: "127.0.0.1:9090", Secret: "synthetic-secret"}

	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal clash api: %v", err)
	}
	if !strings.Contains(string(data), `"external_controller":"127.0.0.1:9090"`) {
		t.Fatalf("marshal output %s missing external_controller", data)
	}
	if !strings.Contains(string(data), `"secret":"synthetic-secret"`) {
		t.Fatalf("marshal output %s missing secret", data)
	}

	var got ClashAPI
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal clash api: %v", err)
	}
	if got.ExternalController != orig.ExternalController {
		t.Fatalf("ExternalController = %q, want %q", got.ExternalController, orig.ExternalController)
	}
	if got.Secret != orig.Secret {
		t.Fatalf("Secret = %q, want %q", got.Secret, orig.Secret)
	}
	if got.Extra != nil {
		t.Fatalf("Extra = %#v, want nil", got.Extra)
	}
}

func TestClashAPI_ExtraPreserved(t *testing.T) {
	input := `{"external_controller":"127.0.0.1:9090","default_mode":"rule","store_selected":true}`

	var api ClashAPI
	if err := json.Unmarshal([]byte(input), &api); err != nil {
		t.Fatalf("unmarshal clash api: %v", err)
	}
	if api.ExternalController != "127.0.0.1:9090" {
		t.Fatalf("ExternalController = %q, want %q", api.ExternalController, "127.0.0.1:9090")
	}
	if api.Extra == nil {
		t.Fatal("Extra = nil, want preserved keys")
	}
	if _, ok := api.Extra["default_mode"]; !ok {
		t.Fatalf("default_mode missing from Extra: %#v", api.Extra)
	}
	if _, ok := api.Extra["store_selected"]; !ok {
		t.Fatalf("store_selected missing from Extra: %#v", api.Extra)
	}

	data, err := json.Marshal(api)
	if err != nil {
		t.Fatalf("marshal clash api: %v", err)
	}
	if !strings.Contains(string(data), `"external_controller":"127.0.0.1:9090"`) {
		t.Fatalf("marshal output %s missing external_controller", data)
	}
	if !strings.Contains(string(data), `"default_mode":"rule"`) {
		t.Fatalf("marshal output %s missing default_mode", data)
	}
	if !strings.Contains(string(data), `"store_selected":true`) {
		t.Fatalf("marshal output %s missing store_selected", data)
	}
}

func TestClashAPI_EmptyFields(t *testing.T) {
	data, err := json.Marshal(ClashAPI{})
	if err != nil {
		t.Fatalf("marshal empty clash api: %v", err)
	}
	if string(data) != "{}" {
		t.Fatalf("marshal output = %s, want {}", data)
	}
}

func TestClashAPI_PreservesExplicitEmptyKnownFields(t *testing.T) {
	input := `{"external_controller":"","secret":""}`

	var api ClashAPI
	if err := json.Unmarshal([]byte(input), &api); err != nil {
		t.Fatalf("unmarshal clash api: %v", err)
	}

	data, err := json.Marshal(api)
	if err != nil {
		t.Fatalf("marshal clash api: %v", err)
	}
	if !jsonSemanticallyEqual([]byte(input), data) {
		t.Fatalf("marshal output = %s, want semantic match with %s", data, input)
	}
}

func TestExperimental_ClashAPITyped(t *testing.T) {
	input := `{"clash_api":{"external_controller":"0.0.0.0:9090"}}`

	var exp Experimental
	if err := json.Unmarshal([]byte(input), &exp); err != nil {
		t.Fatalf("unmarshal experimental: %v", err)
	}
	if exp.ClashAPI == nil {
		t.Fatal("ClashAPI = nil, want typed struct")
	}
	if exp.ClashAPI.ExternalController != "0.0.0.0:9090" {
		t.Fatalf("ExternalController = %q, want %q", exp.ClashAPI.ExternalController, "0.0.0.0:9090")
	}
}

func TestExperimental_ClashAPINil(t *testing.T) {
	var exp Experimental
	if err := json.Unmarshal([]byte(`{"cache_file":{"enabled":true}}`), &exp); err != nil {
		t.Fatalf("unmarshal experimental: %v", err)
	}
	if exp.ClashAPI != nil {
		t.Fatalf("ClashAPI = %#v, want nil", exp.ClashAPI)
	}
}

// TestModifySingboxConfig_RejectsNonInboundChange verifies that
// assertAllowedScopeChanges blocks modifications to non-inbound top-level sections.
func TestModifySingboxConfig_RejectsNonInboundChange(t *testing.T) {
	fixtureJSON := `{
		"inbounds": [
			{
				"type": "vless",
				"tag": "test-vless",
				"listen": "0.0.0.0",
				"listen_port": 1080,
				"users": []
			}
		]
	}`

	cfg, _ := newTestConfig(t, fixtureJSON)

	// Modifier changes cfg.Log — assertAllowedScopeChanges should reject this
	err := cfg.ModifySingboxConfig(func(scfg *SingboxConfig) error {
		scfg.Log = json.RawMessage(`{"level":"debug"}`)
		return nil
	})

	if err == nil {
		t.Fatal("expected ModifySingboxConfig to return error when Log is mutated, got nil")
	}
}

func TestUpdateSingboxDNS_ReplacesSectionAndPreservesOthers(t *testing.T) {
	fixtureJSON := `{
		"dns": {"servers": [{"tag": "old", "address": "8.8.8.8"}]},
		"outbounds": [{"type": "direct", "tag": "direct"}],
		"inbounds": [{"type": "vless", "tag": "test-vless", "listen_port": 1080, "users": []}]
	}`

	cfg, stub := newTestConfig(t, fixtureJSON)

	nextDNS := map[string]interface{}{
		"servers": []interface{}{
			map[string]interface{}{"tag": "dns-google-v6", "address": "2001:4860:4860::8888", "strategy": "prefer_ipv6"},
			map[string]interface{}{"tag": "dns-g", "server": "8.8.8.8", "type": "udp"},
		},
		"strategy":          "prefer_ipv6",
		"independent_cache": true,
	}

	if err := cfg.UpdateSingboxDNS(nextDNS); err != nil {
		t.Fatalf("UpdateSingboxDNS: %v", err)
	}

	var result map[string]json.RawMessage
	if err := json.Unmarshal(stub.data, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	gotDNS := result["dns"]
	wantDNS, err := json.Marshal(nextDNS)
	if err != nil {
		t.Fatalf("marshal want dns: %v", err)
	}
	if !jsonSemanticallyEqual(gotDNS, wantDNS) {
		t.Fatalf("dns section mismatch:\n got: %s\nwant: %s", gotDNS, wantDNS)
	}

	var original map[string]json.RawMessage
	if err := json.Unmarshal([]byte(fixtureJSON), &original); err != nil {
		t.Fatalf("unmarshal original: %v", err)
	}
	if !jsonSemanticallyEqual(result["outbounds"], original["outbounds"]) {
		t.Fatalf("outbounds changed unexpectedly:\n got: %s\nwant: %s", result["outbounds"], original["outbounds"])
	}
}

func TestUpdateSingboxOutboundDomainStrategies_UpdatesMatchingTagsOnly(t *testing.T) {
	fixtureJSON := `{
		"dns": {"servers": [{"tag": "google", "address": "8.8.8.8"}]},
		"outbounds": [
			{"type": "direct", "tag": "direct"},
			{"type": "socks", "tag": "proxy", "server": "1.2.3.4", "server_port": 1080}
		],
		"inbounds": [{"type": "vless", "tag": "test-vless", "listen_port": 1080, "users": []}]
	}`

	cfg, stub := newTestConfig(t, fixtureJSON)

	err := cfg.UpdateSingboxOutboundDomainStrategies([]SingboxOutboundDomainStrategyUpdate{
		{Tag: "direct", DomainStrategy: "prefer_ipv6"},
		{Tag: "proxy", DomainStrategy: ""},
	})
	if err != nil {
		t.Fatalf("UpdateSingboxOutboundDomainStrategies: %v", err)
	}

	var result map[string]json.RawMessage
	if err := json.Unmarshal(stub.data, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}

	var outbounds []map[string]interface{}
	if err := json.Unmarshal(result["outbounds"], &outbounds); err != nil {
		t.Fatalf("unmarshal outbounds: %v", err)
	}

	if got, _ := outbounds[0]["domain_strategy"].(string); got != "prefer_ipv6" {
		t.Fatalf("direct.domain_strategy = %q; want %q", got, "prefer_ipv6")
	}
	if _, ok := outbounds[1]["domain_strategy"]; ok {
		t.Fatalf("proxy.domain_strategy should be removed when empty")
	}
}

func TestTLSTyped_RealityDecoded(t *testing.T) {
	fixtureJSON := `{
        "inbounds": [{
            "type": "vless",
            "tag": "test-vless",
            "listen": "0.0.0.0",
            "listen_port": 443,
            "users": [],
            "tls": {
                "enabled": true,
                "server_name": "example.com",
                "reality": {
                    "enabled": true,
                    "handshake": {"server": "example.com", "server_port": 443},
                    "private_key": "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
                    "public_key":  "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=",
                    "short_id": ["deadbeef", "cafebabe"]
                }
            }
        }]
    }`
	cfg, _ := newTestConfig(t, fixtureJSON)
	view, err := cfg.GetSingboxInboundView("test-vless")
	if err != nil {
		t.Fatalf("GetSingboxInboundView: %v", err)
	}
	if view.TLS == nil {
		t.Fatal("TLS is nil; expected *TLSConfig")
	}
	if !view.TLS.Enabled {
		t.Error("TLS.Enabled should be true")
	}
	if view.TLS.ServerName != "example.com" {
		t.Errorf("TLS.ServerName = %q; want %q", view.TLS.ServerName, "example.com")
	}
	if view.TLS.Reality == nil {
		t.Fatal("TLS.Reality is nil; expected *RealityConfig")
	}
	if view.TLS.Reality.Handshake.Server != "example.com" {
		t.Errorf("Reality.Handshake.Server = %q; want %q", view.TLS.Reality.Handshake.Server, "example.com")
	}
	if len(view.TLS.Reality.ShortIDs) != 2 {
		t.Errorf("Reality.ShortIDs len = %d; want 2", len(view.TLS.Reality.ShortIDs))
	}
	if view.TLS.Reality.ShortIDs[0] != "deadbeef" {
		t.Errorf("Reality.ShortIDs[0] = %q; want %q", view.TLS.Reality.ShortIDs[0], "deadbeef")
	}
}

func TestTLSTyped_StandardTLSDecoded(t *testing.T) {
	fixtureJSON := `{
        "inbounds": [{
            "type": "trojan",
            "tag": "test-trojan",
            "listen": "0.0.0.0",
            "listen_port": 443,
            "users": [],
            "tls": {
                "enabled": true,
                "server_name": "my.domain.com",
                "alpn": ["h2", "http/1.1"],
                "certificate_path": "/etc/ssl/cert.pem"
            }
        }]
    }`
	cfg, _ := newTestConfig(t, fixtureJSON)
	view, err := cfg.GetSingboxInboundView("test-trojan")
	if err != nil {
		t.Fatalf("GetSingboxInboundView: %v", err)
	}
	if view.TLS == nil {
		t.Fatal("TLS is nil; expected *TLSConfig")
	}
	if !view.TLS.Enabled {
		t.Error("TLS.Enabled should be true")
	}
	if view.TLS.ServerName != "my.domain.com" {
		t.Errorf("TLS.ServerName = %q; want %q", view.TLS.ServerName, "my.domain.com")
	}
	if len(view.TLS.ALPN) != 2 || view.TLS.ALPN[0] != "h2" || view.TLS.ALPN[1] != "http/1.1" {
		t.Errorf("TLS.ALPN = %#v; want [\"h2\", \"http/1.1\"]", view.TLS.ALPN)
	}
	if view.TLS.CertificatePath != "/etc/ssl/cert.pem" {
		t.Errorf("TLS.CertificatePath = %q; want %q", view.TLS.CertificatePath, "/etc/ssl/cert.pem")
	}
	if view.TLS.Reality != nil {
		t.Error("TLS.Reality should be nil for standard TLS inbound")
	}
}

func TestTLSTyped_AbsentTLSIsNil(t *testing.T) {
	fixtureJSON := `{
        "inbounds": [{
            "type": "vmess",
            "tag": "test-vmess",
            "listen": "0.0.0.0",
            "listen_port": 10080,
            "users": []
        }]
    }`
	cfg, _ := newTestConfig(t, fixtureJSON)
	view, err := cfg.GetSingboxInboundView("test-vmess")
	if err != nil {
		t.Fatalf("GetSingboxInboundView: %v", err)
	}
	if view.TLS != nil {
		t.Errorf("TLS should be nil for inbound without tls block; got %+v", view.TLS)
	}
}

func TestGetUserInbounds_Hysteria2(t *testing.T) {
	fixtureJSON := `{
		"inbounds": [
			{
				"type": "hysteria2",
				"tag": "hy2-in",
				"listen_port": 8443,
				"users": [
					{"name": "alice", "password": "s3cr3t"}
				]
			}
		]
	}`
	cfg, _ := newTestConfig(t, fixtureJSON)
	results, err := cfg.GetUserInbounds("alice")
	if err != nil {
		t.Fatalf("GetUserInbounds() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("GetUserInbounds() returned %d entries; want 1", len(results))
	}
	info := results[0]
	if info.Password != "s3cr3t" {
		t.Errorf("Password = %q; want %q", info.Password, "s3cr3t")
	}
	if info.UUID != "" {
		t.Errorf("UUID = %q; want empty string", info.UUID)
	}
}
