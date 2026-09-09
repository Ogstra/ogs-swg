package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// updateConfigReadGolden rewrites internal/core/testdata/singbox_config_read_golden.json
// from the current (unmodified) behavior of the sing-box config read accessors. Run with:
//
//	go test ./internal/core -run TestSingboxConfigReadGolden -updateconfiggolden -count=1
var updateConfigReadGolden = flag.Bool("updateconfiggolden", false, "rewrite internal/core/testdata/singbox_config_read_golden.json")

// fileBackedExecutor is a SystemExecutor whose ReadConfig/WriteConfig hit a REAL
// file on disk, so the golden exercises the same os.Stat-able path production uses.
type fileBackedExecutor struct{ path string }

func (e *fileBackedExecutor) ReadConfig(_ context.Context, path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (e *fileBackedExecutor) WriteConfig(_ context.Context, path string, content []byte, mode os.FileMode) error {
	return os.WriteFile(path, content, mode)
}

func (e *fileBackedExecutor) ValidateSingboxConfig(_ context.Context, _ []byte) error { return nil }
func (e *fileBackedExecutor) RestartService(_ context.Context, _ string) error        { return nil }
func (e *fileBackedExecutor) StartService(_ context.Context, _ string) error          { return nil }
func (e *fileBackedExecutor) StopService(_ context.Context, _ string) error           { return nil }
func (e *fileBackedExecutor) IsServiceActive(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (e *fileBackedExecutor) ApplySysctl(_ context.Context, _, _ string) error      { return nil }
func (e *fileBackedExecutor) GetSysctl(_ context.Context, _ string) (string, error) { return "", nil }
func (e *fileBackedExecutor) SyncWireGuard(_ context.Context, _ string, _ []byte) error {
	return nil
}
func (e *fileBackedExecutor) RestartWireGuard(_ context.Context, _ string) error { return nil }
func (e *fileBackedExecutor) ListWireGuardInterfaces(_ context.Context) ([]string, error) {
	return nil, nil
}
func (e *fileBackedExecutor) EnableWireGuardInterface(_ context.Context, _ string) error {
	return nil
}
func (e *fileBackedExecutor) DisableWireGuardInterface(_ context.Context, _ string) error {
	return nil
}
func (e *fileBackedExecutor) GetWireGuardStats(_ context.Context) (map[string]PeerStats, error) {
	return nil, nil
}
func (e *fileBackedExecutor) CheckConnectivity(_ context.Context) error             { return nil }
func (e *fileBackedExecutor) Close() error                                          { return nil }
func (e *fileBackedExecutor) Dial(_ context.Context, _, _ string) (net.Conn, error) { return nil, nil }

// newFileBackedTestConfig writes initialJSON to a real temp file and returns a
// *Config wired to it through fileBackedExecutor.
func newFileBackedTestConfig(t *testing.T, initialJSON string) (*Config, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "singbox-config.json")
	if err := os.WriteFile(path, []byte(initialJSON), 0o644); err != nil {
		t.Fatalf("write fixture config: %v", err)
	}

	cfg := &Config{
		SingboxConfigPath: path,
		ManagedInbounds:   []string{"gold-vless", "gold-vmess", "gold-trojan", "gold-hy2", "gold-ss", "gold-anytls", "gold-naive"},
		StatsInbounds:     []string{"gold-vless"},
		StatsOutbounds:    []string{"direct"},
		SingboxAPIAddr:    "127.0.0.1:19001",
		EnableSingbox:     false,
	}
	cfg.SetExecutor(&fileBackedExecutor{path: path})
	return cfg, path
}

// configReadFixtureJSON is a representative sing-box config using only synthetic
// placeholders, covering one inbound per managed type plus an unmanaged inbound
// and a second vless inbound whose only user is bob.
const configReadFixtureJSON = `{
  "log": {"level": "info", "timestamp": true},
  "dns": {
    "servers": [
      {"tag": "dns-remote", "address": "198.51.100.53"}
    ]
  },
  "inbounds": [
    {
      "type": "vless",
      "tag": "gold-vless",
      "listen": "::",
      "listen_port": 443,
      "users": [
        {"name": "alice", "uuid": "11111111-1111-1111-1111-111111111111", "flow": "xtls-rprx-vision"}
      ],
      "tls": {
        "enabled": true,
        "server_name": "edge.example.com",
        "reality": {
          "enabled": true,
          "handshake": {"server": "edge.example.com", "server_port": 443},
          "private_key": "reality-private-key-placeholder",
          "public_key": "reality-public-key-placeholder",
          "short_id": ["deadbeef"]
        }
      },
      "network": "tcp"
    },
    {
      "type": "vless",
      "tag": "gold-vless-bob",
      "listen": "::",
      "listen_port": 444,
      "users": [
        {"name": "bob", "uuid": "77777777-7777-7777-7777-777777777777", "flow": "xtls-rprx-vision"}
      ]
    },
    {
      "type": "vmess",
      "tag": "gold-vmess",
      "listen": "::",
      "listen_port": 445,
      "users": [
        {"name": "alice", "id": "22222222-2222-2222-2222-222222222222", "security": "aes-128-gcm", "alterId": 0}
      ],
      "tls": {
        "enabled": true,
        "server_name": "edge.example.com"
      },
      "transport": {"type": "ws", "path": "/gold-vmess-ws"}
    },
    {
      "type": "trojan",
      "tag": "gold-trojan",
      "listen": "::",
      "listen_port": 446,
      "users": [
        {"name": "alice", "password": "trojan-password-placeholder"}
      ],
      "tls": {
        "enabled": true,
        "server_name": "edge.example.com"
      }
    },
    {
      "type": "hysteria2",
      "tag": "gold-hy2",
      "listen": "::",
      "listen_port": 447,
      "users": [
        {"name": "alice", "password": "hy2-password-placeholder"}
      ],
      "tls": {
        "enabled": true,
        "server_name": "edge.example.com"
      },
      "obfs": {
        "type": "salamander",
        "password": "obfs-password-placeholder"
      }
    },
    {
      "type": "shadowsocks",
      "tag": "gold-ss",
      "listen": "::",
      "listen_port": 448,
      "method": "2022-blake3-aes-128-gcm",
      "password": "ss-server-key-placeholder",
      "users": [
        {"name": "alice", "password": "ss-user-key-placeholder"}
      ]
    },
    {
      "type": "anytls",
      "tag": "gold-anytls",
      "listen": "::",
      "listen_port": 449,
      "users": [
        {"name": "alice", "password": "anytls-password-placeholder"}
      ],
      "tls": {
        "enabled": true,
        "server_name": "edge.example.com",
        "alpn": ["h2", "http/1.1"]
      }
    },
    {
      "type": "naive",
      "tag": "gold-naive",
      "listen": "::",
      "listen_port": 450,
      "network": "udp",
      "users": [
        {"username": "alice", "password": "naive-password-placeholder"}
      ],
      "tls": {
        "enabled": true,
        "server_name": "edge.example.com"
      }
    },
    {
      "type": "socks",
      "tag": "gold-unknown",
      "listen": "::",
      "listen_port": 451
    }
  ],
  "outbounds": [
    {"type": "direct", "tag": "direct", "domain_strategy": "prefer_ipv4"},
    {"type": "block", "tag": "block"}
  ],
  "route": {
    "rules": [
      {"inbound": ["gold-vless"], "auth_user": ["alice"], "outbound": "direct"},
      {"protocol": "dns", "action": "hijack-dns"}
    ]
  },
  "experimental": {
    "clash_api": {
      "external_controller": "127.0.0.1:9090",
      "secret": "clash-secret-placeholder"
    },
    "v2ray_api": {
      "listen": "127.0.0.1:8964",
      "stats": {
        "enabled": true,
        "inbounds": ["gold-vless"],
        "outbounds": ["direct"],
        "users": ["alice"]
      }
    }
  }
}`

func fmtInboundView(view SingboxInboundView) map[string]interface{} {
	return map[string]interface{}{
		"tag":         view.Tag,
		"type":        view.Type,
		"listen_port": view.ListenPort,
		"users":       view.Users,
		"tls":         view.TLS,
		"obfs":        view.Obfs,
		"network":     view.Network,
		"method":      view.Method,
		"server_key":  view.ServerKey,
		"raw":         view.Raw,
	}
}

func withErrString(v interface{}, err error) interface{} {
	if err != nil {
		return "ERROR: " + err.Error()
	}
	return v
}

// captureConfigReadSnapshot exercises every sing-box config read accessor and
// returns a stable, JSON-marshalable snapshot of their outputs.
func captureConfigReadSnapshot(t *testing.T, cfg *Config) map[string]interface{} {
	t.Helper()
	snapshot := map[string]interface{}{}

	rawStr, err := cfg.GetSingboxConfig()
	if err != nil {
		snapshot["raw_bytes_sha256"] = "ERROR: " + err.Error()
	} else {
		sum := sha256.Sum256([]byte(rawStr))
		snapshot["raw_bytes_sha256"] = hex.EncodeToString(sum[:])
	}

	configMap, err := cfg.GetSingboxConfigMap()
	snapshot["config_map"] = withErrString(configMap, err)

	dns, err := cfg.GetSingboxDNS()
	snapshot["dns"] = withErrString(dns, err)

	views, err := cfg.GetSingboxInboundViews()
	if err != nil {
		snapshot["inbound_views"] = "ERROR: " + err.Error()
	} else {
		formatted := make([]map[string]interface{}, 0, len(views))
		for _, v := range views {
			formatted = append(formatted, fmtInboundView(v))
		}
		snapshot["inbound_views"] = formatted
	}

	hy2View, err := cfg.GetSingboxInboundView("gold-hy2")
	if err != nil {
		snapshot["inbound_view_gold_hy2"] = "ERROR: " + err.Error()
	} else {
		snapshot["inbound_view_gold_hy2"] = fmtInboundView(*hy2View)
	}

	_, missingErr := cfg.GetSingboxInboundView("does-not-exist")
	if missingErr != nil {
		snapshot["inbound_view_missing_error"] = missingErr.Error()
	} else {
		snapshot["inbound_view_missing_error"] = ""
	}

	inboundsRaw, err := cfg.GetSingboxInbounds()
	snapshot["inbounds_raw"] = withErrString(inboundsRaw, err)

	inboundMetas, err := cfg.GetSingboxInboundMetas()
	snapshot["inbound_metas"] = withErrString(inboundMetas, err)

	outboundViews, err := cfg.GetSingboxOutboundViews()
	snapshot["outbound_views"] = withErrString(outboundViews, err)

	routeRules, err := cfg.GetSingboxRouteRules()
	snapshot["route_rules"] = withErrString(routeRules, err)

	userInboundsAlice, err := cfg.GetUserInbounds("alice")
	snapshot["user_inbounds_alice"] = withErrString(userInboundsAlice, err)

	userInboundsBob, err := cfg.GetUserInbounds("bob")
	snapshot["user_inbounds_bob"] = withErrString(userInboundsBob, err)

	snapshot["clash_api"] = func() interface{} {
		cfg.mu.Lock()
		defer cfg.mu.Unlock()
		v, err := cfg.getSingboxClashAPILocked()
		if err != nil {
			return "ERROR: " + err.Error()
		}
		return v
	}()

	return snapshot
}

func TestSingboxConfigReadGolden(t *testing.T) {
	cfg, path := newFileBackedTestConfig(t, configReadFixtureJSON)

	first := captureConfigReadSnapshot(t, cfg)
	second := captureConfigReadSnapshot(t, cfg)

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("repeat reads of an unchanged file must be identical")
	}

	// Modify the on-disk file to prove reads reflect the current file state.
	modified := strings.Replace(configReadFixtureJSON, `"listen_port": 443,`, `"listen_port": 8443,`, 1)
	modified = strings.Replace(
		modified,
		`"users": [
        {"name": "alice", "id": "22222222-2222-2222-2222-222222222222", "security": "aes-128-gcm", "alterId": 0}
      ],
      "tls": {
        "enabled": true,
        "server_name": "edge.example.com"
      },
      "transport": {"type": "ws", "path": "/gold-vmess-ws"}`,
		`"users": [
        {"name": "alice", "id": "22222222-2222-2222-2222-222222222222", "security": "aes-128-gcm", "alterId": 0},
        {"name": "carol", "id": "88888888-8888-8888-8888-888888888888", "security": "aes-128-gcm", "alterId": 0}
      ],
      "tls": {
        "enabled": true,
        "server_name": "edge.example.com"
      },
      "transport": {"type": "ws", "path": "/gold-vmess-ws"}`,
		1,
	)
	if modified == configReadFixtureJSON {
		t.Fatalf("fixture edit did not apply — check replace targets")
	}

	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(path, []byte(modified), 0o644); err != nil {
		t.Fatalf("write modified fixture: %v", err)
	}

	third := captureConfigReadSnapshot(t, cfg)
	if reflect.DeepEqual(first, third) {
		t.Fatalf("post-edit snapshot must differ from initial snapshot")
	}

	golden := map[string]interface{}{
		"initial":             first,
		"repeat_same_file":    second,
		"after_external_edit": third,
	}

	got, err := json.MarshalIndent(golden, "", "  ")
	if err != nil {
		t.Fatalf("marshal golden: %v", err)
	}
	got = append(got, '\n')

	const goldenPath = "testdata/singbox_config_read_golden.json"

	if *updateConfigReadGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("wrote %s", goldenPath)
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with -updateconfiggolden first): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("golden mismatch:\n%s", diffFirstLines(got, want, 40))
	}
}

// diffFirstLines returns a human-readable dump of the first n differing lines
// between got and want, for golden-mismatch failure messages.
func diffFirstLines(got, want []byte, n int) string {
	gotLines := strings.Split(string(got), "\n")
	wantLines := strings.Split(string(want), "\n")
	var b strings.Builder
	shown := 0
	max := len(gotLines)
	if len(wantLines) > max {
		max = len(wantLines)
	}
	for i := 0; i < max && shown < n; i++ {
		var g, w string
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if g != w {
			fmt.Fprintf(&b, "line %d:\n  got:  %s\n  want: %s\n", i+1, g, w)
			shown++
		}
	}
	return b.String()
}
