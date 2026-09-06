package core

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"testing"
)

// updateUserInboundsGolden rewrites internal/core/testdata/user_inbounds_golden.json
// from the current (unmodified) behavior of GetUserInbounds. Run with:
//
//	go test ./internal/core -run TestGetUserInboundsGolden -updateuserinbounds -count=1
var updateUserInboundsGolden = flag.Bool("updateuserinbounds", false, "rewrite internal/core/testdata/user_inbounds_golden.json")

// userInboundsFixtureJSON contains one inbound per known protocol type (plus
// one unknown type and one inbound belonging to a different user) so
// GetUserInbounds's full credential-mapping switch is exercised.
const userInboundsFixtureJSON = `{
  "inbounds": [
    {"type":"vless","tag":"gold-vless","listen_port":10001,"users":[{"name":"alice","uuid":"11111111-1111-1111-1111-111111111111","flow":"xtls-rprx-vision"}]},
    {"type":"vmess","tag":"gold-vmess","listen_port":10002,"users":[{"name":"alice","id":"22222222-2222-2222-2222-222222222222","security":"aes-128-gcm","alterId":2,"flow":"ignored-flow"}]},
    {"type":"vmess","tag":"gold-vmess-uuid","listen_port":10003,"users":[{"name":"alice","uuid":"33333333-3333-3333-3333-333333333333","id":"44444444-4444-4444-4444-444444444444"}]},
    {"type":"trojan","tag":"gold-trojan","listen_port":10004,"users":[{"name":"alice","password":"trojan-password","flow":"should-be-cleared","security":"aes-128-gcm","alterId":3}]},
    {"type":"hysteria2","tag":"gold-hy2","listen_port":10005,"users":[{"name":"alice","password":"hy2-password"}]},
    {"type":"shadowsocks","tag":"gold-ss","listen_port":10006,"method":"2022-blake3-aes-128-gcm","password":"server-key-placeholder","users":[{"name":"alice","password":"user-key-placeholder"}]},
    {"type":"anytls","tag":"gold-anytls","listen_port":10007,"users":[{"name":"alice","password":"anytls-password"}]},
    {"type":"naive","tag":"gold-naive","listen_port":10008,"network":"udp","users":[{"username":"alice","password":"naive-password"}]},
    {"type":"socks","tag":"gold-unknown","listen_port":10009,"users":[{"username":"alice","password":"socks-password","uuid":"55555555-5555-5555-5555-555555555555"}]},
    {"type":"vless","tag":"gold-other-user","listen_port":10010,"users":[{"name":"bob","uuid":"66666666-6666-6666-6666-666666666666"}]}
  ]
}`

func TestGetUserInboundsGolden(t *testing.T) {
	cfg, _ := newTestConfig(t, userInboundsFixtureJSON)

	result, err := cfg.GetUserInbounds("alice")
	if err != nil {
		t.Fatalf("GetUserInbounds() error = %v", err)
	}

	got, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatalf("marshal golden: %v", err)
	}
	got = append(got, '\n')

	const goldenPath = "testdata/user_inbounds_golden.json"

	if *updateUserInboundsGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("wrote %s with %d entries", goldenPath, len(result))
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with -updateuserinbounds first): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("golden mismatch:\n got: %s\nwant: %s", got, want)
	}
}
