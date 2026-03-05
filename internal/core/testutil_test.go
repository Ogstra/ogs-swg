// Package core test infrastructure provides shared stubs and factory functions used by tests in this package.
package core

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"sync"
	"testing"
)

// stubExecutor is an in-memory SystemExecutor for tests.
// It holds config bytes so tests can read/write without a sing-box binary or real disk.
type stubExecutor struct {
	mu   sync.Mutex
	data []byte
}

func (s *stubExecutor) ReadConfig(_ context.Context, _ string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.data...), nil
}

func (s *stubExecutor) WriteConfig(_ context.Context, _ string, content []byte, _ os.FileMode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = append([]byte(nil), content...)
	return nil
}

func (s *stubExecutor) ValidateSingboxConfig(_ context.Context, _ []byte) error { return nil }
func (s *stubExecutor) RestartService(_ context.Context, _ string) error        { return nil }
func (s *stubExecutor) StartService(_ context.Context, _ string) error          { return nil }
func (s *stubExecutor) StopService(_ context.Context, _ string) error           { return nil }
func (s *stubExecutor) IsServiceActive(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (s *stubExecutor) ApplySysctl(_ context.Context, _, _ string) error      { return nil }
func (s *stubExecutor) GetSysctl(_ context.Context, _ string) (string, error) { return "", nil }
func (s *stubExecutor) ReadJournal(_ context.Context, _ string, _ int) ([]string, error) {
	return nil, nil
}
func (s *stubExecutor) SearchJournal(_ context.Context, _, _ string, _ int) ([]string, error) {
	return nil, nil
}
func (s *stubExecutor) SyncWireGuard(_ context.Context, _ string, _ []byte) error { return nil }
func (s *stubExecutor) RestartWireGuard(_ context.Context, _ string) error        { return nil }
func (s *stubExecutor) ListWireGuardInterfaces(_ context.Context) ([]string, error) {
	return nil, nil
}
func (s *stubExecutor) EnableWireGuardInterface(_ context.Context, _ string) error { return nil }
func (s *stubExecutor) DisableWireGuardInterface(_ context.Context, _ string) error {
	return nil
}
func (s *stubExecutor) GetWireGuardStats(_ context.Context) (map[string]PeerStats, error) {
	return nil, nil
}
func (s *stubExecutor) CheckConnectivity(_ context.Context) error             { return nil }
func (s *stubExecutor) Close() error                                          { return nil }
func (s *stubExecutor) Dial(_ context.Context, _, _ string) (net.Conn, error) { return nil, nil }

// newTestConfig creates a *Config wired to an in-memory stubExecutor.
// initialJSON is the raw JSON that ReadConfig will return from the stub.
func newTestConfig(t *testing.T, initialJSON string) (*Config, *stubExecutor) {
	t.Helper()
	stub := &stubExecutor{data: []byte(initialJSON)}
	cfg := &Config{
		SingboxConfigPath: "/test/config.json",
		ManagedInbounds:   []string{"test-vless", "test-vmess", "test-trojan"},
		StatsInbounds:     []string{"test-vless"},
		StatsOutbounds:    []string{"direct"},
		SingboxAPIAddr:    "127.0.0.1:19001",
		EnableSingbox:     false, // skip sing-box binary calls
	}
	cfg.SetExecutor(stub)
	return cfg, stub
}

// newTestConfigJSON creates a *Config wired to an in-memory stubExecutor,
// marshalling parts into JSON first.
func newTestConfigJSON(t *testing.T, parts map[string]interface{}) (*Config, *stubExecutor) {
	t.Helper()
	data, err := json.MarshalIndent(parts, "", "  ")
	if err != nil {
		t.Fatalf("newTestConfigJSON: marshal: %v", err)
	}
	return newTestConfig(t, string(data))
}
