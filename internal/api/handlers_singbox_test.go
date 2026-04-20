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
	"path/filepath"
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
func (s *singboxConfigExecutorStub) ReadAllJournal(context.Context, string) ([]string, error) {
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

func TestHandleApplySingboxChanges_ReturnsRestartRequired(t *testing.T) {
	server, _ := newSingboxHandlerTestServer(`{}`)
	server.config.MarkSingboxPending()

	req := httptest.NewRequest(http.MethodPost, "/api/singbox/apply", nil)
	rec := httptest.NewRecorder()

	server.handleApplySingboxChanges(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}

	var body struct {
		Success         bool   `json:"success"`
		RestartRequired bool   `json:"restart_required"`
		Message         string `json:"message"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Success {
		t.Fatalf("success=true, want false")
	}
	if !body.RestartRequired {
		t.Fatalf("restart_required=false, want true")
	}
	if !server.config.GetSingboxPendingChanges() {
		t.Fatalf("pending changes cleared before confirmed restart")
	}
}

func newSingboxHandlerTestServerWithStore(t *testing.T, initialJSON string) (*Server, *singboxConfigExecutorStub, *core.Store) {
	return newSingboxHandlerTestServerWithStoreAndManagedInbounds(t, initialJSON, []string{"test-vless"})
}

func newSingboxHandlerTestServerWithStoreAndManagedInbounds(t *testing.T, initialJSON string, managedInbounds []string) (*Server, *singboxConfigExecutorStub, *core.Store) {
	t.Helper()

	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.db")
	store, err := core.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	stub := &singboxConfigExecutorStub{data: []byte(initialJSON)}
	cfg := &core.Config{
		EnableSingbox:     true,
		SingboxConfigPath: "/test/config.json",
		ManagedInbounds:   managedInbounds,
		StatsInbounds:     managedInbounds,
	}
	cfg.SetExecutor(stub)
	return NewServer(store, cfg, stub), stub, store
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

func readStoredConfigMap(t *testing.T, stub *singboxConfigExecutorStub) map[string]interface{} {
	t.Helper()

	raw, err := stub.ReadConfig(context.Background(), "/test/config.json")
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}

	var top map[string]interface{}
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatalf("unmarshal top-level config: %v", err)
	}
	return top
}

func readStoredStatsInbounds(t *testing.T, stub *singboxConfigExecutorStub) []string {
	t.Helper()

	top := readStoredConfigMap(t, stub)
	experimental, ok := top["experimental"].(map[string]interface{})
	if !ok {
		t.Fatalf("experimental missing or wrong type: %#v", top["experimental"])
	}
	v2rayAPI, ok := experimental["v2ray_api"].(map[string]interface{})
	if !ok {
		t.Fatalf("v2ray_api missing or wrong type: %#v", experimental["v2ray_api"])
	}
	stats, ok := v2rayAPI["stats"].(map[string]interface{})
	if !ok {
		t.Fatalf("stats missing or wrong type: %#v", v2rayAPI["stats"])
	}
	rawInbounds, ok := stats["inbounds"].([]interface{})
	if !ok {
		t.Fatalf("stats.inbounds missing or wrong type: %#v", stats["inbounds"])
	}

	out := make([]string, 0, len(rawInbounds))
	for _, rawInbound := range rawInbounds {
		if tag, ok := rawInbound.(string); ok {
			out = append(out, tag)
		}
	}
	return out
}

func TestBuildVlessLink_RealityVisionIncludesALPNAndXUDP(t *testing.T) {
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

	link, err := buildVlessLink("alice", user, view, "1.2.3.4", "443", nil, "")
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
	if got := u.Query().Get("alpn"); got != "h2,http/1.1" {
		t.Fatalf("alpn = %q; want %q", got, "h2,http/1.1")
	}
	if got := u.Query().Get("packetEncoding"); got != "xudp" {
		t.Fatalf("packetEncoding = %q; want %q", got, "xudp")
	}
	if got := u.Query().Get("udp"); got != "" {
		t.Fatalf("udp = %q; want empty", got)
	}
}

func TestHandleCreateUser_RejectsMultipleInboundAssignment(t *testing.T) {
	server, stub, _ := newSingboxHandlerTestServerWithStoreAndManagedInbounds(t, `{
		"inbounds": [
			{
				"type":"vless",
				"tag":"test-vless",
				"listen":"0.0.0.0",
				"listen_port":443,
				"users":[]
			},
			{
				"type":"vmess",
				"tag":"test-vmess",
				"listen":"0.0.0.0",
				"listen_port":8443,
				"users":[]
			}
		],
		"experimental":{
			"v2ray_api":{"listen":"127.0.0.1:19001","stats":{"enabled":true,"inbounds":["test-vless","test-vmess"],"outbounds":["direct"],"users":[]}}
		}
	}`, []string{"test-vless", "test-vmess"})

	makeReq := func(tag string) *httptest.ResponseRecorder {
		body, err := json.Marshal(CreateUserRequest{
			Name:       "alice",
			UUID:       "11111111-1111-1111-1111-111111111111",
			QuotaLimit: 0,
			ResetDay:   1,
			Enabled:    boolPtr(true),
			InboundTag: tag,
		})
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/users", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		server.handleCreateUser(rec, req)
		return rec
	}

	first := makeReq("test-vless")
	if first.Code != http.StatusCreated {
		t.Fatalf("first create status=%d body=%q", first.Code, first.Body.String())
	}

	second := makeReq("test-vmess")
	if second.Code != http.StatusBadRequest {
		t.Fatalf("second create status=%d body=%q", second.Code, second.Body.String())
	}
	if !strings.Contains(second.Body.String(), "multiple inbounds per user are deprecated") {
		t.Fatalf("second create body=%q", second.Body.String())
	}

	namesVless := inboundUserNames(t, stub, "test-vless")
	if len(namesVless) != 1 || namesVless[0] != "alice" {
		t.Fatalf("test-vless users=%v want [alice]", namesVless)
	}
	namesVmess := inboundUserNames(t, stub, "test-vmess")
	if len(namesVmess) != 0 {
		t.Fatalf("test-vmess users=%v want []", namesVmess)
	}
}

func TestBuildVlessLink_VisionIncludesALPN(t *testing.T) {
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

	link, err := buildVlessLink("alice", user, view, "1.2.3.4", "443", nil, "")
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
	if got := u.Query().Get("packetEncoding"); got != "xudp" {
		t.Fatalf("packetEncoding = %q; want %q", got, "xudp")
	}
	if got := u.Query().Get("udp"); got != "" {
		t.Fatalf("udp = %q; want empty", got)
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

	link, err := buildVlessLink("alice", user, view, "1.2.3.4", "443", nil, "")
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
	if got := u.Query().Get("packetEncoding"); got != "" {
		t.Fatalf("packetEncoding = %q; want empty for non-direct transport", got)
	}
}

func TestBuildVlessLink_DirectTCPIncludesXUDPWithoutFlow(t *testing.T) {
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
	user := &core.UserInboundInfo{UUID: "11111111-1111-1111-1111-111111111111"}

	link, err := buildVlessLink("alice", user, view, "1.2.3.4", "443", nil, "")
	if err != nil {
		t.Fatalf("buildVlessLink() error = %v", err)
	}

	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse link: %v", err)
	}
	if got := u.Query().Get("packetEncoding"); got != "xudp" {
		t.Fatalf("packetEncoding = %q; want %q", got, "xudp")
	}
	if got := u.Query().Get("alpn"); got != "h2,http/1.1" {
		t.Fatalf("alpn = %q; want %q", got, "h2,http/1.1")
	}
}

func TestBuildVlessLink_RealityDirectTCPIncludesXUDPWithoutFlow(t *testing.T) {
	view := &core.SingboxInboundView{
		Type: "vless",
		TLS: &core.TLSConfig{
			Enabled:    true,
			ServerName: "tls.example.com",
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
	user := &core.UserInboundInfo{UUID: "11111111-1111-1111-1111-111111111111"}

	link, err := buildVlessLink("alice", user, view, "1.2.3.4", "443", nil, "")
	if err != nil {
		t.Fatalf("buildVlessLink() error = %v", err)
	}

	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse link: %v", err)
	}
	if got := u.Query().Get("packetEncoding"); got != "xudp" {
		t.Fatalf("packetEncoding = %q; want %q", got, "xudp")
	}
}

func TestBuildVlessLink_NonDirectTransportOmitsXUDP(t *testing.T) {
	testCases := []struct {
		name      string
		transport map[string]interface{}
	}{
		{
			name:      "ws",
			transport: map[string]interface{}{"type": "ws", "path": "/ws"},
		},
		{
			name:      "grpc",
			transport: map[string]interface{}{"type": "grpc", "service_name": "svc"},
		},
		{
			name:      "httpupgrade",
			transport: map[string]interface{}{"type": "httpupgrade", "path": "/up"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			view := &core.SingboxInboundView{
				Type: "vless",
				TLS: &core.TLSConfig{
					Enabled:    true,
					ServerName: "example.com",
				},
				Raw: map[string]interface{}{
					"transport": tc.transport,
				},
			}
			user := &core.UserInboundInfo{UUID: "11111111-1111-1111-1111-111111111111"}

			link, err := buildVlessLink("alice", user, view, "1.2.3.4", "443", nil, "")
			if err != nil {
				t.Fatalf("buildVlessLink() error = %v", err)
			}

			u, err := url.Parse(link)
			if err != nil {
				t.Fatalf("parse link: %v", err)
			}
			if got := u.Query().Get("packetEncoding"); got != "" {
				t.Fatalf("packetEncoding = %q; want empty", got)
			}
			if got := u.Query().Get("udp"); got != "" {
				t.Fatalf("udp = %q; want empty", got)
			}
		})
	}
}

func TestBuildVlessLink_RecommendedTransportVariants(t *testing.T) {
	testCases := []struct {
		name            string
		transport       map[string]interface{}
		wantType        string
		wantPath        string
		wantServiceName string
	}{
		{
			name:            "grpc tls",
			transport:       map[string]interface{}{"type": "grpc", "service_name": "svc-vless"},
			wantType:        "grpc",
			wantServiceName: "svc-vless",
		},
		{
			name:      "httpupgrade tls",
			transport: map[string]interface{}{"type": "httpupgrade", "path": "/upgrade"},
			wantType:  "httpupgrade",
			wantPath:  "/upgrade",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			view := &core.SingboxInboundView{
				Type: "vless",
				TLS: &core.TLSConfig{
					Enabled:    true,
					ServerName: "example.com",
					ALPN:       []string{"h2", "http/1.1"},
				},
				Raw: map[string]interface{}{
					"transport": tc.transport,
				},
			}
			user := &core.UserInboundInfo{UUID: "11111111-1111-1111-1111-111111111111"}

			link, err := buildVlessLink("alice", user, view, "1.2.3.4", "443", nil, "")
			if err != nil {
				t.Fatalf("buildVlessLink() error = %v", err)
			}
			u, err := url.Parse(link)
			if err != nil {
				t.Fatalf("parse link: %v", err)
			}
			if got := u.Query().Get("security"); got != "tls" {
				t.Fatalf("security = %q; want tls", got)
			}
			if got := u.Query().Get("type"); got != tc.wantType {
				t.Fatalf("type = %q; want %q", got, tc.wantType)
			}
			if got := u.Query().Get("path"); got != tc.wantPath {
				t.Fatalf("path = %q; want %q", got, tc.wantPath)
			}
			if got := u.Query().Get("serviceName"); got != tc.wantServiceName {
				t.Fatalf("serviceName = %q; want %q", got, tc.wantServiceName)
			}
			if got := u.Query().Get("packetEncoding"); got != "" {
				t.Fatalf("packetEncoding = %q; want empty", got)
			}
		})
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

	link, err := buildTrojanLink("alice", user, view, "1.2.3.4", "443", nil, "")
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

func TestBuildTrojanLink_RecommendedTransportVariants(t *testing.T) {
	testCases := []struct {
		name            string
		transport       map[string]interface{}
		wantType        string
		wantPath        string
		wantServiceName string
	}{
		{
			name:            "grpc tls",
			transport:       map[string]interface{}{"type": "grpc", "service_name": "svc-trojan"},
			wantType:        "grpc",
			wantServiceName: "svc-trojan",
		},
		{
			name:      "httpupgrade tls",
			transport: map[string]interface{}{"type": "httpupgrade", "path": "/trojan-up"},
			wantType:  "httpupgrade",
			wantPath:  "/trojan-up",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			view := &core.SingboxInboundView{
				Type: "trojan",
				TLS: &core.TLSConfig{
					Enabled:    true,
					ServerName: "example.com",
					ALPN:       []string{"h2"},
				},
				Raw: map[string]interface{}{
					"transport": tc.transport,
				},
			}
			user := &core.UserInboundInfo{UUID: "secret-password"}

			link, err := buildTrojanLink("alice", user, view, "1.2.3.4", "443", nil, "")
			if err != nil {
				t.Fatalf("buildTrojanLink() error = %v", err)
			}
			u, err := url.Parse(link)
			if err != nil {
				t.Fatalf("parse link: %v", err)
			}
			if got := u.Query().Get("security"); got != "tls" {
				t.Fatalf("security = %q; want tls", got)
			}
			if got := u.Query().Get("type"); got != tc.wantType {
				t.Fatalf("type = %q; want %q", got, tc.wantType)
			}
			if got := u.Query().Get("path"); got != tc.wantPath {
				t.Fatalf("path = %q; want %q", got, tc.wantPath)
			}
			if got := u.Query().Get("serviceName"); got != tc.wantServiceName {
				t.Fatalf("serviceName = %q; want %q", got, tc.wantServiceName)
			}
		})
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

	link, err := buildVmessLink("alice", user, view, "1.2.3.4", "443", nil, "")
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

func TestBuildVmessLink_RecommendedTransportVariants(t *testing.T) {
	testCases := []struct {
		name         string
		transport    map[string]interface{}
		wantNet      string
		wantPath     string
		wantTLS      string
		wantSecurity string
	}{
		{
			name:         "tcp tls",
			transport:    nil,
			wantNet:      "tcp",
			wantTLS:      "tls",
			wantSecurity: "auto",
		},
		{
			name:         "grpc tls",
			transport:    map[string]interface{}{"type": "grpc", "service_name": "svc-vmess"},
			wantNet:      "grpc",
			wantPath:     "svc-vmess",
			wantTLS:      "tls",
			wantSecurity: "auto",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			raw := map[string]interface{}{}
			if tc.transport != nil {
				raw["transport"] = tc.transport
			}
			view := &core.SingboxInboundView{
				Type: "vmess",
				TLS: &core.TLSConfig{
					Enabled:    true,
					ServerName: "example.com",
				},
				Raw: raw,
			}
			user := &core.UserInboundInfo{
				UUID:          "11111111-1111-1111-1111-111111111111",
				VmessSecurity: "auto",
			}

			link, err := buildVmessLink("alice", user, view, "1.2.3.4", "443", nil, "")
			if err != nil {
				t.Fatalf("buildVmessLink() error = %v", err)
			}

			rawPayload := strings.TrimPrefix(link, "vmess://")
			decoded, err := base64.StdEncoding.DecodeString(rawPayload)
			if err != nil {
				t.Fatalf("decode vmess: %v", err)
			}
			var payload map[string]string
			if err := json.Unmarshal(decoded, &payload); err != nil {
				t.Fatalf("unmarshal payload: %v", err)
			}
			if got := payload["net"]; got != tc.wantNet {
				t.Fatalf("payload net = %q; want %q", got, tc.wantNet)
			}
			if got := payload["path"]; got != tc.wantPath {
				t.Fatalf("payload path = %q; want %q", got, tc.wantPath)
			}
			if got := payload["tls"]; got != tc.wantTLS {
				t.Fatalf("payload tls = %q; want %q", got, tc.wantTLS)
			}
			if got := payload["scy"]; got != tc.wantSecurity {
				t.Fatalf("payload scy = %q; want %q", got, tc.wantSecurity)
			}
		})
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

	link, err := buildVmessLink("alice", user, view, "1.2.3.4", "443", nil, "")
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

func TestBuildVlessLink_AllowInsecureMetadataOverride(t *testing.T) {
	view := &core.SingboxInboundView{
		Type: "vless",
		TLS: &core.TLSConfig{
			Enabled:         true,
			CertificatePath: "/etc/sing-box/certs/selfsigned_test.crt",
		},
		Raw: map[string]interface{}{
			"transport": map[string]interface{}{"type": "tcp"},
		},
	}
	user := &core.UserInboundInfo{UUID: "11111111-1111-1111-1111-111111111111"}

	forcedOff := false
	link, err := buildVlessLink("alice", user, view, "1.2.3.4", "443", &core.InboundMeta{
		Tag:               "test-vless",
		LinkAllowInsecure: &forcedOff,
	}, "")
	if err != nil {
		t.Fatalf("buildVlessLink() error = %v", err)
	}
	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse link: %v", err)
	}
	if got := u.Query().Get("allowInsecure"); got != "" {
		t.Fatalf("allowInsecure = %q; want empty when metadata disables it", got)
	}

	forcedOn := true
	link, err = buildVlessLink("alice", user, view, "1.2.3.4", "443", &core.InboundMeta{
		Tag:               "test-vless",
		LinkAllowInsecure: &forcedOn,
	}, "")
	if err != nil {
		t.Fatalf("buildVlessLink() error = %v", err)
	}
	u, err = url.Parse(link)
	if err != nil {
		t.Fatalf("parse link: %v", err)
	}
	if got := u.Query().Get("allowInsecure"); got != "1" {
		t.Fatalf("allowInsecure = %q; want %q when metadata enables it", got, "1")
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
		],
		"experimental": {
			"v2ray_api": {
				"listen": "127.0.0.1:19001",
				"stats": {
					"enabled": true,
					"inbounds": ["test-vless"],
					"outbounds": ["direct"],
					"users": []
				}
			}
		}
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

func TestHandleUpdateSingboxInbound_RenamePropagatesInboundReferences(t *testing.T) {
	server, stub, store := newSingboxHandlerTestServerWithStore(t, `{
		"inbounds": [
			{
				"type": "vless",
				"tag": "test-vless",
				"listen": "0.0.0.0",
				"listen_port": 443,
				"users": [
					{"name":"alice","uuid":"11111111-1111-1111-1111-111111111111"}
				],
				"tls": {"enabled": true, "server_name": "example.com"},
				"transport": {"type": "tcp"}
			}
		]
	}`)

	if err := store.SaveInboundMeta(core.InboundMeta{Tag: "test-vless", ExternalPort: 7443}); err != nil {
		t.Fatalf("SaveInboundMeta: %v", err)
	}
	if err := store.SaveUserMetadata(core.UserMetadata{
		Email:       "alice@example.com",
		Enabled:     true,
		InboundTags: []string{"test-vless"},
	}); err != nil {
		t.Fatalf("SaveUserMetadata: %v", err)
	}

	payload := `{
		"type": "vless",
		"tag": "renamed-vless",
		"listen": "0.0.0.0",
		"listen_port": 443,
		"users": [
			{"name":"alice","uuid":"11111111-1111-1111-1111-111111111111"}
		],
		"tls": {"enabled": true, "server_name": "example.com"},
		"transport": {"type": "tcp"}
	}`

	req := httptest.NewRequest(http.MethodPut, "/api/singbox/inbounds?tag=test-vless", bytes.NewBufferString(payload))
	rec := httptest.NewRecorder()
	server.handleUpdateSingboxInbound(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}

	var resp struct {
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v body=%q", err, rec.Body.String())
	}
	if resp.Warnings == nil {
		t.Fatalf("warnings = nil; want empty array")
	}
	if len(resp.Warnings) != 0 {
		t.Fatalf("warnings = %#v; want empty", resp.Warnings)
	}

	readStoredInboundByTag(t, stub, "renamed-vless")
	if statsInbounds := readStoredStatsInbounds(t, stub); len(statsInbounds) != 1 || statsInbounds[0] != "renamed-vless" {
		t.Fatalf("stats.inbounds = %#v; want [renamed-vless]", statsInbounds)
	}
	if meta, err := store.GetInboundMeta("renamed-vless"); err != nil {
		t.Fatalf("GetInboundMeta(new): %v", err)
	} else if meta == nil || meta.ExternalPort != 7443 {
		t.Fatalf("renamed inbound meta = %#v; want external port preserved", meta)
	}
	if meta, err := store.GetInboundMeta("test-vless"); err != nil {
		t.Fatalf("GetInboundMeta(old): %v", err)
	} else if meta != nil {
		t.Fatalf("old inbound meta still present: %#v", meta)
	}

	userMeta, err := store.GetUserMetadata("alice@example.com")
	if err != nil {
		t.Fatalf("GetUserMetadata: %v", err)
	}
	if userMeta == nil {
		t.Fatal("expected user metadata")
	}
	if len(userMeta.InboundTags) != 1 || userMeta.InboundTags[0] != "renamed-vless" {
		t.Fatalf("inbound_tags = %#v; want [renamed-vless]", userMeta.InboundTags)
	}
}

func TestHandleUpdateSingboxInbound_WebSocketSwitchReturnsWarningsAndStripsFlow(t *testing.T) {
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
				"tls": {"enabled": true, "server_name": "example.com"},
				"transport": {"type": "tcp"}
			}
		],
		"experimental": {
			"v2ray_api": {
				"listen": "127.0.0.1:19001",
				"stats": {
					"enabled": true,
					"inbounds": ["test-vless"],
					"outbounds": ["direct"],
					"users": []
				}
			}
		}
	}`)

	payload := `{
		"type": "vless",
		"tag": "test-vless",
		"listen": "0.0.0.0",
		"listen_port": 443,
		"users": [
			{"name":"alice","uuid":"11111111-1111-1111-1111-111111111111","flow":"xtls-rprx-vision"}
		],
		"tls": {"enabled": true, "server_name": "example.com"},
		"transport": {"type": "ws", "path": "/ws"}
	}`

	req := httptest.NewRequest(http.MethodPut, "/api/singbox/inbounds?tag=test-vless", bytes.NewBufferString(payload))
	rec := httptest.NewRecorder()
	server.handleUpdateSingboxInbound(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}

	var resp struct {
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v body=%q", err, rec.Body.String())
	}
	if len(resp.Warnings) == 0 {
		t.Fatalf("warnings = %#v; want at least one warning", resp.Warnings)
	}
	if !strings.Contains(strings.ToLower(resp.Warnings[0]), "flow") {
		t.Fatalf("warning %q does not mention flow stripping", resp.Warnings[0])
	}

	inbound := readStoredInboundByTag(t, stub, "test-vless")
	users, ok := inbound["users"].([]interface{})
	if !ok || len(users) != 1 {
		t.Fatalf("users = %#v; want one user", inbound["users"])
	}
	user, ok := users[0].(map[string]interface{})
	if !ok {
		t.Fatalf("user = %#v; want object", users[0])
	}
	if got := user["flow"]; got != nil {
		t.Fatalf("stored user flow = %#v; want removed", got)
	}
}

func TestHandleUpdateSingboxInbound_RenameFailureRollsBackConfig(t *testing.T) {
	server, stub, store := newSingboxHandlerTestServerWithStore(t, `{
		"inbounds": [
			{
				"type": "vless",
				"tag": "test-vless",
				"listen": "0.0.0.0",
				"listen_port": 443,
				"users": [
					{"name":"alice","uuid":"11111111-1111-1111-1111-111111111111"}
				],
				"tls": {"enabled": true, "server_name": "example.com"},
				"transport": {"type": "tcp"}
			}
		]
	}`)

	if err := store.SaveInboundMeta(core.InboundMeta{Tag: "test-vless", ExternalPort: 7443}); err != nil {
		t.Fatalf("SaveInboundMeta: %v", err)
	}
	if err := store.SaveUserMetadata(core.UserMetadata{
		Email:       "alice@example.com",
		Enabled:     true,
		InboundTags: []string{"test-vless"},
	}); err != nil {
		t.Fatalf("SaveUserMetadata: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close store: %v", err)
	}

	payload := `{
		"type": "vless",
		"tag": "renamed-vless",
		"listen": "0.0.0.0",
		"listen_port": 443,
		"users": [
			{"name":"alice","uuid":"11111111-1111-1111-1111-111111111111"}
		],
		"tls": {"enabled": true, "server_name": "example.com"},
		"transport": {"type": "tcp"}
	}`

	req := httptest.NewRequest(http.MethodPut, "/api/singbox/inbounds?tag=test-vless", bytes.NewBufferString(payload))
	rec := httptest.NewRecorder()
	server.handleUpdateSingboxInbound(rec, req)

	if rec.Code == http.StatusOK {
		t.Fatalf("expected rename failure, got 200 body=%q", rec.Body.String())
	}

	readStoredInboundByTag(t, stub, "test-vless")
	if statsInbounds := readStoredStatsInbounds(t, stub); len(statsInbounds) != 1 || statsInbounds[0] != "test-vless" {
		t.Fatalf("stats.inbounds = %#v; want rollback to original tag", statsInbounds)
	}
}

func TestBuildHysteria2Link_Basic(t *testing.T) {
	view := &core.SingboxInboundView{
		Type: "hysteria2",
		TLS:  nil,
		Raw:  map[string]interface{}{},
	}
	user := &core.UserInboundInfo{Password: "s3cr3t"}

	link, err := buildHysteria2Link("alice", user, view, "1.2.3.4", "443")
	if err != nil {
		t.Fatalf("buildHysteria2Link() error = %v", err)
	}
	if !strings.HasPrefix(link, "hysteria2://s3cr3t@1.2.3.4:443") {
		t.Errorf("link = %q; want prefix hysteria2://s3cr3t@1.2.3.4:443", link)
	}
	if !strings.HasSuffix(link, "#alice") {
		t.Errorf("link = %q; want suffix #alice", link)
	}
	if strings.Contains(link, "sni=") {
		t.Errorf("link should not contain sni=; got %q", link)
	}
	if strings.Contains(link, "obfs=") {
		t.Errorf("link should not contain obfs=; got %q", link)
	}
}

func TestBuildHysteria2Link_TLS(t *testing.T) {
	view := &core.SingboxInboundView{
		Type: "hysteria2",
		TLS: &core.TLSConfig{
			Enabled:    true,
			ServerName: "example.com",
		},
		Raw: map[string]interface{}{},
	}
	user := &core.UserInboundInfo{Password: "s3cr3t"}

	link, err := buildHysteria2Link("alice", user, view, "1.2.3.4", "443")
	if err != nil {
		t.Fatalf("buildHysteria2Link() error = %v", err)
	}
	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse link: %v", err)
	}
	if got := u.Query().Get("sni"); got != "example.com" {
		t.Errorf("sni = %q; want %q", got, "example.com")
	}
}

