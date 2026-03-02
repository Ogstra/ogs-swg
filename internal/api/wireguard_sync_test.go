package api

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/Ogstra/ogs-swg/internal/core"
)

type wireGuardExecutorStub struct {
	files            map[string][]byte
	lastReadPath     string
	lastWritePath    string
	lastWriteContent []byte
	syncCalls        int
	restartCalls     int
	lastSyncIface    string
	lastRestartIface string
}

func newWireGuardExecutorStub() *wireGuardExecutorStub {
	return &wireGuardExecutorStub{files: make(map[string][]byte)}
}

func (s *wireGuardExecutorStub) RestartService(context.Context, string) error { return nil }
func (s *wireGuardExecutorStub) StartService(context.Context, string) error   { return nil }
func (s *wireGuardExecutorStub) StopService(context.Context, string) error    { return nil }
func (s *wireGuardExecutorStub) IsServiceActive(context.Context, string) (bool, error) {
	return false, nil
}
func (s *wireGuardExecutorStub) WriteConfig(_ context.Context, path string, content []byte, _ os.FileMode) error {
	s.lastWritePath = path
	s.lastWriteContent = append([]byte(nil), content...)
	s.files[path] = append([]byte(nil), content...)
	return nil
}
func (s *wireGuardExecutorStub) ReadConfig(_ context.Context, path string) ([]byte, error) {
	s.lastReadPath = path
	content, ok := s.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), content...), nil
}
func (s *wireGuardExecutorStub) ApplySysctl(context.Context, string, string) error { return nil }
func (s *wireGuardExecutorStub) GetSysctl(context.Context, string) (string, error) { return "", nil }
func (s *wireGuardExecutorStub) ReadJournal(context.Context, string, int) ([]string, error) {
	return nil, nil
}
func (s *wireGuardExecutorStub) SearchJournal(context.Context, string, string, int) ([]string, error) {
	return nil, nil
}
func (s *wireGuardExecutorStub) SyncWireGuard(_ context.Context, interfaceName string, configContent []byte) error {
	s.syncCalls++
	s.lastSyncIface = interfaceName
	if len(configContent) == 0 {
		return fmt.Errorf("empty sync content")
	}
	return nil
}
func (s *wireGuardExecutorStub) RestartWireGuard(_ context.Context, interfaceName string) error {
	s.restartCalls++
	s.lastRestartIface = interfaceName
	return nil
}
func (s *wireGuardExecutorStub) ListWireGuardInterfaces(context.Context) ([]string, error) {
	return []string{"wg0"}, nil
}
func (s *wireGuardExecutorStub) EnableWireGuardInterface(context.Context, string) error  { return nil }
func (s *wireGuardExecutorStub) DisableWireGuardInterface(context.Context, string) error { return nil }
func (s *wireGuardExecutorStub) ValidateSingboxConfig(context.Context, []byte) error     { return nil }
func (s *wireGuardExecutorStub) GetWireGuardStats(context.Context) (map[string]core.PeerStats, error) {
	return nil, nil
}
func (s *wireGuardExecutorStub) CheckConnectivity(context.Context) error { return nil }
func (s *wireGuardExecutorStub) Close() error                            { return nil }
func (s *wireGuardExecutorStub) Dial(context.Context, string, string) (net.Conn, error) {
	return nil, nil
}

func TestInterfaceChanged_DetectsInterfaceFields(t *testing.T) {
	before := core.WireGuardInterface{PrivateKey: "k1", ListenPort: 51820, Address: "10.0.0.1/24"}

	if interfaceChanged(before, before) {
		t.Fatalf("interfaceChanged should be false for identical interface")
	}
	if !interfaceChanged(before, core.WireGuardInterface{PrivateKey: "k2", ListenPort: 51820, Address: "10.0.0.1/24"}) {
		t.Fatalf("interfaceChanged should detect private key change")
	}
	if !interfaceChanged(before, core.WireGuardInterface{PrivateKey: "k1", ListenPort: 51821, Address: "10.0.0.1/24"}) {
		t.Fatalf("interfaceChanged should detect listen port change")
	}
	if !interfaceChanged(before, core.WireGuardInterface{PrivateKey: "k1", ListenPort: 51820, Address: "10.0.1.1/24"}) {
		t.Fatalf("interfaceChanged should detect address change")
	}
	// Non-interface fields are out of scope for this phase's decision logic.
	if interfaceChanged(before, core.WireGuardInterface{PrivateKey: "k1", ListenPort: 51820, Address: "10.0.0.1/24", DNS: "1.1.1.1"}) {
		t.Fatalf("interfaceChanged should ignore non-phase fields")
	}
}

