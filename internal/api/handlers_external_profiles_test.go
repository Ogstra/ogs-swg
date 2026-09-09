package api

import (
	"encoding/base64"
	"net/url"
	"strings"
	"testing"

	"github.com/Ogstra/ogs-swg/internal/core"
)

func TestBuildExternalVlessLinkIncludesALPN(t *testing.T) {
	link, err := buildExternalVlessLink("homelab", core.ExternalProfile{
		Name:       "homelab",
		Type:       "vless",
		HostIPv4:   "example.test",
		Port:       443,
		UUID:       "11111111-1111-1111-1111-111111111111",
		PublicKey:  "public-key",
		ShortID:    "abc123",
		ServerName: "sni.example.test",
		ALPN:       "h2, http/1.1, h2",
		Flow:       "xtls-rprx-vision",
	})
	if err != nil {
		t.Fatalf("buildExternalVlessLink: %v", err)
	}

	parsed, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse link: %v", err)
	}
	if got := parsed.Query().Get("alpn"); got != "h2,http/1.1" {
		t.Fatalf("alpn=%q; want %q in link %s", got, "h2,http/1.1", link)
	}
	if !strings.Contains(link, "packetEncoding=xudp") {
		t.Fatalf("link missing packetEncoding=xudp: %s", link)
	}
}

func TestBuildExternalShadowsocksLinkRequiresServerAndUserPasswords(t *testing.T) {
	_, err := buildExternalShadowsocksLink("homelab", core.ExternalProfile{
		Name:        "homelab",
		Type:        "shadowsocks",
		HostIPv4:    "example.test",
		Port:        8388,
		Password:    "user-password",
		SSMethod:    "2022-blake3-aes-128-gcm",
		SSServerKey: "",
	})
	if err == nil || !strings.Contains(err.Error(), "missing server password") {
		t.Fatalf("error=%v; want missing server password", err)
	}
}

func TestBuildExternalShadowsocksLinkUsesServerAndUserPasswordCredential(t *testing.T) {
	link, err := buildExternalShadowsocksLink("homelab", core.ExternalProfile{
		Name:        "homelab",
		Type:        "shadowsocks",
		HostIPv4:    "example.test",
		Port:        8388,
		Password:    "user-password",
		SSMethod:    "2022-blake3-aes-128-gcm",
		SSServerKey: "server-password",
	})
	if err != nil {
		t.Fatalf("buildExternalShadowsocksLink: %v", err)
	}

	withoutFragment, _, _ := strings.Cut(link, "#")
	rest := strings.TrimPrefix(withoutFragment, "ss://")
	atIdx := strings.LastIndex(rest, "@")
	if atIdx < 0 {
		t.Fatalf("link missing @: %q", link)
	}
	decoded, err := base64.StdEncoding.DecodeString(rest[:atIdx])
	if err != nil {
		t.Fatalf("decode credential: %v", err)
	}
	want := "2022-blake3-aes-128-gcm:server-password:user-password"
	if string(decoded) != want {
		t.Fatalf("credential=%q; want %q", string(decoded), want)
	}
}