func TestBuildHysteria2Link_Obfs(t *testing.T) {
	view := &core.SingboxInboundView{
		Type: "hysteria2",
		TLS:  nil,
		Raw: map[string]interface{}{
			"obfs": map[string]interface{}{
				"type":     "salamander",
				"password": "obfs-secret",
			},
		},
	}
	user := &core.UserInboundInfo{Password: "s3cr3t"}

	link, err := buildHysteria2Link("alice", user, view, "1.2.3.4", "443")
	if err != nil {
		t.Fatalf("buildHysteria2Link() error = %v", err)
	}
	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse link: %v", err)
	}
	if got := u.Query().Get("obfs"); got != "salamander" {
		t.Errorf("obfs = %q; want %q", got, "salamander")
	}
	if got := u.Query().Get("obfs-password"); got != "obfs-secret" {
		t.Errorf("obfs-password = %q; want %q", got, "obfs-secret")
	}
}

func TestBuildHysteria2Link_NoObfs(t *testing.T) {
	view := &core.SingboxInboundView{
		Type: "hysteria2",
		TLS:  nil,
		Raw:  map[string]interface{}{},
	}
	user := &core.UserInboundInfo{Password: "s3cr3t"}

	link, err := buildHysteria2Link("alice", user, view, "1.2.3.4", "443")
	if err != nil {
		t.Fatalf("buildHysteria2Link() error = %v", err)
	}
	if strings.Contains(link, "obfs") {
		t.Errorf("link should not contain obfs; got %q", link)
	}
}

