package core

import (
	"log"
	"os"
	"path/filepath"
	"strings"
)

func (c *Config) SetExecutor(exec SystemExecutor) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.executor = exec
}

func (c *Config) getExecutor() SystemExecutor {
	// c.mu.Lock() // Avoid lock here if possible or be careful with deadlocks if caller holds lock
	// Since executor is set once at startup, we might get away with no lock or atomic value,
	// but to be safe and simple, let's assume it's accessed where safe.
	// Actually, most Config methods lock c.mu. We cannot lock again.
	// We should just access c.executor since we are inside locked methods usually.
	return c.executor
}

// ApplyWireGuardTestModeDefaults keeps WireGuard test-mode artifacts inside the project/config directory.
// It prevents accidental writes under system paths such as /etc/wireguard.
func (c *Config) ApplyWireGuardTestModeDefaults() {
	if !c.WireGuardTestMode {
		return
	}

	baseDir := "."
	if cfgPath := strings.TrimSpace(c.ConfigPath); cfgPath != "" {
		baseDir = filepath.Dir(cfgPath)
	}
	if !filepath.IsAbs(baseDir) {
		if abs, err := filepath.Abs(baseDir); err == nil {
			baseDir = abs
		}
	}
	if baseDir == "" {
		if cwd, err := os.Getwd(); err == nil {
			baseDir = cwd
		}
	}
	baseDir = filepath.Clean(baseDir)

	fallbackDir := filepath.Join(baseDir, "data", "wg-test-mode", "wireguard")
	defaultDir := strings.TrimSpace(c.WireGuardConfigDir) == "" || filepath.Clean(c.WireGuardConfigDir) == "/etc/wireguard"
	defaultPath := strings.TrimSpace(c.WireGuardConfigPath) == "" || filepath.Clean(c.WireGuardConfigPath) == "/etc/wireguard/wg0.conf"

	if defaultDir {
		c.WireGuardConfigDir = fallbackDir
	}

	// Even if a custom value is present, keep test-mode writes inside project scope.
	if !isPathWithinBase(baseDir, c.WireGuardConfigDir) {
		log.Printf("wireguard test mode: overriding external dir %q -> %q", c.WireGuardConfigDir, fallbackDir)
		c.WireGuardConfigDir = fallbackDir
	}

	if defaultPath {
		c.WireGuardConfigPath = filepath.Join(c.WireGuardConfigDir, "wg0.conf")
	}

	if !isPathWithinBase(baseDir, c.WireGuardConfigPath) {
		safePath := filepath.Join(c.WireGuardConfigDir, "wg0.conf")
		log.Printf("wireguard test mode: overriding external config path %q -> %q", c.WireGuardConfigPath, safePath)
		c.WireGuardConfigPath = safePath
	}

	c.WireGuardConfigDir = filepath.Clean(c.WireGuardConfigDir)
	c.WireGuardConfigPath = filepath.Clean(c.WireGuardConfigPath)
}

func isPathWithinBase(baseDir, target string) bool {
	if strings.TrimSpace(target) == "" {
		return false
	}

	absTarget, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	absBase, err := filepath.Abs(baseDir)
	if err != nil {
		return false
	}

	rel, err := filepath.Rel(absBase, absTarget)
	if err != nil {
		return false
	}
	rel = filepath.Clean(rel)
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..")
}
