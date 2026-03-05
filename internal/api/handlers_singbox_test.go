package api

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"github.com/Ogstra/ogs-swg/internal/core"
)

func TestBuildVlessLink_IncludesALPN(t *testing.T) {
	view := &core.SingboxInboundView{
		Type: "vless",
		TLS: &core.TLSConfig{
			Enabled:    true,
			ServerName: "example.com",
			ALPN:       []string{"h2", "http/1.1"},
		},
		Raw: map[string]interface{}{
			"transport": map[string]interface{}{"type": "ws", "path": "/ws"},
		},
	}
	user := &core.UserInboundInfo{UUID: "11111111-1111-1111-1111-111111111111"}

	link, err := buildVlessLink("alice", user, view, "1.2.3.4", "443")
	if err != nil {
		t.Fatalf("buildVlessLink() error = %v", err)
	}

	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse link: %v", err)
	}
	if got := u.Query().Get("alpn"); got != "h2,http/1.1" {
		t.Fatalf("alpn = %q; want %q", got, "h2,http/1.1")
	}
}

func TestBuildTrojanLink_IncludesALPN(t *testing.T) {
	view := &core.SingboxInboundView{
		Type: "trojan",
		TLS: &core.TLSConfig{
			Enabled:    true,
			ServerName: "example.com",
			ALPN:       []string{"h2"},
		},
		Raw: map[string]interface{}{},
	}
	user := &core.UserInboundInfo{UUID: "secret-password"}

	link, err := buildTrojanLink("alice", user, view, "1.2.3.4", "443")
	if err != nil {
		t.Fatalf("buildTrojanLink() error = %v", err)
	}

	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse link: %v", err)
	}
	if got := u.Query().Get("alpn"); got != "h2" {
		t.Fatalf("alpn = %q; want %q", got, "h2")
	}
}

func TestBuildVmessLink_IncludesALPN(t *testing.T) {
	view := &core.SingboxInboundView{
		Type: "vmess",
		TLS: &core.TLSConfig{
			Enabled:    true,
			ServerName: "example.com",
			ALPN:       []string{"h2", "http/1.1"},
		},
		Raw: map[string]interface{}{
			"transport": map[string]interface{}{"type": "tcp"},
		},
	}
	user := &core.UserInboundInfo{
		UUID:          "11111111-1111-1111-1111-111111111111",
		VmessSecurity: "auto",
	}

	link, err := buildVmessLink("alice", user, view, "1.2.3.4", "443")
	if err != nil {
		t.Fatalf("buildVmessLink() error = %v", err)
	}

	raw := strings.TrimPrefix(link, "vmess://")
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("decode vmess: %v", err)
	}
	var payload map[string]string
	if err := json.Unmarshal(decoded, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got := payload["alpn"]; got != "h2,http/1.1" {
		t.Fatalf("payload alpn = %q; want %q", got, "h2,http/1.1")
	}
}

func TestBuildVmessLink_HTTPTransportMapsToH2(t *testing.T) {
	view := &core.SingboxInboundView{
		Type: "vmess",
		TLS: &core.TLSConfig{
			Enabled:    true,
			ServerName: "example.com",
		},
		Raw: map[string]interface{}{
			"transport": map[string]interface{}{"type": "http", "path": "/"},
		},
	}
	user := &core.UserInboundInfo{
		UUID:          "11111111-1111-1111-1111-111111111111",
		VmessSecurity: "auto",
	}

	link, err := buildVmessLink("alice", user, view, "1.2.3.4", "443")
	if err != nil {
		t.Fatalf("buildVmessLink() error = %v", err)
	}

	raw := strings.TrimPrefix(link, "vmess://")
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("decode vmess: %v", err)
	}
	var payload map[string]string
	if err := json.Unmarshal(decoded, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got := payload["net"]; got != "h2" {
		t.Fatalf("payload net = %q; want %q", got, "h2")
	}
}

func TestNormalizeInboundMultiplex_DisabledRemovesField(t *testing.T) {
	inbound := map[string]interface{}{
		"tag": "in-test",
		"multiplex": map[string]interface{}{
			"enabled": false,
			"padding": true,
		},
	}

	if err := normalizeInboundMultiplex(inbound); err != nil {
		t.Fatalf("normalizeInboundMultiplex() error = %v", err)
	}

	if _, ok := inbound["multiplex"]; ok {
		t.Fatalf("expected multiplex to be removed when disabled")
	}
}

func TestNormalizeInboundMultiplex_EnabledWithBrutal(t *testing.T) {
	inbound := map[string]interface{}{
		"tag": "in-test",
		"multiplex": map[string]interface{}{
			"enabled": true,
			"padding": true,
			"brutal": map[string]interface{}{
				"enabled":   true,
				"up_mbps":   "100",
				"down_mbps": float64(200),
			},
		},
	}

	if err := normalizeInboundMultiplex(inbound); err != nil {
		t.Fatalf("normalizeInboundMultiplex() error = %v", err)
	}

	multiplex, ok := inbound["multiplex"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected multiplex map, got %#v", inbound["multiplex"])
	}
	if enabled, _ := multiplex["enabled"].(bool); !enabled {
		t.Fatalf("expected multiplex.enabled=true")
	}
	if padding, _ := multiplex["padding"].(bool); !padding {
		t.Fatalf("expected multiplex.padding=true")
	}

	brutal, ok := multiplex["brutal"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected brutal map, got %#v", multiplex["brutal"])
	}
	if up, _ := brutal["up_mbps"].(int); up != 100 {
		t.Fatalf("expected brutal.up_mbps=100, got %#v", brutal["up_mbps"])
	}
	if down, _ := brutal["down_mbps"].(int); down != 200 {
		t.Fatalf("expected brutal.down_mbps=200, got %#v", brutal["down_mbps"])
	}
}

func TestNormalizeInboundMultiplex_InvalidBrutalValue(t *testing.T) {
	inbound := map[string]interface{}{
		"tag": "in-test",
		"multiplex": map[string]interface{}{
			"enabled": true,
			"brutal": map[string]interface{}{
				"enabled":   true,
				"up_mbps":   0,
				"down_mbps": 100,
			},
		},
	}

	if err := normalizeInboundMultiplex(inbound); err == nil {
		t.Fatalf("expected error for invalid brutal values")
	}
}
