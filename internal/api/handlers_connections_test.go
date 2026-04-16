package api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
)

type connectionsExecutorStub struct{}

func (s *connectionsExecutorStub) RestartService(context.Context, string) error { return nil }
func (s *connectionsExecutorStub) StartService(context.Context, string) error   { return nil }
func (s *connectionsExecutorStub) StopService(context.Context, string) error    { return nil }
func (s *connectionsExecutorStub) IsServiceActive(context.Context, string) (bool, error) {
	return true, nil
}
func (s *connectionsExecutorStub) WriteConfig(context.Context, string, []byte, os.FileMode) error {
	return nil
}
func (s *connectionsExecutorStub) ReadConfig(_ context.Context, path string) ([]byte, error) {
	return os.ReadFile(path)
}
func (s *connectionsExecutorStub) ApplySysctl(context.Context, string, string) error { return nil }
func (s *connectionsExecutorStub) GetSysctl(context.Context, string) (string, error) { return "", nil }
func (s *connectionsExecutorStub) ReadJournal(context.Context, string, int) ([]string, error) {
	return nil, nil
}
func (s *connectionsExecutorStub) ReadAllJournal(context.Context, string) ([]string, error) {
	return nil, nil
}
func (s *connectionsExecutorStub) SearchJournal(context.Context, string, string, int) ([]string, error) {
	return nil, nil
}
func (s *connectionsExecutorStub) SyncWireGuard(context.Context, string, []byte) error { return nil }
func (s *connectionsExecutorStub) RestartWireGuard(context.Context, string) error      { return nil }
func (s *connectionsExecutorStub) ListWireGuardInterfaces(context.Context) ([]string, error) {
	return nil, nil
}
func (s *connectionsExecutorStub) EnableWireGuardInterface(context.Context, string) error { return nil }
func (s *connectionsExecutorStub) DisableWireGuardInterface(context.Context, string) error {
	return nil
}
func (s *connectionsExecutorStub) ValidateSingboxConfig(context.Context, []byte) error { return nil }
func (s *connectionsExecutorStub) GetWireGuardStats(context.Context) (map[string]core.PeerStats, error) {
	return nil, nil
}
func (s *connectionsExecutorStub) CheckConnectivity(context.Context) error { return nil }
func (s *connectionsExecutorStub) Close() error                            { return nil }
func (s *connectionsExecutorStub) Dial(context.Context, string, string) (net.Conn, error) {
	return nil, nil
}

