package core

import (
	"encoding/json"
	"testing"
)

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
	if len(resultExp.ClashAPI) == 0 {
		t.Error("clash_api missing from experimental after AddUser")
	}
	if !jsonSemanticallyEqual(origExp.ClashAPI, resultExp.ClashAPI) {
		t.Errorf("clash_api changed after AddUser:\n  original: %s\n  result:   %s", origExp.ClashAPI, resultExp.ClashAPI)
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

func TestUpdateSingboxOutboundDomainStrategies_UpdatesMatchingTagsOnly(t *testing.T) {
	fixtureJSON := `{
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