func TestBuildHysteria2Link_EmptyPassword(t *testing.T) {
	view := &core.SingboxInboundView{
		Type: "hysteria2",
		TLS:  nil,
		Raw:  map[string]interface{}{},
	}
	user := &core.UserInboundInfo{Password: ""}

	_, err := buildHysteria2Link("alice", user, view, "1.2.3.4", "443")
	if err == nil {
		t.Fatal("buildHysteria2Link() expected error for empty password; got nil")
	}
}

func TestBuildShadowsocksLink(t *testing.T) {
	view := &core.SingboxInboundView{
		Type: "shadowsocks",
		Raw: map[string]interface{}{
			"method": "2022-blake3-aes-128-gcm",
		},
	}
	user := &core.UserInboundInfo{Password: "shadow-secret"}

	link, err := buildShadowsocksLink("alice", user, view, "1.2.3.4", "443")
	if err != nil {
		t.Fatalf("buildShadowsocksLink() error = %v", err)
	}

	// Must be a valid ss:// SIP002 URI: ss://BASE64(method:password)@host:port#tag
	if !strings.HasPrefix(link, "ss://") {
		t.Fatalf("link = %q; want ss:// prefix", link)
	}
	// Split off fragment
	withoutFragment, fragment, _ := strings.Cut(link, "#")
	wantFragment := url.QueryEscape("alice")
	if fragment != wantFragment {
		t.Errorf("fragment = %q; want %q", fragment, wantFragment)
	}
	// Split userinfo from host
	rest := strings.TrimPrefix(withoutFragment, "ss://")
	atIdx := strings.LastIndex(rest, "@")
	if atIdx < 0 {
		t.Fatalf("link missing @: %q", link)
	}
	userinfo, hostport := rest[:atIdx], rest[atIdx+1:]
	if hostport != "1.2.3.4:443" {
		t.Errorf("hostport = %q; want %q", hostport, "1.2.3.4:443")
	}
	decoded, err := base64.StdEncoding.DecodeString(userinfo)
	if err != nil {
		t.Fatalf("base64 decode userinfo %q: %v", userinfo, err)
	}
	want := "2022-blake3-aes-128-gcm:shadow-secret"
	if string(decoded) != want {
		t.Errorf("decoded userinfo = %q; want %q", string(decoded), want)
	}
}

