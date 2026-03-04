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
