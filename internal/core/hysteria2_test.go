package core

import (
	"encoding/json"
	"strings"
	"testing"
)

// Fixture JSON constants reused across Hysteria2 tests.
const hy2FixtureJSON = `{
    "inbounds": [
        {
            "type": "hysteria2",
            "tag": "hy2-in",
            "listen": "0.0.0.0",
            "listen_port": 8443,
            "users": [],
            "tls": {"enabled": true}
        }
    ]
}`

const hy2FixtureWithUserJSON = `{
    "inbounds": [
        {
            "type": "hysteria2",
            "tag": "hy2-in",
            "listen": "0.0.0.0",
            "listen_port": 8443,
            "users": [{"name":"alice","password":"pass"}],
            "tls": {"enabled": true}
        }
    ]
}`

// TestHysteria2Struct_JSONRoundTrip verifies that Hysteria2Inbound marshals and
// unmarshals correctly, preserving all typed fields, and that no "uuid" key
// appears in the marshalled output. (HY2-01, HY2-04)
func TestHysteria2Struct_JSONRoundTrip(t *testing.T) {
	original := Hysteria2Inbound{
		InboundBase: InboundBase{Type: "hysteria2", Tag: "hy2-in"},
		UpMbps:      100,
		DownMbps:    50,
		Obfs:        &Hysteria2Obfs{Type: "salamander", Password: "obfs-secret"},
		Users:       []Hysteria2User{{Name: "alice", Password: "pass1"}},
		TLS:         json.RawMessage(`{"enabled":true}`),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	// No "uuid" key must appear in marshalled JSON.
	if strings.Contains(string(data), `"uuid"`) {
		t.Errorf("marshalled JSON must not contain \"uuid\" key; got: %s", data)
	}

	var decoded Hysteria2Inbound
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if decoded.UpMbps != 100 {
		t.Errorf("UpMbps = %d; want 100", decoded.UpMbps)
	}
	if decoded.DownMbps != 50 {
		t.Errorf("DownMbps = %d; want 50", decoded.DownMbps)
	}
	if decoded.Obfs == nil {
		t.Fatal("Obfs is nil; want non-nil")
	}
	if decoded.Obfs.Type != "salamander" {
		t.Errorf("Obfs.Type = %q; want %q", decoded.Obfs.Type, "salamander")
	}
	if len(decoded.Users) != 1 {
		t.Fatalf("len(Users) = %d; want 1", len(decoded.Users))
	}
	if decoded.Users[0].Password != "pass1" {
		t.Errorf("Users[0].Password = %q; want %q", decoded.Users[0].Password, "pass1")
	}
	if decoded.Users[0].Name != "alice" {
		t.Errorf("Users[0].Name = %q; want %q", decoded.Users[0].Name, "alice")
	}
}

// TestHysteria2Struct_OmitemptyFields verifies that zero-value UpMbps/DownMbps
// and nil Obfs are omitted from the marshalled JSON. (HY2-04)
func TestHysteria2Struct_OmitemptyFields(t *testing.T) {
	minimal := Hysteria2Inbound{
		InboundBase: InboundBase{Type: "hysteria2", Tag: "hy2-in"},
		// UpMbps and DownMbps intentionally zero
		// Obfs intentionally nil
		Users: []Hysteria2User{},
	}

	data, err := json.Marshal(minimal)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	s := string(data)
	if strings.Contains(s, `"up_mbps"`) {
		t.Errorf("marshalled JSON must not contain \"up_mbps\" when zero; got: %s", s)
	}
	if strings.Contains(s, `"down_mbps"`) {
		t.Errorf("marshalled JSON must not contain \"down_mbps\" when zero; got: %s", s)
	}
	if strings.Contains(s, `"obfs"`) {
		t.Errorf("marshalled JSON must not contain \"obfs\" when nil; got: %s", s)
	}
}

// TestHysteria2Struct_ObfsTypeRequired verifies that when an Obfs block is
// present, both "type" and "password" are written (no omitempty on Type). (HY2-04)
func TestHysteria2Struct_ObfsTypeRequired(t *testing.T) {
	obfs := Hysteria2Obfs{Type: "salamander", Password: "p"}
	data, err := json.Marshal(obfs)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	s := string(data)
	if !strings.Contains(s, `"type":"salamander"`) {
		t.Errorf("marshalled Obfs must contain \"type\":\"salamander\"; got: %s", s)
	}
	if !strings.Contains(s, `"password":"p"`) {
		t.Errorf("marshalled Obfs must contain \"password\":\"p\"; got: %s", s)
	}
}

// TestDecodeTypedInbound_Hysteria2 verifies that decodeTypedInbound returns a
// *Hysteria2Inbound with the correct Base() tag and UserNames(). (HY2-01)
func TestDecodeTypedInbound_Hysteria2(t *testing.T) {
	raw := json.RawMessage(`{"type":"hysteria2","tag":"hy2-in","listen":"0.0.0.0","listen_port":8443,"users":[{"name":"bob","password":"secret"}],"tls":{"enabled":true}}`)

	inbound, err := decodeTypedInbound(raw)
	if err != nil {
		t.Fatalf("decodeTypedInbound: %v", err)
	}

	hy2, ok := inbound.(*Hysteria2Inbound)
	if !ok {
		t.Fatalf("decodeTypedInbound returned %T; want *Hysteria2Inbound", inbound)
	}

	if hy2.Base().Tag != "hy2-in" {
		t.Errorf("Base().Tag = %q; want %q", hy2.Base().Tag, "hy2-in")
	}

	names := inbound.UserNames()
	if len(names) != 1 || names[0] != "bob" {
		t.Errorf("UserNames() = %v; want [bob]", names)
	}
}

// TestIsUserInboundType_Hysteria2AndShadowsocks verifies isUserInboundType
// recognises both password-based protocols case-insensitively. (HY2-02)
func TestIsUserInboundType_Hysteria2AndShadowsocks(t *testing.T) {
	if !isUserInboundType("hysteria2") {
		t.Error("isUserInboundType(\"hysteria2\") = false; want true")
	}
	if !isUserInboundType("HYSTERIA2") {
		t.Error("isUserInboundType(\"HYSTERIA2\") = false; want true (case insensitive)")
	}
	if !isUserInboundType("shadowsocks") {
		t.Error("isUserInboundType(\"shadowsocks\") = false; want true")
	}
}

// TestAddUser_Hysteria2_AddsPasswordUser verifies that AddUser appends a
// {name, password} user to a Hysteria2 inbound and that no "uuid" key appears
// for that user in the stored JSON. Duplicate add must error. (HY2-02)
func TestAddUser_Hysteria2_AddsPasswordUser(t *testing.T) {
	cfg, stub := newTestConfig(t, hy2FixtureJSON)
	cfg.ManagedInbounds = []string{"hy2-in"}
	cfg.StatsInbounds = []string{"hy2-in"}

	if err := cfg.AddUser("alice", "mypassword", "", "hy2-in", "", 0); err != nil {
		t.Fatalf("AddUser: %v", err)
	}

	// Re-read from stub and find the hy2-in inbound.
	var top map[string]json.RawMessage
	if err := json.Unmarshal(stub.data, &top); err != nil {
		t.Fatalf("unmarshal stub data: %v", err)
	}
	var inboundList []json.RawMessage
	if err := json.Unmarshal(top["inbounds"], &inboundList); err != nil {
		t.Fatalf("unmarshal inbounds: %v", err)
	}

	var hy2 Hysteria2Inbound
	found := false
	for _, raw := range inboundList {
		var base InboundBase
		if err := json.Unmarshal(raw, &base); err != nil {
			continue
		}
		if base.Tag == "hy2-in" {
			if err := json.Unmarshal(raw, &hy2); err != nil {
				t.Fatalf("unmarshal Hysteria2Inbound: %v", err)
			}
			found = true

			// No "uuid" key should appear for this user.
			if strings.Contains(string(raw), `"uuid"`) {
				t.Errorf("stored JSON for hy2-in must not contain \"uuid\"; got: %s", raw)
			}
			break
		}
	}
	if !found {
		t.Fatal("hy2-in inbound not found in stub data after AddUser")
	}
	if len(hy2.Users) != 1 {
		t.Fatalf("len(Users) = %d; want 1", len(hy2.Users))
	}
	if hy2.Users[0].Name != "alice" || hy2.Users[0].Password != "mypassword" {
		t.Errorf("Users[0] = %+v; want {Name:alice Password:mypassword}", hy2.Users[0])
	}

	// Duplicate add must return error containing "already exists".
	err := cfg.AddUser("alice", "dup", "", "hy2-in", "", 0)
	if err == nil {
		t.Fatal("expected error for duplicate AddUser, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected error to contain \"already exists\"; got: %v", err)
	}
}

// TestRemoveUser_Hysteria2 verifies RemoveUserFromInbound removes the correct
// user from a Hysteria2 inbound and errors on subsequent removal. (HY2-02)
func TestRemoveUser_Hysteria2(t *testing.T) {
	cfg, stub := newTestConfig(t, hy2FixtureWithUserJSON)
	cfg.ManagedInbounds = []string{"hy2-in"}
	cfg.StatsInbounds = []string{"hy2-in"}

	if err := cfg.RemoveUserFromInbound("alice", "hy2-in"); err != nil {
		t.Fatalf("RemoveUserFromInbound: %v", err)
	}

	// Re-read and assert users array is empty for hy2-in.
	var top map[string]json.RawMessage
	if err := json.Unmarshal(stub.data, &top); err != nil {
		t.Fatalf("unmarshal stub data: %v", err)
	}
	var inboundList []json.RawMessage
	if err := json.Unmarshal(top["inbounds"], &inboundList); err != nil {
		t.Fatalf("unmarshal inbounds: %v", err)
	}
	found := false
	for _, raw := range inboundList {
		var hy2 Hysteria2Inbound
		if err := json.Unmarshal(raw, &hy2); err != nil {
			continue
		}
		if hy2.Base().Tag != "hy2-in" {
			continue
		}
		found = true
		if len(hy2.Users) != 0 {
			t.Errorf("expected 0 users after RemoveUserFromInbound; got %d", len(hy2.Users))
		}
		break
	}
	if !found {
		t.Fatal("hy2-in inbound not found in stub data after remove")
	}

	// Second removal must error containing "not found".
	err := cfg.RemoveUserFromInbound("alice", "hy2-in")
	if err == nil {
		t.Fatal("expected error on second RemoveUserFromInbound, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected error to contain \"not found\"; got: %v", err)
	}
}

// TestUpdateUser_Hysteria2_UpdatesPassword verifies UpdateUserInInbound changes
// the password for a Hysteria2 user and that no "flow" field appears for the
// user in the stored JSON. (HY2-02)
func TestUpdateUser_Hysteria2_UpdatesPassword(t *testing.T) {
	cfg, stub := newTestConfig(t, hy2FixtureWithUserJSON)
	cfg.ManagedInbounds = []string{"hy2-in"}
	cfg.StatsInbounds = []string{"hy2-in"}

	if err := cfg.UpdateUserInInbound("alice", "newpassword", "some-flow", "hy2-in", "", 0); err != nil {
		t.Fatalf("UpdateUserInInbound: %v", err)
	}

	// Re-read and assert password was updated.
	var top map[string]json.RawMessage
	if err := json.Unmarshal(stub.data, &top); err != nil {
		t.Fatalf("unmarshal stub data: %v", err)
	}
	var inboundList []json.RawMessage
	if err := json.Unmarshal(top["inbounds"], &inboundList); err != nil {
		t.Fatalf("unmarshal inbounds: %v", err)
	}
	found := false
	for _, raw := range inboundList {
		var hy2 Hysteria2Inbound
		if err := json.Unmarshal(raw, &hy2); err != nil {
			continue
		}
		if hy2.Base().Tag != "hy2-in" {
			continue
		}
		found = true
		if len(hy2.Users) != 1 {
			t.Fatalf("expected 1 user after update; got %d", len(hy2.Users))
		}
		if hy2.Users[0].Password != "newpassword" {
			t.Errorf("Users[0].Password = %q; want %q", hy2.Users[0].Password, "newpassword")
		}
		// "flow" must not appear in the stored JSON for Hysteria2 users.
		if strings.Contains(string(raw), `"flow"`) {
			t.Errorf("stored JSON for hy2-in must not contain \"flow\"; got: %s", raw)
		}
		break
	}
	if !found {
		t.Fatal("hy2-in inbound not found in stub data after update")
	}
}

// TestSyncInboundsFromSingbox_Hysteria2_StatsRegistration verifies that
// SyncInboundsFromSingbox auto-registers a hysteria2 inbound tag into
// StatsInbounds when it is not already listed. (HY2-03)
func TestSyncInboundsFromSingbox_Hysteria2_StatsRegistration(t *testing.T) {
	// Fixture contains both a hysteria2 and a vless inbound; only vl-in is
	// initially in StatsInbounds.
	fixture := `{
        "inbounds": [
            {
                "type": "hysteria2",
                "tag": "hy2-in",
                "listen": "0.0.0.0",
                "listen_port": 8443,
                "users": [],
                "tls": {"enabled": true}
            },
            {
                "type": "vless",
                "tag": "vl-in",
                "listen": "0.0.0.0",
                "listen_port": 1080,
                "users": []
            }
        ]
    }`

	cfg, _ := newTestConfig(t, fixture)
	cfg.ManagedInbounds = []string{"hy2-in", "vl-in"}
	cfg.StatsInbounds = []string{"vl-in"} // hy2-in intentionally absent

	if err := cfg.SyncInboundsFromSingbox(); err != nil {
		t.Fatalf("SyncInboundsFromSingbox: %v", err)
	}

	found := false
	for _, tag := range cfg.StatsInbounds {
		if tag == "hy2-in" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("StatsInbounds = %v; want it to contain \"hy2-in\" after SyncInboundsFromSingbox", cfg.StatsInbounds)
	}
}