func TestBuildShadowsocksLink_2022MultiUser_IncludesServerKey(t *testing.T) {
	// Shadowsocks 2022 multi-user: inbound has top-level server password.
	// Client needs "server_key:user_key" so sing-box can authenticate.
	view := &core.SingboxInboundView{
		Type: "shadowsocks",
		Raw: map[string]interface{}{
			"method":   "2022-blake3-aes-128-gcm",
			"password": "server-key-base64",
		},
	}
	user := &core.UserInboundInfo{Password: "user-key-base64"}

	link, err := buildShadowsocksLink("alice", user, view, "1.2.3.4", "443")
	if err != nil {
		t.Fatalf("buildShadowsocksLink() error = %v", err)
	}

	withoutFragment, _, _ := strings.Cut(link, "#")
	rest := strings.TrimPrefix(withoutFragment, "ss://")
	atIdx := strings.LastIndex(rest, "@")
	if atIdx < 0 {
		t.Fatalf("link missing @: %q", link)
	}
	userinfo := rest[:atIdx]
	decoded, err := base64.StdEncoding.DecodeString(userinfo)
	if err != nil {
		t.Fatalf("base64 decode userinfo %q: %v", userinfo, err)
	}
	want := "2022-blake3-aes-128-gcm:server-key-base64:user-key-base64"
	if string(decoded) != want {
		t.Errorf("decoded userinfo = %q; want %q", string(decoded), want)
	}
}

