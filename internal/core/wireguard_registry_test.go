package core

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestWireGuardRegistry_DiscoverInterfaces_SortedAndFiltered(t *testing.T) {
	dir := t.TempDir()

	mustWriteWGFile(t, dir, "wg1.conf", "[Interface]\nAddress = 10.0.1.1/24\nListenPort = 51821\n")
	mustWriteWGFile(t, dir, "wg0.conf", "[Interface]\nAddress = 10.0.0.1/24\nListenPort = 51820\n")
	mustWriteWGFile(t, dir, "bad.conf", "[Peer]\nPublicKey = abc\n")
	mustWriteWGFile(t, dir, "wg.bad.conf", "[Interface]\nAddress = 10.0.2.1/24\n")

	registry := WireGuardRegistry{}
	got, err := registry.DiscoverInterfaces(dir)
	if err != nil {
		t.Fatalf("DiscoverInterfaces returned error: %v", err)
	}

	want := []string{"wg0", "wg1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DiscoverInterfaces=%v, want %v", got, want)
	}
}

func TestWireGuardRegistry_LoadInterface_Success(t *testing.T) {
	dir := t.TempDir()
	mustWriteWGFile(t, dir, "wg0.conf", "[Interface]\nAddress = 10.0.0.1/24\nListenPort = 51820\nPrivateKey = invalid-test-key\n")

	registry := WireGuardRegistry{}
	cfg, err := registry.LoadInterface(dir, "wg0")
	if err != nil {
		t.Fatalf("LoadInterface returned error: %v", err)
	}

	wantPath := filepath.Join(dir, "wg0.conf")
	if cfg.Path != wantPath {
		t.Fatalf("cfg.Path=%q, want %q", cfg.Path, wantPath)
	}
	if cfg.Interface.ListenPort != 51820 {
		t.Fatalf("cfg.Interface.ListenPort=%d, want %d", cfg.Interface.ListenPort, 51820)
	}
	if cfg.Interface.Address != "10.0.0.1/24" {
		t.Fatalf("cfg.Interface.Address=%q, want %q", cfg.Interface.Address, "10.0.0.1/24")
	}
}

func TestWireGuardRegistry_LoadInterface_NotFound(t *testing.T) {
	registry := WireGuardRegistry{}
	_, err := registry.LoadInterface(t.TempDir(), "wg9")
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("LoadInterface error=%v, want os.ErrNotExist", err)
	}
}

func TestWireGuardRegistry_LoadInterface_RejectsInvalidName(t *testing.T) {
	registry := WireGuardRegistry{}
	_, err := registry.LoadInterface(t.TempDir(), "../etc/passwd")
	if err == nil {
		t.Fatal("LoadInterface expected validation error for invalid interface name")
	}
}

func TestWireGuardRegistry_LoadInterface_RejectsMissingInterfaceSection(t *testing.T) {
	dir := t.TempDir()
	mustWriteWGFile(t, dir, "wg0.conf", "[Peer]\nPublicKey = x\n")

	registry := WireGuardRegistry{}
	_, err := registry.LoadInterface(dir, "wg0")
	if err == nil {
		t.Fatal("LoadInterface expected error for file without [Interface] section")
	}
}

func mustWriteWGFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
