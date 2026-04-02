package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Ogstra/ogs-swg/internal/core"
)

type multiIfaceExecutorStub struct {
	files            map[string][]byte
	lastReadPath     string
	lastWritePath    string
	lastWriteContent []byte
	writeCalls       int
	lastSyncIface    string
	enableCalls      int
	disableCalls     int
	lastEnableIface  string
	lastDisableIface string
	enableErr        error
	disableErr       error
	writeErr         error
}

func newMultiIfaceExecutorStub() *multiIfaceExecutorStub {
	return &multiIfaceExecutorStub{files: make(map[string][]byte)}
}

func (s *multiIfaceExecutorStub) RestartService(context.Context, string) error { return nil }
func (s *multiIfaceExecutorStub) StartService(context.Context, string) error   { return nil }
func (s *multiIfaceExecutorStub) StopService(context.Context, string) error    { return nil }
func (s *multiIfaceExecutorStub) IsServiceActive(context.Context, string) (bool, error) {
	return false, nil
}
func (s *multiIfaceExecutorStub) WriteConfig(_ context.Context, path string, content []byte, _ os.FileMode) error {
	if s.writeErr != nil {
		return s.writeErr
	}
	s.writeCalls++
	s.lastWritePath = path
	s.lastWriteContent = append([]byte(nil), content...)
	s.files[path] = append([]byte(nil), content...)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(path, content, 0644); err != nil {
		return err
	}
	return nil
}
func (s *multiIfaceExecutorStub) ReadConfig(_ context.Context, path string) ([]byte, error) {
	s.lastReadPath = path
	content, ok := s.files[path]
	if !ok {
		if disk, err := os.ReadFile(path); err == nil {
			return append([]byte(nil), disk...), nil
		}
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), content...), nil
}
func (s *multiIfaceExecutorStub) ApplySysctl(context.Context, string, string) error { return nil }
func (s *multiIfaceExecutorStub) GetSysctl(context.Context, string) (string, error) { return "", nil }
func (s *multiIfaceExecutorStub) ReadJournal(context.Context, string, int) ([]string, error) {
	return nil, nil
}
func (s *multiIfaceExecutorStub) ReadAllJournal(context.Context, string) ([]string, error) {
	return nil, nil
}
func (s *multiIfaceExecutorStub) SearchJournal(context.Context, string, string, int) ([]string, error) {
	return nil, nil
}
func (s *multiIfaceExecutorStub) SyncWireGuard(_ context.Context, interfaceName string, configContent []byte) error {
	if len(configContent) == 0 {
		return fmt.Errorf("empty sync content")
	}
	s.lastSyncIface = interfaceName
	return nil
}
func (s *multiIfaceExecutorStub) RestartWireGuard(context.Context, string) error { return nil }
func (s *multiIfaceExecutorStub) ListWireGuardInterfaces(context.Context) ([]string, error) {
	return []string{"wg0", "wg1"}, nil
}
func (s *multiIfaceExecutorStub) EnableWireGuardInterface(_ context.Context, interfaceName string) error {
	if s.enableErr != nil {
		return s.enableErr
	}
	s.enableCalls++
	s.lastEnableIface = interfaceName
	return nil
}
func (s *multiIfaceExecutorStub) DisableWireGuardInterface(_ context.Context, interfaceName string) error {
	if s.disableErr != nil {
		return s.disableErr
	}
	s.disableCalls++
	s.lastDisableIface = interfaceName
	return nil
}
func (s *multiIfaceExecutorStub) ValidateSingboxConfig(context.Context, []byte) error { return nil }
func (s *multiIfaceExecutorStub) GetWireGuardStats(context.Context) (map[string]core.PeerStats, error) {
	return nil, nil
}
func (s *multiIfaceExecutorStub) CheckConnectivity(context.Context) error { return nil }
func (s *multiIfaceExecutorStub) Close() error                            { return nil }
func (s *multiIfaceExecutorStub) Dial(context.Context, string, string) (net.Conn, error) {
	return nil, nil
}