func TestBuildShadowsocksLink_RecommendedMethods(t *testing.T) {
	methods := []string{
		"2022-blake3-aes-256-gcm",
		"2022-blake3-chacha20-poly1305",
		"aes-128-gcm",
		"aes-256-gcm",
		"chacha20-ietf-poly1305",
	}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			view := &core.SingboxInboundView{
				Type: "shadowsocks",
				Raw: map[string]interface{}{
					"method": method,
				},
			}
			user := &core.UserInboundInfo{Password: "shadow-secret"}

			link, err := buildShadowsocksLink("alice", user, view, "1.2.3.4", "443")
			if err != nil {
				t.Fatalf("buildShadowsocksLink() error = %v", err)
			}

			withoutFragment, _, _ := strings.Cut(link, "#")
			rest := strings.TrimPrefix(withoutFragment, "ss://")
			atIdx := strings.LastIndex(rest, "@")
			if atIdx < 0 {
				t.Fatalf("link missing @: %q", link)
			}
			userinfo := rest[:atIdx]
			decoded, err := base64.StdEncoding.DecodeString(userinfo)
			if err != nil {
				t.Fatalf("base64 decode userinfo %q: %v", userinfo, err)
			}
			want := method + ":shadow-secret"
			if string(decoded) != want {
				t.Errorf("decoded userinfo = %q; want %q", string(decoded), want)
			}
		})
	}
}

