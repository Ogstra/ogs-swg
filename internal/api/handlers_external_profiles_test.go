package api

import (
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
