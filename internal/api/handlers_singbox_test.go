package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/Ogstra/ogs-swg/internal/core"
)

type singboxConfigExecutorStub struct {
	mu   sync.Mutex
	data []byte
}

func (s *singboxConfigExecutorStub) ReadConfig(_ context.Context, _ string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.data...), nil
}

func (s *singboxConfigExecutorStub) WriteConfig(_ context.Context, _ string, content []byte, _ os.FileMode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = append([]byte(nil), content...)
	return nil
}

func (s *singboxConfigExecutorStub) ValidateSingboxConfig(context.Context, []byte) error { return nil }
func (s *singboxConfigExecutorStub) RestartService(context.Context, string) error        { return nil }
func (s *singboxConfigExecutorStub) StartService(context.Context, string) error          { return nil }
func (s *singboxConfigExecutorStub) StopService(context.Context, string) error           { return nil }
func (s *singboxConfigExecutorStub) IsServiceActive(context.Context, string) (bool, error) {
	return false, nil
}
func (s *singboxConfigExecutorStub) ApplySysctl(context.Context, string, string) error { return nil }
func (s *singboxConfigExecutorStub) GetSysctl(context.Context, string) (string, error) {
	return "", nil
}
func (s *singboxConfigExecutorStub) ReadJournal(context.Context, string, int) ([]string, error) {
	return nil, nil
}
func (s *singboxConfigExecutorStub) SearchJournal(context.Context, string, string, int) ([]string, error) {
	return nil, nil
}
func (s *singboxConfigExecutorStub) SyncWireGuard(context.Context, string, []byte) error { return nil }
func (s *singboxConfigExecutorStub) RestartWireGuard(context.Context, string) error      { return nil }
func (s *singboxConfigExecutorStub) ListWireGuardInterfaces(context.Context) ([]string, error) {
	return nil, nil
}
func (s *singboxConfigExecutorStub) EnableWireGuardInterface(context.Context, string) error {
	return nil
}
func (s *singboxConfigExecutorStub) DisableWireGuardInterface(context.Context, string) error {
	return nil
}
func (s *singboxConfigExecutorStub) GetWireGuardStats(context.Context) (map[string]core.PeerStats, error) {
	return nil, nil
}
func (s *singboxConfigExecutorStub) CheckConnectivity(context.Context) error { return nil }
func (s *singboxConfigExecutorStub) Close() error                            { return nil }
func (s *singboxConfigExecutorStub) Dial(context.Context, string, string) (net.Conn, error) {
	return nil, nil
}

func newSingboxHandlerTestServer(initialJSON string) (*Server, *singboxConfigExecutorStub) {
	stub := &singboxConfigExecutorStub{data: []byte(initialJSON)}
	cfg := &core.Config{
		EnableSingbox:     true,
		SingboxConfigPath: "/test/config.json",
		ManagedInbounds:   []string{"test-vless"},
		StatsInbounds:     []string{"test-vless"},
	}
	cfg.SetExecutor(stub)
	return NewServer(nil, cfg, stub), stub
}

func readStoredInboundByTag(t *testing.T, stub *singboxConfigExecutorStub, tag string) map[string]interface{} {
	t.Helper()

	raw, err := stub.ReadConfig(context.Background(), "/test/config.json")
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}

	var top map[string]interface{}
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("unmarshal top-level config: %v", err)
	}

	inbounds, ok := top["inbounds"].([]interface{})
	if !ok {
		t.Fatalf("inbounds missing or wrong type: %#v", top["inbounds"])
	}
	for _, rawInbound := range inbounds {
		inbound, ok := rawInbound.(map[string]interface{})
		if !ok {
			continue
		}
		if currentTag, _ := inbound["tag"].(string); currentTag == tag {
			return inbound
		}
	}

	t.Fatalf("inbound %q not found", tag)
	return nil
}

func TestBuildVlessLink_RealityOmitsALPNAndH2(t *testing.T) {
	view := &core.SingboxInboundView{
		Type: "vless",
		TLS: &core.TLSConfig{
			Enabled:    true,
			ServerName: "tls.example.com",
			ALPN:       []string{"h2", "http/1.1"},
			Reality: &core.RealityConfig{
				Enabled:   true,
				PublicKey: "public-key",
				ShortIDs:  []string{"abcd"},
				Handshake: core.RealityHandshake{Server: "hs.example.com"},
			},
		},
		Raw: map[string]interface{}{
			"transport": map[string]interface{}{"type": "tcp"},
		},
	}
	user := &core.UserInboundInfo{
		UUID: "11111111-1111-1111-1111-111111111111",
		Flow: "xtls-rprx-vision",
	}

	link, err := buildVlessLink("alice", user, view, "1.2.3.4", "443")
	if err != nil {
		t.Fatalf("buildVlessLink() error = %v", err)
	}
	if strings.Contains(link, "h2=") || strings.Contains(link, "&h2") {
		t.Fatalf("link contains stray h2 parameter: %q", link)
	}

	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse link: %v", err)
	}
	if got := u.Query().Get("alpn"); got != "" {
		t.Fatalf("alpn = %q; want empty", got)
	}
}

func TestBuildVlessLink_VisionOmitsALPN(t *testing.T) {
	view := &core.SingboxInboundView{
		Type: "vless",
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
		UUID: "11111111-1111-1111-1111-111111111111",
		Flow: "xtls-rprx-vision",
	}

	link, err := buildVlessLink("alice", user, view, "1.2.3.4", "443")
	if err != nil {
		t.Fatalf("buildVlessLink() error = %v", err)
	}

	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse link: %v", err)
	}
	if got := u.Query().Get("alpn"); got != "" {
		t.Fatalf("alpn = %q; want empty", got)
	}
}

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

func TestHandleUpdateSingboxInbound_WebSocketSubmissionOmitsALPN(t *testing.T) {
	server, stub := newSingboxHandlerTestServer(`{
		"inbounds": [
			{
				"type": "vless",
				"tag": "test-vless",
				"listen": "0.0.0.0",
				"listen_port": 443,
				"users": [
					{"name":"alice","uuid":"11111111-1111-1111-1111-111111111111","flow":"xtls-rprx-vision"}
				],
				"tls": {
					"enabled": true,
					"server_name": "example.com",
					"alpn": ["h2", "http/1.1"]
				},
				"transport": {"type": "tcp"}
			}
		]
	}`)

	payload := `{
		"type": "vless",
		"tag": "test-vless",
		"listen": "0.0.0.0",
		"listen_port": 443,
		"users": [
			{"name":"alice","uuid":"11111111-1111-1111-1111-111111111111","flow":"xtls-rprx-vision"}
		],
		"tls": {
			"enabled": true,
			"server_name": "example.com",
			"alpn": ["h2", "http/1.1"]
		},
		"transport": {"type": "ws", "path": "/ws"}
	}`

	req := httptest.NewRequest(http.MethodPut, "/api/singbox/inbounds?tag=test-vless", bytes.NewBufferString(payload))
	rec := httptest.NewRecorder()

	server.handleUpdateSingboxInbound(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}

	inbound := readStoredInboundByTag(t, stub, "test-vless")
	tls, ok := inbound["tls"].(map[string]interface{})
	if !ok {
		t.Fatalf("tls missing or wrong type: %#v", inbound["tls"])
	}
	if got := tls["alpn"]; got != nil {
		t.Fatalf("stored tls.alpn = %#v; want removed", got)
	}
}