func TestHandleGetUserInbounds_ShadowsocksRedaction(t *testing.T) {
	const ssConfig = `{"inbounds":[{"type":"shadowsocks","tag":"ss-in","listen":"::","listen_port":8443,"method":"2022-blake3-aes-128-gcm","users":[{"name":"alice","password":"s3cr3t"}]}]}`

	server, _ := newSingboxHandlerTestServer(ssConfig)

	withPerms := func(perms core.PanelUserPermissions) *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/api/users/alice/inbounds", nil)
		req.SetPathValue("name", "alice")
		ctx := context.WithValue(req.Context(), permissionsContextKey, &perms)
		return req.WithContext(ctx)
	}

	decodeInbounds := func(t *testing.T, rec *httptest.ResponseRecorder) []core.UserInboundInfo {
		t.Helper()
		var inbounds []core.UserInboundInfo
		if err := json.NewDecoder(rec.Body).Decode(&inbounds); err != nil {
			t.Fatalf("decode inbounds: %v body=%q", err, rec.Body.String())
		}
		return inbounds
	}

	t.Run("read-only masks Shadowsocks password", func(t *testing.T) {
		rec := httptest.NewRecorder()
		server.handleGetUserInbounds(rec, withPerms(core.PanelUserPermissions{
			CanReadUsers:  true,
			CanWriteUsers: false,
		}))
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
		}
		inbounds := decodeInbounds(t, rec)
		if len(inbounds) == 0 {
			t.Fatal("expected at least one inbound; got none")
		}
		if got := inbounds[0].Password; got != maskedValue {
			t.Errorf("password = %q; want %q (masked)", got, maskedValue)
		}
		if got := inbounds[0].UUID; got != "" {
			t.Errorf("uuid = %q; want empty", got)
		}
	})

	t.Run("write-capable preserves plaintext password", func(t *testing.T) {
		rec := httptest.NewRecorder()
		server.handleGetUserInbounds(rec, withPerms(core.PanelUserPermissions{
			CanReadUsers:  true,
			CanWriteUsers: true,
		}))
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
		}
		inbounds := decodeInbounds(t, rec)
		if len(inbounds) == 0 {
			t.Fatal("expected at least one inbound; got none")
		}
		if got := inbounds[0].Password; got != "s3cr3t" {
			t.Errorf("password = %q; want %q (plaintext)", got, "s3cr3t")
		}
	})
}

