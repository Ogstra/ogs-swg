package core

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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

func TestAddUser_Shadowsocks(t *testing.T) {
	cfg, _ := newTestConfig(t, `{
  "inbounds": [
    {"type":"shadowsocks","tag":"test-ss","listen":"0.0.0.0","listen_port":10005,"method":"2022-blake3-aes-128-gcm","users":[]}
  ],
  "experimental": {
    "v2ray_api": {"listen":"127.0.0.1:19001","stats":{"enabled":true,"inbounds":["test-ss"],"outbounds":["direct"],"users":[]}}
  }
}`)
	cfg.ManagedInbounds = []string{"test-ss"}
	cfg.StatsInbounds = []string{"test-ss"}

	const password = "shadow-secret"
	if err := cfg.AddUser("dora", password, "", "test-ss", "", 0); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	inbounds, err := cfg.GetUserInbounds("dora")
	if err != nil {
		t.Fatalf("GetUserInbounds: %v", err)
	}
	if len(inbounds) != 1 {
		t.Fatalf("expected 1 inbound entry, got %d", len(inbounds))
	}
	if inbounds[0].Tag != "test-ss" {
		t.Errorf("expected Tag=test-ss, got %q", inbounds[0].Tag)
	}
	if inbounds[0].Password != password {
		t.Errorf("expected Password=%q, got %q", password, inbounds[0].Password)
	}
	if inbounds[0].UUID != "" {
		t.Errorf("expected UUID empty for shadowsocks, got %q", inbounds[0].UUID)
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

func TestAddUser_RejectsMultipleInboundsPerUser(t *testing.T) {
	cfg, _ := newTestConfig(t, fixtureJSON)
	const uuid = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	if err := cfg.AddUser("alice", uuid, "", "test-vless", "", 0); err != nil {
		t.Fatalf("first AddUser: %v", err)
	}

	err := cfg.AddUser("alice", uuid, "", "test-vmess", "", 0)
	if err == nil {
		t.Fatal("expected error when assigning user to a second inbound, got nil")
	}
	if !errors.Is(err, ErrUserAssignedToAnotherInbound) {
		t.Fatalf("expected ErrUserAssignedToAnotherInbound, got %v", err)
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

func TestUpdateUserInInbound_Shadowsocks(t *testing.T) {
	cfg, _ := newTestConfig(t, `{
  "inbounds": [
    {"type":"shadowsocks","tag":"test-ss","listen":"0.0.0.0","listen_port":10005,"method":"2022-blake3-aes-128-gcm","users":[]}
  ]
}`)
	cfg.ManagedInbounds = []string{"test-ss"}
	cfg.StatsInbounds = []string{"test-ss"}

	if err := cfg.AddUser("eve", "old-secret", "", "test-ss", "", 0); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	if err := cfg.UpdateUserInInbound("eve", "new-secret", "", "test-ss", "", 0); err != nil {
		t.Fatalf("UpdateUserInInbound: %v", err)
	}

	inbounds, err := cfg.GetUserInbounds("eve")
	if err != nil {
		t.Fatalf("GetUserInbounds: %v", err)
	}
	if len(inbounds) != 1 {
		t.Fatalf("expected 1 inbound entry, got %d", len(inbounds))
	}
	if inbounds[0].Password != "new-secret" {
		t.Errorf("expected Password=%q after update, got %q", "new-secret", inbounds[0].Password)
	}
}

func TestUpdateSingboxInbound_WebSocketSwitchStripsStoredUserFlow(t *testing.T) {
	cfg, stub := newTestConfig(t, `{
		"inbounds": [
			{
				"type":"vless",
				"tag":"test-vless",
				"listen":"0.0.0.0",
				"listen_port":443,
				"users":[
					{"name":"alice","uuid":"11111111-1111-1111-1111-111111111111","flow":"xtls-rprx-vision"}
				],
				"tls":{"enabled":true,"server_name":"example.com"},
				"transport":{"type":"tcp"}
			}
		]
	}`)

	err := cfg.UpdateSingboxInbound("test-vless", map[string]interface{}{
		"type":        "vless",
		"tag":         "test-vless",
		"listen":      "0.0.0.0",
		"listen_port": float64(443),
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
		},
		"transport": map[string]interface{}{
			"type": "ws",
			"path": "/ws",
		},
	})
	if err != nil {
		t.Fatalf("UpdateSingboxInbound: %v", err)
	}

	raw, err := stub.ReadConfig(context.Background(), "/test/config.json")
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	var top map[string]interface{}
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("unmarshal top-level config: %v", err)
	}
	inbounds, ok := top["inbounds"].([]interface{})
	if !ok || len(inbounds) != 1 {
		t.Fatalf("inbounds = %#v; want one inbound", top["inbounds"])
	}
	inbound, ok := inbounds[0].(map[string]interface{})
	if !ok {
		t.Fatalf("inbound = %#v; want object", inbounds[0])
	}
	users, ok := inbound["users"].([]interface{})
	if !ok || len(users) != 1 {
		t.Fatalf("users = %#v; want one user", inbound["users"])
	}
	user, ok := users[0].(map[string]interface{})
	if !ok {
		t.Fatalf("user = %#v; want object", users[0])
	}
	if got := user["flow"]; got != nil {
		t.Fatalf("stored user flow = %#v; want removed", got)
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

func TestLoadConfig_AutoDerivesWireGuardConfigDir(t *testing.T) {
	clearWireGuardEnv(t)

	cfgPath := writeConfigFixture(t, `{
  "wireguard_config_path": "/etc/wireguard/wg0.conf"
}`)

	cfg := LoadConfig(cfgPath)
	if cfg.WireGuardConfigDir != "/etc/wireguard" {
		t.Fatalf("WireGuardConfigDir=%q, want %q", cfg.WireGuardConfigDir, "/etc/wireguard")
	}
}

func TestLoadConfig_PreservesExplicitWireGuardConfigDir(t *testing.T) {
	clearWireGuardEnv(t)

	explicit := filepath.Clean("/custom/wireguard/../wireguard-config")
	cfgPath := writeConfigFixture(t, `{
  "wireguard_config_path": "/etc/wireguard/wg0.conf",
  "wireguard_config_dir": "/custom/wireguard/../wireguard-config"
}`)

	cfg := LoadConfig(cfgPath)
	if cfg.WireGuardConfigDir != explicit {
		t.Fatalf("WireGuardConfigDir=%q, want %q", cfg.WireGuardConfigDir, explicit)
	}
}

func clearWireGuardEnv(t *testing.T) {
	t.Helper()
	_ = os.Unsetenv("OGS_WIREGUARD_CONFIG_PATH")
	_ = os.Unsetenv("OGS_WIREGUARD_CONFIG_DIR")
}

func writeConfigFixture(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}