func TestWireGuardMultiInterface_RoutesAndValidation(t *testing.T) {
	tmp := t.TempDir()

	wg0 := filepath.Join(tmp, "wg0.conf")
	wg1 := filepath.Join(tmp, "wg1.conf")
	bad := filepath.Join(tmp, "bad.conf")
	if err := os.WriteFile(wg0, []byte("[Interface]\nAddress = 10.0.0.1/24\nListenPort = 51820\n"), 0644); err != nil {
		t.Fatalf("write wg0: %v", err)
	}
	if err := os.WriteFile(wg1, []byte("[Interface]\nAddress = 10.1.0.1/24\nListenPort = 51821\n"), 0644); err != nil {
		t.Fatalf("write wg1: %v", err)
	}
	if err := os.WriteFile(bad, []byte("[Peer]\nPublicKey = abc\n"), 0644); err != nil {
		t.Fatalf("write bad: %v", err)
	}

	cfg := &core.Config{
		EnableWireGuard:     true,
		WireGuardConfigPath: wg0,
		WireGuardConfigDir:  tmp,
	}
	srv := NewServer(nil, cfg, newMultiIfaceExecutorStub())

	req := httptest.NewRequest(http.MethodGet, "/api/wireguard/interfaces", nil)
	rec := httptest.NewRecorder()
	srv.handleListWireGuardInterfaces(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("interfaces status=%d body=%q", rec.Code, rec.Body.String())
	}
	var got []string
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode interfaces: %v", err)
	}
	want := []string{"wg0", "wg1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("interfaces=%v want=%v", got, want)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/wireguard/interfaces/%2e%2e/peers", nil)
	req.SetPathValue("iface", "..")
	rec = httptest.NewRecorder()
	srv.handleGetWireGuardPeersForInterface(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid iface status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestWireGuardMultiInterface_PeerCRUDUsesSelectedInterface(t *testing.T) {
	tmp := t.TempDir()
	wg0Path := filepath.Join(tmp, "wg0.conf")
	wg1Path := filepath.Join(tmp, "wg1.conf")

	serverPriv0, _, err := core.GenerateWireGuardKeys()
	if err != nil {
		t.Fatalf("GenerateWireGuardKeys wg0: %v", err)
	}
	serverPriv1, _, err := core.GenerateWireGuardKeys()
	if err != nil {
		t.Fatalf("GenerateWireGuardKeys wg1: %v", err)
	}
	clientPriv, clientPub, err := core.GenerateWireGuardKeys()
	if err != nil {
		t.Fatalf("GenerateWireGuardKeys peer: %v", err)
	}

	wg0Text := "[Interface]\nPrivateKey = " + serverPriv0 + "\nAddress = 10.10.0.1/24\nListenPort = 51820\n\n[Peer]\nPublicKey = " + clientPub + "\nAllowedIPs = 10.10.0.2/32\n"
	wg1Text := "[Interface]\nPrivateKey = " + serverPriv1 + "\nAddress = 10.20.0.1/24\nListenPort = 51821\n\n[Peer]\nPublicKey = " + clientPub + "\nAllowedIPs = 10.20.0.2/32\n"

	stub := newMultiIfaceExecutorStub()
	stub.files[wg0Path] = []byte(wg0Text)
	stub.files[wg1Path] = []byte(wg1Text)

	cfg := &core.Config{
		EnableWireGuard:     true,
		PublicIP:            "198.51.100.10",
		WireGuardConfigPath: wg0Path,
		WireGuardConfigDir:  tmp,
	}
	srv := NewServer(nil, cfg, stub)

	updatePayload := map[string]any{
		"alias":       "peer-wg1",
		"allowed_ips": "10.20.0.9/32",
		"endpoint":    "vpn.example.test:51821",
	}
	body, _ := json.Marshal(updatePayload)
	req := httptest.NewRequest(http.MethodPut, "/api/wireguard/interfaces/wg1/peer?public_key="+url.QueryEscape(clientPub), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("iface", "wg1")
	rec := httptest.NewRecorder()
	srv.handleUpdateWireGuardPeerForInterface(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update peer status=%d body=%q", rec.Code, rec.Body.String())
	}
	if stub.lastReadPath != wg1Path {
		t.Fatalf("read path=%q want=%q", stub.lastReadPath, wg1Path)
	}
	if stub.lastWritePath != wg1Path {
		t.Fatalf("write path=%q want=%q", stub.lastWritePath, wg1Path)
	}
	if stub.lastSyncIface != "wg1" {
		t.Fatalf("sync iface=%q want=wg1", stub.lastSyncIface)
	}

	req = httptest.NewRequest(
		http.MethodGet,
		"/api/wireguard/interfaces/wg1/peer/config?public_key="+url.QueryEscape(clientPub)+"&private_key="+url.QueryEscape(clientPriv),
		nil,
	)
	req.SetPathValue("iface", "wg1")
	rec = httptest.NewRecorder()
	srv.handleGetWireGuardPeerConfigForInterface(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("peer config status=%d body=%q", rec.Code, rec.Body.String())
	}
	var payload map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode peer config: %v", err)
	}
	configText := payload["config"]
	if !strings.Contains(configText, "Endpoint = 198.51.100.10:51821") {
		t.Fatalf("peer config endpoint missing iface port, got: %q", configText)
	}
}

func TestWireGuardMultiInterface_CreateInterface_Success(t *testing.T) {
	tmp := t.TempDir()
	wg0 := filepath.Join(tmp, "wg0.conf")
	if err := os.WriteFile(wg0, []byte("[Interface]\nAddress = 10.0.0.1/24\nListenPort = 51820\nPrivateKey = base\n"), 0644); err != nil {
		t.Fatalf("write wg0: %v", err)
	}

	stub := newMultiIfaceExecutorStub()
	cfg := &core.Config{
		EnableWireGuard:     true,
		WireGuardConfigPath: wg0,
		WireGuardConfigDir:  tmp,
	}
	srv := NewServer(nil, cfg, stub)

	body := bytes.NewBufferString(`{"name":"wg2","subnet":"10.20.0.0/24","listen_port":51830}`)
	req := httptest.NewRequest(http.MethodPost, "/api/wireguard/interfaces", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.handleCreateWireGuardInterface(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create interface status=%d body=%q", rec.Code, rec.Body.String())
	}

	var created CreateWireGuardInterfaceResponse
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if created.Name != "wg2" {
		t.Fatalf("name=%q want=wg2", created.Name)
	}
	if created.Subnet != "10.20.0.0/24" {
		t.Fatalf("subnet=%q want=10.20.0.0/24", created.Subnet)
	}
	if created.Address != "10.20.0.1/24" {
		t.Fatalf("address=%q want=10.20.0.1/24", created.Address)
	}
	if created.ListenPort != 51830 {
		t.Fatalf("listen_port=%d want=51830", created.ListenPort)
	}
	if strings.TrimSpace(created.PublicKey) == "" {
		t.Fatalf("public_key should be set")
	}

	wg2Path := filepath.Join(tmp, "wg2.conf")
	if stub.lastWritePath != wg2Path || stub.writeCalls != 1 {
		t.Fatalf("write path=%q calls=%d want_path=%q", stub.lastWritePath, stub.writeCalls, wg2Path)
	}
	if stub.enableCalls != 1 || stub.lastEnableIface != "wg2" {
		t.Fatalf("enable calls=%d iface=%q", stub.enableCalls, stub.lastEnableIface)
	}

	content, err := os.ReadFile(wg2Path)
	if err != nil {
		t.Fatalf("read created config: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, "[Interface]") || !strings.Contains(text, "Address = 10.20.0.1/24") || !strings.Contains(text, "ListenPort = 51830") {
		t.Fatalf("created config content invalid: %q", text)
	}
	if !strings.Contains(text, "PrivateKey = ") {
		t.Fatalf("created config missing private key")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/wireguard/interfaces", nil)
	rec = httptest.NewRecorder()
	srv.handleListWireGuardInterfaces(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("interfaces status=%d body=%q", rec.Code, rec.Body.String())
	}
	var got []string
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode interfaces: %v", err)
	}
	want := []string{"wg0", "wg2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("interfaces=%v want=%v", got, want)
	}
}

func TestWireGuardMultiInterface_CreateInterface_RejectsSubnetOverlap(t *testing.T) {
	tmp := t.TempDir()
	wg0 := filepath.Join(tmp, "wg0.conf")
	if err := os.WriteFile(wg0, []byte("[Interface]\nAddress = 10.50.0.1/24\nListenPort = 51820\n"), 0644); err != nil {
		t.Fatalf("write wg0: %v", err)
	}

	stub := newMultiIfaceExecutorStub()
	cfg := &core.Config{
		EnableWireGuard:     true,
		WireGuardConfigPath: wg0,
		WireGuardConfigDir:  tmp,
	}
	srv := NewServer(nil, cfg, stub)

	req := httptest.NewRequest(http.MethodPost, "/api/wireguard/interfaces", bytes.NewBufferString(`{"name":"wg2","subnet":"10.50.0.128/25","listen_port":51830}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.handleCreateWireGuardInterface(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("overlap status=%d body=%q", rec.Code, rec.Body.String())
	}
	if stub.writeCalls != 0 {
		t.Fatalf("writeCalls=%d want=0", stub.writeCalls)
	}
	if stub.enableCalls != 0 {
		t.Fatalf("enableCalls=%d want=0", stub.enableCalls)
	}
	if _, err := os.Stat(filepath.Join(tmp, "wg2.conf")); !os.IsNotExist(err) {
		t.Fatalf("wg2.conf should not exist, err=%v", err)
	}
}

func TestWireGuardMultiInterface_CreateInterface_RejectsInvalidInput(t *testing.T) {
	tmp := t.TempDir()
	wg0 := filepath.Join(tmp, "wg0.conf")
	if err := os.WriteFile(wg0, []byte("[Interface]\nAddress = 10.60.0.1/24\nListenPort = 51820\n"), 0644); err != nil {
		t.Fatalf("write wg0: %v", err)
	}

	stub := newMultiIfaceExecutorStub()
	cfg := &core.Config{
		EnableWireGuard:     true,
		WireGuardConfigPath: wg0,
		WireGuardConfigDir:  tmp,
	}
	srv := NewServer(nil, cfg, stub)

	cases := []struct {
		name    string
		payload string
	}{
		{
			name:    "invalid-interface-name",
			payload: `{"name":"../etc/passwd","subnet":"10.61.0.0/24","listen_port":51830}`,
		},
		{
			name:    "invalid-listen-port",
			payload: `{"name":"wg3","subnet":"10.61.0.0/24","listen_port":70000}`,
		},
		{
			name:    "private-key-not-allowed",
			payload: `{"name":"wg3","subnet":"10.61.0.0/24","listen_port":51830,"private_key":"forbidden"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/wireguard/interfaces", bytes.NewBufferString(tc.payload))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			srv.handleCreateWireGuardInterface(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
			}
		})
	}
	if stub.writeCalls != 0 {
		t.Fatalf("writeCalls=%d want=0", stub.writeCalls)
	}
	if stub.enableCalls != 0 {
		t.Fatalf("enableCalls=%d want=0", stub.enableCalls)
	}
}

func TestWireGuardMultiInterface_EnableDisable(t *testing.T) {
	tmp := t.TempDir()
	wg0 := filepath.Join(tmp, "wg0.conf")
	if err := os.WriteFile(wg0, []byte("[Interface]\nAddress = 10.0.0.1/24\nListenPort = 51820\n"), 0644); err != nil {
		t.Fatalf("write wg0: %v", err)
	}

	stub := newMultiIfaceExecutorStub()
	cfg := &core.Config{
		EnableWireGuard:     true,
		WireGuardConfigPath: wg0,
		WireGuardConfigDir:  tmp,
	}
	srv := NewServer(nil, cfg, stub)

	req := httptest.NewRequest(http.MethodPost, "/api/wireguard/interfaces/wg1/enable", nil)
	req.SetPathValue("iface", "wg1")
	rec := httptest.NewRecorder()
	srv.handleEnableWireGuardInterface(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("enable status=%d body=%q", rec.Code, rec.Body.String())
	}
	if stub.enableCalls != 1 || stub.lastEnableIface != "wg1" {
		t.Fatalf("enable calls=%d iface=%q", stub.enableCalls, stub.lastEnableIface)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/wireguard/interfaces/wg1/disable", nil)
	req.SetPathValue("iface", "wg1")
	rec = httptest.NewRecorder()
	srv.handleDisableWireGuardInterface(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable status=%d body=%q", rec.Code, rec.Body.String())
	}
	if stub.disableCalls != 1 || stub.lastDisableIface != "wg1" {
		t.Fatalf("disable calls=%d iface=%q", stub.disableCalls, stub.lastDisableIface)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/wireguard/interfaces/bad$name/enable", nil)
	req.SetPathValue("iface", "bad$name")
	rec = httptest.NewRecorder()
	srv.handleEnableWireGuardInterface(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid iface status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestWireGuardMultiInterface_DeleteInterface_Success(t *testing.T) {
	tmp := t.TempDir()
	wg0 := filepath.Join(tmp, "wg0.conf")
	wg1 := filepath.Join(tmp, "wg1.conf")
	if err := os.WriteFile(wg0, []byte("[Interface]\nAddress = 10.0.0.1/24\nListenPort = 51820\n"), 0644); err != nil {
		t.Fatalf("write wg0: %v", err)
	}
	if err := os.WriteFile(wg1, []byte("[Interface]\nAddress = 10.1.0.1/24\nListenPort = 51821\n"), 0644); err != nil {
		t.Fatalf("write wg1: %v", err)
	}

	stub := newMultiIfaceExecutorStub()
	cfg := &core.Config{
		EnableWireGuard:     true,
		WireGuardConfigPath: wg0,
		WireGuardConfigDir:  tmp,
	}
	srv := NewServer(nil, cfg, stub)

	req := httptest.NewRequest(http.MethodDelete, "/api/wireguard/interfaces/wg1", nil)
	req.SetPathValue("iface", "wg1")
	rec := httptest.NewRecorder()
	srv.handleDeleteWireGuardInterface(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%q", rec.Code, rec.Body.String())
	}
	if stub.disableCalls != 1 || stub.lastDisableIface != "wg1" {
		t.Fatalf("disable calls=%d iface=%q", stub.disableCalls, stub.lastDisableIface)
	}
	if _, err := os.Stat(wg1); !os.IsNotExist(err) {
		t.Fatalf("wg1 should be removed, err=%v", err)
	}
}

func TestWireGuardMultiInterface_DeleteInterface_RejectsInvalidIface(t *testing.T) {
	tmp := t.TempDir()
	wg0 := filepath.Join(tmp, "wg0.conf")
	if err := os.WriteFile(wg0, []byte("[Interface]\nAddress = 10.0.0.1/24\nListenPort = 51820\n"), 0644); err != nil {
		t.Fatalf("write wg0: %v", err)
	}

	stub := newMultiIfaceExecutorStub()
	cfg := &core.Config{
		EnableWireGuard:     true,
		WireGuardConfigPath: wg0,
		WireGuardConfigDir:  tmp,
	}
	srv := NewServer(nil, cfg, stub)

	req := httptest.NewRequest(http.MethodDelete, "/api/wireguard/interfaces/bad$name", nil)
	req.SetPathValue("iface", "bad$name")
	rec := httptest.NewRecorder()
	srv.handleDeleteWireGuardInterface(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid iface status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestWireGuardMultiInterface_DeleteInterface_MissingInterface(t *testing.T) {
	tmp := t.TempDir()
	wg0 := filepath.Join(tmp, "wg0.conf")
	if err := os.WriteFile(wg0, []byte("[Interface]\nAddress = 10.0.0.1/24\nListenPort = 51820\n"), 0644); err != nil {
		t.Fatalf("write wg0: %v", err)
	}

	stub := newMultiIfaceExecutorStub()
	cfg := &core.Config{
		EnableWireGuard:     true,
		WireGuardConfigPath: wg0,
		WireGuardConfigDir:  tmp,
	}
	srv := NewServer(nil, cfg, stub)

	req := httptest.NewRequest(http.MethodDelete, "/api/wireguard/interfaces/wg99", nil)
	req.SetPathValue("iface", "wg99")
	rec := httptest.NewRecorder()
	srv.handleDeleteWireGuardInterface(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing iface status=%d body=%q", rec.Code, rec.Body.String())
	}
}