func TestHandleGetUserInbounds_Hysteria2Redaction(t *testing.T) {
	const hy2Config = `{"inbounds":[{"type":"hysteria2","tag":"hy2-in","listen":"::","listen_port":8443,"tls":{"enabled":true},"users":[{"name":"alice","password":"s3cr3t"}]}]}`

	server, _ := newSingboxHandlerTestServer(hy2Config)

	withPerms := func(perms core.PanelUserPermissions) *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/api/users/alice/inbounds", nil)
		req.SetPathValue("name", "alice")
		ctx := context.WithValue(req.Context(), permissionsContextKey, &perms)
		return req.WithContext(ctx)
	}

	decodeInbounds := func(t *testing.T, rec *httptest.ResponseRecorder) []core.UserInboundInfo {
		t.Helper()
		var inbounds []core.UserInboundInfo
		if err := json.NewDecoder(rec.Body).Decode(&inbounds); err != nil {
			t.Fatalf("decode inbounds: %v body=%q", err, rec.Body.String())
		}
		return inbounds
	}

	// Sub-test 1: read-only token → password masked, uuid unchanged (empty)
	t.Run("read-only masks Hysteria2 password", func(t *testing.T) {
		rec := httptest.NewRecorder()
		server.handleGetUserInbounds(rec, withPerms(core.PanelUserPermissions{
			CanReadUsers:  true,
			CanWriteUsers: false,
		}))
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
		}
		inbounds := decodeInbounds(t, rec)
		if len(inbounds) == 0 {
			t.Fatal("expected at least one inbound; got none")
		}
		if got := inbounds[0].Password; got != maskedValue {
			t.Errorf("password = %q; want %q (masked)", got, maskedValue)
		}
		// UUID is empty for hysteria2 users — should NOT be masked
		if got := inbounds[0].UUID; got != "" {
			t.Errorf("uuid = %q; want empty (not masked when empty)", got)
		}
	})

	// Sub-test 2: write-capable token → password returned plaintext
	t.Run("write-capable preserves plaintext password", func(t *testing.T) {
		rec := httptest.NewRecorder()
		server.handleGetUserInbounds(rec, withPerms(core.PanelUserPermissions{
			CanReadUsers:  true,
			CanWriteUsers: true,
		}))
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
		}
		inbounds := decodeInbounds(t, rec)
		if len(inbounds) == 0 {
			t.Fatal("expected at least one inbound; got none")
		}
		if got := inbounds[0].Password; got != "s3cr3t" {
			t.Errorf("password = %q; want %q (plaintext)", got, "s3cr3t")
		}
	})
}

