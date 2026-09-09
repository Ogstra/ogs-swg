package api

import (
	"encoding/json"
	"flag"
	"os"
	"testing"

	"github.com/Ogstra/ogs-swg/internal/core"
)

// updateLinkGolden rewrites internal/api/testdata/links_golden.json from the
// current (unmodified) behavior of the link builders. Run with:
//
//	go test ./internal/api -run TestLinkBuildersGolden -updategolden -count=1
var updateLinkGolden = flag.Bool("updategolden", false, "rewrite internal/api/testdata/links_golden.json")

const (
	goldenHost     = "198.51.100.10"
	goldenPort     = "443"
	goldenUserName = "alice"
	goldenUUID     = "11111111-1111-1111-1111-111111111111"
	goldenPassword = "user-password"
)

type linkGoldenCase struct {
	Name  string
	Build func() (string, error)
}

func linkGoldenCases() []linkGoldenCase {
	return []linkGoldenCase{
		{
			Name: "vless_reality_vision_tcp",
			Build: func() (string, error) {
				view := &core.SingboxInboundView{
					Type: "vless",
					TLS: &core.TLSConfig{
						Enabled:    true,
						ServerName: "tls.example.com",
						ALPN:       []string{"h2", "http/1.1"},
						Reality: &core.RealityConfig{
							Enabled:   true,
							PublicKey: "public-key-placeholder",
							ShortIDs:  []string{"abcd"},
							Handshake: core.RealityHandshake{Server: "hs.example.com"},
						},
					},
					Raw: map[string]interface{}{
						"transport": map[string]interface{}{"type": "tcp"},
					},
				}
				user := &core.UserInboundInfo{UUID: goldenUUID, Flow: "xtls-rprx-vision"}
				return buildVlessLink(goldenUserName, user, view, goldenHost, goldenPort, nil, "")
			},
		},
		{
			Name: "vless_reality_no_flow_ws",
			Build: func() (string, error) {
				view := &core.SingboxInboundView{
					Type: "vless",
					TLS: &core.TLSConfig{
						Enabled:    true,
						ServerName: "tls.example.com",
						ALPN:       []string{"h2", "http/1.1"},
						Reality: &core.RealityConfig{
							Enabled:   true,
							PublicKey: "public-key-placeholder",
							ShortIDs:  []string{"abcd"},
							Handshake: core.RealityHandshake{Server: "hs.example.com"},
						},
					},
					Raw: map[string]interface{}{
						"transport": map[string]interface{}{"type": "ws", "path": "/wspath"},
					},
				}
				user := &core.UserInboundInfo{UUID: goldenUUID, Flow: ""}
				return buildVlessLink(goldenUserName, user, view, goldenHost, goldenPort, nil, "")
			},
		},
		{
			Name: "vless_tls_ws_path_and_header_host",
			Build: func() (string, error) {
				view := &core.SingboxInboundView{
					Type: "vless",
					TLS: &core.TLSConfig{
						Enabled:    true,
						ServerName: "tls.example.com",
						ALPN:       []string{"h3"},
					},
					Raw: map[string]interface{}{
						"transport": map[string]interface{}{
							"type":    "ws",
							"path":    "/ws",
							"headers": map[string]interface{}{"Host": "cdn.example.com"},
						},
					},
				}
				user := &core.UserInboundInfo{UUID: goldenUUID}
				return buildVlessLink(goldenUserName, user, view, goldenHost, goldenPort, nil, "")
			},
		},
		{
			Name: "vless_plain_tcp_no_tls",
			Build: func() (string, error) {
				view := &core.SingboxInboundView{
					Type: "vless",
					Raw: map[string]interface{}{
						"transport": map[string]interface{}{"type": "tcp"},
					},
				}
				user := &core.UserInboundInfo{UUID: goldenUUID}
				return buildVlessLink(goldenUserName, user, view, goldenHost, goldenPort, nil, "")
			},
		},
		{
			Name: "vless_tls_grpc_service_name",
			Build: func() (string, error) {
				view := &core.SingboxInboundView{
					Type: "vless",
					TLS: &core.TLSConfig{
						Enabled:    true,
						ServerName: "tls.example.com",
					},
					Raw: map[string]interface{}{
						"transport": map[string]interface{}{"type": "grpc", "service_name": "grpcsvc"},
					},
				}
				user := &core.UserInboundInfo{UUID: goldenUUID}
				return buildVlessLink(goldenUserName, user, view, goldenHost, goldenPort, nil, "")
			},
		},
		{
			Name: "vless_tls_selfsigned_allow_insecure",
			Build: func() (string, error) {
				view := &core.SingboxInboundView{
					Type: "vless",
					TLS: &core.TLSConfig{
						Enabled:         true,
						ServerName:      "tls.example.com",
						CertificatePath: "/etc/ssl/selfsigned.pem",
					},
					Raw: map[string]interface{}{
						"transport": map[string]interface{}{"type": "tcp"},
					},
				}
				user := &core.UserInboundInfo{UUID: goldenUUID}
				return buildVlessLink(goldenUserName, user, view, goldenHost, goldenPort, nil, "")
			},
		},
		{
			Name: "vless_meta_allow_insecure_false",
			Build: func() (string, error) {
				view := &core.SingboxInboundView{
					Type: "vless",
					TLS: &core.TLSConfig{
						Enabled:         true,
						ServerName:      "tls.example.com",
						CertificatePath: "/etc/ssl/selfsigned.pem",
					},
					Raw: map[string]interface{}{
						"transport": map[string]interface{}{"type": "tcp"},
					},
				}
				user := &core.UserInboundInfo{UUID: goldenUUID}
				meta := &core.InboundMeta{Tag: "vless-in", LinkAllowInsecure: boolPtr(false)}
				return buildVlessLink(goldenUserName, user, view, goldenHost, goldenPort, meta, "")
			},
		},
		{
			Name: "vless_sni_fallback_used",
			Build: func() (string, error) {
				view := &core.SingboxInboundView{
					Type: "vless",
					TLS: &core.TLSConfig{
						Enabled: true,
					},
					Raw: map[string]interface{}{
						"transport": map[string]interface{}{"type": "tcp"},
					},
				}
				user := &core.UserInboundInfo{UUID: goldenUUID}
				return buildVlessLink(goldenUserName, user, view, goldenHost, goldenPort, nil, "panel.example.com")
			},
		},
		{
			Name: "vmess_tls_ws",
			Build: func() (string, error) {
				view := &core.SingboxInboundView{
					Type: "vmess",
					TLS: &core.TLSConfig{
						Enabled:    true,
						ServerName: "tls.example.com",
						ALPN:       []string{"h2"},
					},
					Raw: map[string]interface{}{
						"transport": map[string]interface{}{"type": "ws", "path": "/vm", "host": "cdn.example.com"},
					},
				}
				user := &core.UserInboundInfo{UUID: goldenUUID, VmessSecurity: "aes-128-gcm", VmessAlterID: 0}
				return buildVmessLink(goldenUserName, user, view, goldenHost, goldenPort, nil, "")
			},
		},
		{
			Name: "vmess_plain_http_becomes_h2",
			Build: func() (string, error) {
				view := &core.SingboxInboundView{
					Type: "vmess",
					Raw: map[string]interface{}{
						"transport": map[string]interface{}{"type": "http", "path": "/h2"},
					},
				}
				user := &core.UserInboundInfo{UUID: goldenUUID, VmessSecurity: "", VmessAlterID: 2}
				return buildVmessLink(goldenUserName, user, view, goldenHost, goldenPort, nil, "")
			},
		},
		{
			Name: "trojan_tls_tcp",
			Build: func() (string, error) {
				view := &core.SingboxInboundView{
					Type: "trojan",
					TLS: &core.TLSConfig{
						Enabled:    true,
						ServerName: "tls.example.com",
						ALPN:       []string{"h2", "http/1.1"},
					},
					Raw: map[string]interface{}{
						"transport": map[string]interface{}{"type": "tcp"},
					},
				}
				user := &core.UserInboundInfo{UUID: "trojan-password"}
				return buildTrojanLink(goldenUserName, user, view, goldenHost, goldenPort, nil, "")
			},
		},
		{
			Name: "trojan_tls_ws_header_host",
			Build: func() (string, error) {
				view := &core.SingboxInboundView{
					Type: "trojan",
					TLS: &core.TLSConfig{
						Enabled:    true,
						ServerName: "tls.example.com",
						ALPN:       []string{"h2", "http/1.1"},
					},
					Raw: map[string]interface{}{
						"transport": map[string]interface{}{
							"type":    "ws",
							"path":    "/tj",
							"headers": map[string]interface{}{"Host": "cdn.example.com"},
						},
					},
				}
				user := &core.UserInboundInfo{UUID: "trojan-password"}
				return buildTrojanLink(goldenUserName, user, view, goldenHost, goldenPort, nil, "")
			},
		},
		{
			Name: "hysteria2_tls_with_obfs",
			Build: func() (string, error) {
				view := &core.SingboxInboundView{
					Type: "hysteria2",
					TLS: &core.TLSConfig{
						Enabled:    true,
						ServerName: "tls.example.com",
					},
					Obfs: &core.SingboxObfsConfig{Type: "salamander", Password: "obfs-password"},
				}
				user := &core.UserInboundInfo{Password: goldenPassword}
				return buildHysteria2Link(goldenUserName, user, view, goldenHost, goldenPort)
			},
		},
		{
			Name: "hysteria2_tls_no_obfs",
			Build: func() (string, error) {
				view := &core.SingboxInboundView{
					Type: "hysteria2",
					TLS: &core.TLSConfig{
						Enabled:    true,
						ServerName: "tls.example.com",
					},
					Raw: map[string]interface{}{},
				}
				user := &core.UserInboundInfo{Password: goldenPassword}
				return buildHysteria2Link(goldenUserName, user, view, goldenHost, goldenPort)
			},
		},
		{
			Name: "hysteria2_obfs_missing_password",
			Build: func() (string, error) {
				view := &core.SingboxInboundView{
					Type: "hysteria2",
					TLS: &core.TLSConfig{
						Enabled:    true,
						ServerName: "tls.example.com",
					},
					Obfs: &core.SingboxObfsConfig{Type: "salamander", Password: ""},
				}
				user := &core.UserInboundInfo{Password: goldenPassword}
				return buildHysteria2Link(goldenUserName, user, view, goldenHost, goldenPort)
			},
		},
		{
			Name: "shadowsocks_2022_with_server_key",
			Build: func() (string, error) {
				view := &core.SingboxInboundView{
					Type:      "shadowsocks",
					Method:    "2022-blake3-aes-128-gcm",
					ServerKey: "server-key-placeholder",
				}
				user := &core.UserInboundInfo{Password: "user-key-placeholder"}
				return buildShadowsocksLink(goldenUserName, user, view, goldenHost, goldenPort)
			},
		},
		{
			Name: "shadowsocks_legacy_no_server_key",
			Build: func() (string, error) {
				view := &core.SingboxInboundView{
					Type:   "shadowsocks",
					Method: "aes-256-gcm",
				}
				user := &core.UserInboundInfo{Password: goldenPassword}
				return buildShadowsocksLink(goldenUserName, user, view, goldenHost, goldenPort)
			},
		},
		{
			Name: "anytls_tls_alpn_insecure",
			Build: func() (string, error) {
				view := &core.SingboxInboundView{
					Type: "anytls",
					TLS: &core.TLSConfig{
						Enabled:         true,
						ServerName:      "tls.example.com",
						ALPN:            []string{"h2", "h2"},
						CertificatePath: "/etc/ssl/self-signed.pem",
					},
				}
				user := &core.UserInboundInfo{Password: goldenPassword}
				return buildAnyTLSLink(goldenUserName, user, view, goldenHost, goldenPort, nil, "")
			},
		},
		{
			Name: "naive_https_default",
			Build: func() (string, error) {
				view := &core.SingboxInboundView{
					Type: "naive",
					TLS: &core.TLSConfig{
						Enabled:    true,
						ServerName: "tls.example.com",
					},
					Raw: map[string]interface{}{},
				}
				user := &core.UserInboundInfo{Password: goldenPassword}
				return buildNaiveLink(goldenUserName, user, view, goldenHost, goldenPort, nil, "")
			},
		},
		{
			Name: "naive_quic_network_udp",
			Build: func() (string, error) {
				view := &core.SingboxInboundView{
					Type: "naive",
					TLS: &core.TLSConfig{
						Enabled:    true,
						ServerName: goldenHost,
					},
					Network: "UDP",
				}
				user := &core.UserInboundInfo{Password: goldenPassword}
				return buildNaiveLink(goldenUserName, user, view, goldenHost, goldenPort, nil, "")
			},
		},
		{
			Name: "external_vless_ipv4",
			Build: func() (string, error) {
				p := core.ExternalProfile{
					Name:       "homelab",
					Type:       "vless",
					HostIPv4:   "198.51.100.20",
					Port:       8443,
					UUID:       "22222222-2222-2222-2222-222222222222",
					PublicKey:  "public-key-placeholder",
					ShortID:    "beef",
					ServerName: "sni.example.com",
					ALPN:       "h2,http/1.1",
					Flow:       "xtls-rprx-vision",
				}
				return buildExternalVlessLink("homelab EU", p)
			},
		},
		{
			Name: "external_vless_ipv6_literal",
			Build: func() (string, error) {
				p := core.ExternalProfile{
					Name:         "homelab",
					Type:         "vless",
					HostIPv6File: "2001:db8::10",
					HostIPv4:     "",
					Port:         8443,
					UUID:         "22222222-2222-2222-2222-222222222222",
					PublicKey:    "public-key-placeholder",
					ShortID:      "beef",
					ServerName:   "sni.example.com",
					ALPN:         "h2,http/1.1",
					Flow:         "xtls-rprx-vision",
				}
				return buildExternalVlessLink("homelab EU", p)
			},
		},
		{
			Name: "external_shadowsocks_ipv4",
			Build: func() (string, error) {
				p := core.ExternalProfile{
					Name:        "homelab",
					Type:        "shadowsocks",
					HostIPv4:    "198.51.100.20",
					Port:        8388,
					Password:    "user-key-placeholder",
					SSMethod:    "2022-blake3-aes-128-gcm",
					SSServerKey: "server-key-placeholder",
				}
				return buildExternalShadowsocksLink("homelab SS", p)
			},
		},
	}
}

func TestLinkBuildersGolden(t *testing.T) {
	cases := linkGoldenCases()
	got := make(map[string]string, len(cases))
	for _, c := range cases {
		link, err := c.Build()
		if err != nil {
			got[c.Name] = "ERROR: " + err.Error()
			continue
		}
		got[c.Name] = link
	}

	data, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatalf("marshal golden: %v", err)
	}
	data = append(data, '\n')

	const goldenPath = "testdata/links_golden.json"

	if *updateLinkGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(goldenPath, data, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("wrote %s with %d entries", goldenPath, len(got))
		return
	}

	raw, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with -updategolden first): %v", err)
	}
	want := make(map[string]string)
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("unmarshal golden: %v", err)
	}

	for name, wantLink := range want {
		gotLink, ok := got[name]
		if !ok {
			t.Errorf("golden has case %q not present in table", name)
			continue
		}
		if gotLink != wantLink {
			t.Errorf("golden mismatch for %s:\n got: %q\nwant: %q", name, gotLink, wantLink)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("table has case %q not present in golden", name)
		}
	}
}