func TestHandleGetConnections_RealtimeAggregatesInboundTags(t *testing.T) {
	var authHeader string
	clashServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"connections": []map[string]interface{}{
				{
					"metadata": map[string]interface{}{"type": "vless/alice"},
					"upload":   10,
					"download": 20,
				},
				{
					"metadata": map[string]interface{}{"type": "vless/alice"},
					"upload":   30,
					"download": 40,
				},
				{
					"metadata": map[string]interface{}{"type": "trojan/bob"},
					"upload":   5,
					"download": 15,
				},
			},
		})
	}))
	defer clashServer.Close()

	server := newConnectionsTestServer(t, connectionsTestOptions{
		singboxConfig: `{"experimental":{"clash_api":{"external_controller":"` + clashServer.Listener.Addr().String() + `","secret":"test-secret"}}}`,
		enableSingbox: true,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/connections", nil)
	rec := httptest.NewRecorder()
	server.handleGetConnections(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
	if authHeader != "Bearer test-secret" {
		t.Fatalf("Authorization header = %q; want %q", authHeader, "Bearer test-secret")
	}

	var payload ConnectionsResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.Realtime {
		t.Fatalf("Realtime = %v; want true", payload.Realtime)
	}
	if len(payload.Users) != 2 {
		t.Fatalf("users len = %d; want 2 (%+v)", len(payload.Users), payload.Users)
	}

	users := make(map[string]ConnectionUser, len(payload.Users))
	for _, user := range payload.Users {
		users[user.Name] = user
	}

	alice := users["alice"]
	if alice.Upload != 40 || alice.Download != 60 || alice.Connections != 2 {
		t.Fatalf("alice = %+v; want upload=40 download=60 connections=2", alice)
	}
	bob := users["bob"]
	if bob.Upload != 5 || bob.Download != 15 || bob.Connections != 1 {
		t.Fatalf("bob = %+v; want upload=5 download=15 connections=1", bob)
	}
}

func TestHandleGetConnections_FallsBackWhenClashAPIUnavailable(t *testing.T) {
	now := time.Now().Unix()
	server := newConnectionsTestServer(t, connectionsTestOptions{
		singboxConfig: `{"experimental":{}}`,
		enableSingbox: true,
		activeSamples: []core.Sample{
			{User: "bob", Timestamp: now - 60, Uplink: 5, Downlink: 5},
			{User: "alice", Timestamp: now - 30, Uplink: 10, Downlink: 10},
		},
		activeThresholdBytes: 1,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/connections", nil)
	rec := httptest.NewRecorder()
	server.handleGetConnections(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}

	var payload ConnectionsResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Realtime {
		t.Fatalf("Realtime = %v; want false", payload.Realtime)
	}
	if len(payload.Users) != 2 {
		t.Fatalf("users len = %d; want 2 (%+v)", len(payload.Users), payload.Users)
	}
	if payload.Users[0].Name != "alice" || payload.Users[1].Name != "bob" {
		t.Fatalf("users = %+v; want alphabetical fallback names", payload.Users)
	}
}

func TestHandleGetConnections_FallsBackOnClashAPIError(t *testing.T) {
	clashServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer clashServer.Close()

	now := time.Now().Unix()
	server := newConnectionsTestServer(t, connectionsTestOptions{
		singboxConfig: `{"experimental":{"clash_api":{"external_controller":"` + clashServer.Listener.Addr().String() + `"}}}`,
		enableSingbox: true,
		activeSamples: []core.Sample{
			{User: "carol", Timestamp: now - 45, Uplink: 8, Downlink: 12},
		},
		activeThresholdBytes: 1,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/connections", nil)
	rec := httptest.NewRecorder()
	server.handleGetConnections(rec, req)

	var payload ConnectionsResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Realtime {
		t.Fatalf("Realtime = %v; want false", payload.Realtime)
	}
	if len(payload.Users) != 1 || payload.Users[0].Name != "carol" {
		t.Fatalf("users = %+v; want fallback user carol", payload.Users)
	}
}

func TestHandleGetConnections_DemoModeFallback(t *testing.T) {
	server := newConnectionsTestServer(t, connectionsTestOptions{
		singboxConfig: `{"inbounds":[{"type":"vless","tag":"demo","listen":"0.0.0.0","listen_port":443,"users":[{"name":"demo-user","uuid":"11111111-1111-1111-1111-111111111111"}]}]}`,
		enableSingbox: true,
		demoMode:      true,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/connections", nil)
	rec := httptest.NewRecorder()
	server.handleGetConnections(rec, req)

	var payload ConnectionsResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Realtime {
		t.Fatalf("Realtime = %v; want false", payload.Realtime)
	}
	if len(payload.Users) != 1 || payload.Users[0].Name != "demo-user" {
		t.Fatalf("users = %+v; want demo fallback user", payload.Users)
	}
}

type connectionsTestOptions struct {
	singboxConfig        string
	enableSingbox        bool
	demoMode             bool
	activeThresholdBytes int64
	activeSamples        []core.Sample
}

func newConnectionsTestServer(t *testing.T, opts connectionsTestOptions) *Server {
	t.Helper()

	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.db")
	store, err := core.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if len(opts.activeSamples) > 0 {
		if err := store.BulkInsert(opts.activeSamples); err != nil {
			t.Fatalf("BulkInsert: %v", err)
		}
	}

	configPath := filepath.Join(tmp, "singbox.json")
	if err := os.WriteFile(configPath, []byte(opts.singboxConfig), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg := &core.Config{
		SingboxConfigPath:    configPath,
		EnableSingbox:        opts.enableSingbox,
		DemoMode:             opts.demoMode,
		ActiveThresholdBytes: opts.activeThresholdBytes,
	}

	exec := &connectionsExecutorStub{}
	cfg.SetExecutor(exec)

	return NewServer(store, cfg, exec)
}
