package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// fixtureJSON is the canonical test fixture: three managed inbounds (vless, vmess, trojan)
// plus one unmanaged direct inbound, and an experimental v2ray_api section.
const fixtureJSON = `{
  "inbounds": [
    {"type":"vless","tag":"test-vless","listen":"0.0.0.0","listen_port":10001,"users":[]},
    {"type":"vmess","tag":"test-vmess","listen":"0.0.0.0","listen_port":10002,"users":[]},
    {"type":"trojan","tag":"test-trojan","listen":"0.0.0.0","listen_port":10003,"users":[]},
    {"type":"direct","tag":"unmanaged-direct","listen_port":10004}
  ],
  "experimental": {
    "v2ray_api": {"listen":"127.0.0.1:19001","stats":{"enabled":true,"inbounds":["test-vless"],"outbounds":["direct"],"users":[]}}
  }
}`

// readStatsUsers reads the stub's in-memory JSON and returns the
// experimental.v2ray_api.stats.users list.
func readStatsUsers(t *testing.T, stub *stubExecutor) []string {
	t.Helper()
	raw, err := stub.ReadConfig(context.Background(), "/test/config.json")
	if err != nil {
		t.Fatalf("readStatsUsers: ReadConfig: %v", err)
	}
	var top map[string]interface{}
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("readStatsUsers: unmarshal: %v", err)
	}
	exp, ok := top["experimental"].(map[string]interface{})
	if !ok {
		return nil
	}
	v2, ok := exp["v2ray_api"].(map[string]interface{})
	if !ok {
		return nil
	}
	stats, ok := v2["stats"].(map[string]interface{})
	if !ok {
		return nil
	}
	usersRaw, ok := stats["users"].([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(usersRaw))
	for _, u := range usersRaw {
		if s, ok := u.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// containsString returns true if slice contains s.
func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// CRUD — AddUser (TEST-01)
// ---------------------------------------------------------------------------

func TestAddUser_Vless(t *testing.T) {
	cfg, _ := newTestConfig(t, fixtureJSON)
	const uuid = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	if err := cfg.AddUser("alice", uuid, "", "test-vless", "", 0); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	inbounds, err := cfg.GetUserInbounds("alice")
	if err != nil {
		t.Fatalf("GetUserInbounds: %v", err)
	}
	if len(inbounds) != 1 {
		t.Fatalf("expected 1 inbound entry, got %d", len(inbounds))
	}
	if inbounds[0].Tag != "test-vless" {
		t.Errorf("expected Tag=test-vless, got %q", inbounds[0].Tag)
	}
}

func TestAddUser_Vmess(t *testing.T) {
	cfg, _ := newTestConfig(t, fixtureJSON)
	const uuid = "bbbbbbbb-cccc-dddd-eeee-ffffffffffff"

	if err := cfg.AddUser("bob", uuid, "", "test-vmess", "", 0); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	inbounds, err := cfg.GetUserInbounds("bob")
	if err != nil {
		t.Fatalf("GetUserInbounds: %v", err)
	}
	if len(inbounds) != 1 {
		t.Fatalf("expected 1 inbound entry, got %d", len(inbounds))
	}
	if inbounds[0].Tag != "test-vmess" {
		t.Errorf("expected Tag=test-vmess, got %q", inbounds[0].Tag)
	}
}

func TestAddUser_Trojan(t *testing.T) {
	cfg, _ := newTestConfig(t, fixtureJSON)
	// For Trojan, the "uuid" parameter is stored as the password field.
	const password = "supersecretpassword"

	if err := cfg.AddUser("carol", password, "", "test-trojan", "", 0); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	inbounds, err := cfg.GetUserInbounds("carol")
	if err != nil {
		t.Fatalf("GetUserInbounds: %v", err)
	}
	if len(inbounds) != 1 {
		t.Fatalf("expected 1 inbound entry, got %d", len(inbounds))
	}
	if inbounds[0].Tag != "test-trojan" {
		t.Errorf("expected Tag=test-trojan, got %q", inbounds[0].Tag)
	}
	// GetUserInbounds maps Trojan password → UUID field for callers.
	if inbounds[0].UUID != password {
		t.Errorf("expected UUID=%q (trojan password), got %q", password, inbounds[0].UUID)
	}
}

func TestAddUser_Duplicate(t *testing.T) {
	cfg, _ := newTestConfig(t, fixtureJSON)
	const uuid = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	if err := cfg.AddUser("alice", uuid, "", "test-vless", "", 0); err != nil {
		t.Fatalf("first AddUser: %v", err)
	}

	// Duplicate add must return a non-nil error containing "already exists".
	err := cfg.AddUser("alice", uuid, "", "test-vless", "", 0)
	if err == nil {
		t.Fatal("expected error for duplicate AddUser, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected error to contain %q, got %q", "already exists", err.Error())
	}
}

// ---------------------------------------------------------------------------
// CRUD — RemoveUser (TEST-01)
// ---------------------------------------------------------------------------

func TestRemoveUser(t *testing.T) {
	cfg, _ := newTestConfig(t, fixtureJSON)
	const uuid = "cccccccc-dddd-eeee-ffff-aaaaaaaaaaaa"

	if err := cfg.AddUser("dave", uuid, "", "test-vless", "", 0); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	if err := cfg.RemoveUser("dave"); err != nil {
		t.Fatalf("RemoveUser: %v", err)
	}

	inbounds, err := cfg.GetUserInbounds("dave")
	if err != nil {
		t.Fatalf("GetUserInbounds after remove: %v", err)
	}
	if len(inbounds) != 0 {
		t.Errorf("expected 0 inbound entries after RemoveUser, got %d", len(inbounds))
	}
}

func TestRemoveUser_NotFound(t *testing.T) {
	// RemoveUser on a non-existent user should NOT return an error.
	// The implementation removes from all managed inbounds silently when the user
	// is not present (idempotent no-op).
	cfg, _ := newTestConfig(t, fixtureJSON)

	err := cfg.RemoveUser("nonexistent-user")
	if err != nil {
		t.Errorf("RemoveUser on non-existent user returned unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// CRUD — UpdateUserInInbound (TEST-01)
// ---------------------------------------------------------------------------

func TestUpdateUserInInbound(t *testing.T) {
	cfg, _ := newTestConfig(t, fixtureJSON)
	const oldUUID = "aaaaaaaa-1111-2222-3333-eeeeeeeeeeee"
	const newUUID = "ffffffff-4444-5555-6666-aaaaaaaaaaaa"

	if err := cfg.AddUser("eve", oldUUID, "", "test-vless", "", 0); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	if err := cfg.UpdateUserInInbound("eve", newUUID, "", "test-vless", "", 0); err != nil {
		t.Fatalf("UpdateUserInInbound: %v", err)
	}

	inbounds, err := cfg.GetUserInbounds("eve")
	if err != nil {
		t.Fatalf("GetUserInbounds: %v", err)
	}
	if len(inbounds) == 0 {
		t.Fatal("expected user to remain after update, got 0 inbound entries")
	}
	if inbounds[0].UUID != newUUID {
		t.Errorf("expected UUID=%q after update, got %q", newUUID, inbounds[0].UUID)
	}
}

// ---------------------------------------------------------------------------
// syncStatsUsers continuity (TEST-03)
// ---------------------------------------------------------------------------

func TestSyncStatsUsers_AddThenRemove(t *testing.T) {
	cfg, stub := newTestConfig(t, fixtureJSON)

	// 1. Add "frank" to vless — stats must include "frank".
	if err := cfg.AddUser("frank", "aaaaaaaa-ffff-ffff-ffff-000000000001", "", "test-vless", "", 0); err != nil {
		t.Fatalf("AddUser frank: %v", err)
	}
	users := readStatsUsers(t, stub)
	if !containsString(users, "frank") {
		t.Errorf("after AddUser(frank): stats.users = %v, want to contain %q", users, "frank")
	}

	// 2. Add "grace" to vmess — stats must include both "frank" and "grace".
	if err := cfg.AddUser("grace", "aaaaaaaa-ffff-ffff-ffff-000000000002", "", "test-vmess", "", 0); err != nil {
		t.Fatalf("AddUser grace: %v", err)
	}
	users = readStatsUsers(t, stub)
	if !containsString(users, "frank") {
		t.Errorf("after AddUser(grace): stats.users = %v, want to contain %q", users, "frank")
	}
	if !containsString(users, "grace") {
		t.Errorf("after AddUser(grace): stats.users = %v, want to contain %q", users, "grace")
	}

	// 3. Remove "frank" — stats must contain only "grace", not "frank".
	if err := cfg.RemoveUser("frank"); err != nil {
		t.Fatalf("RemoveUser frank: %v", err)
	}
	users = readStatsUsers(t, stub)
	if containsString(users, "frank") {
		t.Errorf("after RemoveUser(frank): stats.users = %v, must not contain %q", users, "frank")
	}
	if !containsString(users, "grace") {
		t.Errorf("after RemoveUser(frank): stats.users = %v, want to contain %q", users, "grace")
	}
}

// TestAddRemoveAdd verifies idempotency: add → remove → add results in exactly one entry.
func TestAddRemoveAdd(t *testing.T) {
	cfg, _ := newTestConfig(t, fixtureJSON)
	const uuid = "dddddddd-eeee-ffff-0000-111111111111"

	if err := cfg.AddUser("henry", uuid, "", "test-vless", "", 0); err != nil {
		t.Fatalf("first AddUser: %v", err)
	}
	if err := cfg.RemoveUser("henry"); err != nil {
		t.Fatalf("RemoveUser: %v", err)
	}
	if err := cfg.AddUser("henry", uuid, "", "test-vless", "", 0); err != nil {
		t.Fatalf("second AddUser: %v", err)
	}

	inbounds, err := cfg.GetUserInbounds("henry")
	if err != nil {
		t.Fatalf("GetUserInbounds: %v", err)
	}
	if len(inbounds) != 1 {
		t.Errorf("expected exactly 1 inbound entry after add-remove-add, got %d", len(inbounds))
	}
}