func TestDeleteUserRemovesSubscriptionMembership(t *testing.T) {
	server, _, _ := newSingboxHandlerTestServerWithStore(t, `{
		"inbounds": [
			{
				"type": "vless",
				"tag": "test-vless",
				"listen": "0.0.0.0",
				"listen_port": 443,
				"users": [
					{"name": "alice", "uuid": "11111111-1111-1111-1111-111111111111"},
					{"name": "bob", "uuid": "22222222-2222-2222-2222-222222222222"}
				]
			}
		]
	}`)

	created := createSubscriptionForTest(t, server, subscriptionMutationRequest{
		Name:        "Cleanup Bundle",
		QuotaLimit:  0,
		QuotaPeriod: "monthly",
		Users:       []string{"alice", "bob"},
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/users?name=alice", nil)
	rec := httptest.NewRecorder()
	server.handleDeleteUser(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%q", rec.Code, rec.Body.String())
	}

	got := getSubscriptionForTest(t, server, created.ID)
	if len(got.Users) != 1 || got.Users[0] != "bob" {
		t.Fatalf("subscription users=%v want [bob]", got.Users)
	}
}

func TestRemoveUserFromInboundRemovesSubscriptionMembershipWhenUnassigned(t *testing.T) {
	server, _, _ := newSingboxHandlerTestServerWithStore(t, `{
		"inbounds": [
			{
				"type": "vless",
				"tag": "test-vless",
				"listen": "0.0.0.0",
				"listen_port": 443,
				"users": [
					{"name": "alice", "uuid": "11111111-1111-1111-1111-111111111111"}
				]
			}
		]
	}`)

	created := createSubscriptionForTest(t, server, subscriptionMutationRequest{
		Name:        "Inbound Cleanup Bundle",
		QuotaLimit:  0,
		QuotaPeriod: "monthly",
		Users:       []string{"alice"},
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/users/alice/inbounds/test-vless", nil)
	req.SetPathValue("name", "alice")
	req.SetPathValue("tag", "test-vless")
	rec := httptest.NewRecorder()
	server.handleRemoveUserFromInbound(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("remove status=%d body=%q", rec.Code, rec.Body.String())
	}

	got := getSubscriptionForTest(t, server, created.ID)
	if len(got.Users) != 0 {
		t.Fatalf("subscription users=%v want empty", got.Users)
	}
}

func TestDeleteInboundRemovesOnlyUnassignedUsersFromSubscriptions(t *testing.T) {
	server, _, _ := newSingboxHandlerTestServerWithStoreAndManagedInbounds(t, `{
		"inbounds": [
			{
				"type": "vless",
				"tag": "test-vless",
				"listen": "0.0.0.0",
				"listen_port": 443,
				"users": [
					{"name": "alice", "uuid": "11111111-1111-1111-1111-111111111111"}
				]
			},
			{
				"type": "vmess",
				"tag": "test-vmess",
				"listen": "0.0.0.0",
				"listen_port": 8443,
				"users": [
					{"name": "bob", "uuid": "22222222-2222-2222-2222-222222222222"}
				]
			}
		]
	}`, []string{"test-vless", "test-vmess"})

	created := createSubscriptionForTest(t, server, subscriptionMutationRequest{
		Name:        "Inbound Delete Bundle",
		QuotaLimit:  0,
		QuotaPeriod: "monthly",
		Users:       []string{"alice", "bob"},
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/singbox/inbound?tag=test-vless", nil)
	rec := httptest.NewRecorder()
	server.handleDeleteSingboxInbound(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete inbound status=%d body=%q", rec.Code, rec.Body.String())
	}

	got := getSubscriptionForTest(t, server, created.ID)
	if len(got.Users) != 1 || got.Users[0] != "bob" {
		t.Fatalf("subscription users=%v want [bob]", got.Users)
	}
}