func TestWireGuardConfigForIface_IOHelpersUseNamedInterfacePath(t *testing.T) {
	dir := t.TempDir()
	wg1Path := filepath.Join(dir, "wg1.conf")
	stub := newWireGuardExecutorStub()
	stub.files[wg1Path] = []byte("[Interface]\nAddress = 10.0.1.1/24\nListenPort = 51821\n")

	cfg := &core.Config{
		EnableWireGuard:     true,
		WireGuardConfigPath: filepath.Join(dir, "wg0.conf"),
		WireGuardConfigDir:  dir,
	}
	server := NewServer(nil, cfg, stub)

	loaded, err := server.loadWireGuardConfigForIface(context.Background(), "wg1")
	if err != nil {
		t.Fatalf("loadWireGuardConfigForIface returned error: %v", err)
	}
	if loaded.Path != wg1Path {
		t.Fatalf("loaded.Path=%q, want %q", loaded.Path, wg1Path)
	}
	if stub.lastReadPath != wg1Path {
		t.Fatalf("ReadConfig path=%q, want %q", stub.lastReadPath, wg1Path)
	}

	loaded.Interface.ListenPort = 51822
	if err := server.saveWireGuardConfigForIface(context.Background(), loaded, "wg1"); err != nil {
		t.Fatalf("saveWireGuardConfigForIface returned error: %v", err)
	}
	if stub.lastWritePath != wg1Path {
		t.Fatalf("WriteConfig path=%q, want %q", stub.lastWritePath, wg1Path)
	}
	if len(stub.lastWriteContent) == 0 {
		t.Fatalf("expected non-empty written content")
	}
}

func TestSyncWireGuardConfigForIface_PeerOnlyUsesSync(t *testing.T) {
	stub := newWireGuardExecutorStub()
	cfg := &core.Config{EnableWireGuard: true, WireGuardConfigPath: "/etc/wireguard/wg0.conf"}
	server := NewServer(nil, cfg, stub)

	before := core.WireGuardInterface{PrivateKey: "k", ListenPort: 51820, Address: "10.0.0.1/24"}
	after := before
	wgCfg := &core.WireGuardConfig{Interface: after}

	ok := server.syncWireGuardConfigForIface(context.Background(), "wg0", before, after, wgCfg)
	if !ok {
		t.Fatalf("syncWireGuardConfigForIface returned false for peer-only case")
	}
	if stub.syncCalls != 1 {
		t.Fatalf("syncCalls=%d, want 1", stub.syncCalls)
	}
	if stub.restartCalls != 0 {
		t.Fatalf("restartCalls=%d, want 0", stub.restartCalls)
	}
}

func TestSyncWireGuardConfigForIface_InterfaceChangeUsesRestart(t *testing.T) {
	stub := newWireGuardExecutorStub()
	cfg := &core.Config{EnableWireGuard: true, WireGuardConfigPath: "/etc/wireguard/wg0.conf"}
	server := NewServer(nil, cfg, stub)

	before := core.WireGuardInterface{PrivateKey: "k", ListenPort: 51820, Address: "10.0.0.1/24"}
	after := core.WireGuardInterface{PrivateKey: "k", ListenPort: 51821, Address: "10.0.0.1/24"}
	wgCfg := &core.WireGuardConfig{Interface: after}

	ok := server.syncWireGuardConfigForIface(context.Background(), "wg0", before, after, wgCfg)
	if !ok {
		t.Fatalf("syncWireGuardConfigForIface returned false for interface-change case")
	}
	if stub.restartCalls != 1 {
		t.Fatalf("restartCalls=%d, want 1", stub.restartCalls)
	}
	if stub.syncCalls != 0 {
		t.Fatalf("syncCalls=%d, want 0", stub.syncCalls)
	}
}
