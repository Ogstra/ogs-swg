package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_AutoDerivesWireGuardConfigDir(t *testing.T) {
	clearWireGuardEnv(t)

	cfgPath := writeConfigFixture(t, `{
  "wireguard_config_path": "/etc/wireguard/wg0.conf"
}`)

	cfg := LoadConfig(cfgPath)
	if cfg.WireGuardConfigDir != "/etc/wireguard" {
		t.Fatalf("WireGuardConfigDir=%q, want %q", cfg.WireGuardConfigDir, "/etc/wireguard")
	}
}

func TestLoadConfig_PreservesExplicitWireGuardConfigDir(t *testing.T) {
	clearWireGuardEnv(t)

	explicit := filepath.Clean("/custom/wireguard/../wireguard-config")
	cfgPath := writeConfigFixture(t, `{
  "wireguard_config_path": "/etc/wireguard/wg0.conf",
  "wireguard_config_dir": "/custom/wireguard/../wireguard-config"
}`)

	cfg := LoadConfig(cfgPath)
	if cfg.WireGuardConfigDir != explicit {
		t.Fatalf("WireGuardConfigDir=%q, want %q", cfg.WireGuardConfigDir, explicit)
	}
}

func clearWireGuardEnv(t *testing.T) {
	t.Helper()
	_ = os.Unsetenv("OGS_WIREGUARD_CONFIG_PATH")
	_ = os.Unsetenv("OGS_WIREGUARD_CONFIG_DIR")
}

func writeConfigFixture(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}
