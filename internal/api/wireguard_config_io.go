package api

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/Ogstra/ogs-swg/internal/core"
)

func isNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	if os.IsNotExist(err) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not exist") || strings.Contains(msg, "no such file")
}

func (s *Server) loadWireGuardConfig(ctx context.Context) (*core.WireGuardConfig, error) {
	return s.loadWireGuardConfigAtPath(ctx, s.config.WireGuardConfigPath)
}

func (s *Server) loadWireGuardConfigForIface(ctx context.Context, iface string) (*core.WireGuardConfig, error) {
	path, err := s.wireGuardConfigPathForIface(iface)
	if err != nil {
		return nil, err
	}
	return s.loadWireGuardConfigAtPath(ctx, path)
}

func (s *Server) loadWireGuardConfigAtPath(ctx context.Context, path string) (*core.WireGuardConfig, error) {
	if s.executor == nil {
		return core.LoadWireGuardConfig(path)
	}

	content, err := s.executor.ReadConfig(ctx, path)
	if err != nil {
		if isNotFoundErr(err) {
			return &core.WireGuardConfig{Path: path}, nil
		}
		return nil, err
	}

	tmpFile, err := os.CreateTemp("", "wg-read-*.conf")
	if err != nil {
		return nil, err
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.Write(content); err != nil {
		tmpFile.Close()
		_ = os.Remove(tmpPath)
		return nil, err
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return nil, err
	}
	defer os.Remove(tmpPath)

	cfg, err := core.LoadWireGuardConfig(tmpPath)
	if err != nil {
		return nil, err
	}
	cfg.Path = path
	return cfg, nil
}

func (s *Server) saveWireGuardConfig(ctx context.Context, cfg *core.WireGuardConfig) error {
	return s.saveWireGuardConfigAtPath(ctx, s.config.WireGuardConfigPath, cfg)
}

func (s *Server) saveWireGuardConfigForIface(ctx context.Context, cfg *core.WireGuardConfig, iface string) error {
	path, err := s.wireGuardConfigPathForIface(iface)
	if err != nil {
		return err
	}
	return s.saveWireGuardConfigAtPath(ctx, path, cfg)
}

func (s *Server) saveWireGuardConfigAtPath(ctx context.Context, path string, cfg *core.WireGuardConfig) error {
	if cfg == nil {
		return nil
	}

	content, err := serializeWireGuardConfig(cfg)
	if err != nil {
		return err
	}

	if s.executor != nil {
		return s.executor.WriteConfig(ctx, path, content, 0644)
	}
	return os.WriteFile(path, content, 0644)
}

func serializeWireGuardConfig(cfg *core.WireGuardConfig) ([]byte, error) {
	tmpFile, err := os.CreateTemp("", "wg-write-*.conf")
	if err != nil {
		return nil, err
	}
	tmpPath := tmpFile.Name()
	_ = tmpFile.Close()
	defer os.Remove(tmpPath)

	clone := *cfg
	clone.Path = tmpPath
	if err := clone.Save(); err != nil {
		return nil, err
	}
	return os.ReadFile(tmpPath)
}

func mutateWireGuardConfig(cfg *core.WireGuardConfig, mutate func(*core.WireGuardConfig) error) error {
	if cfg == nil {
		return nil
	}

	tmpFile, err := os.CreateTemp("", "wg-mutate-*.conf")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	_ = tmpFile.Close()
	defer os.Remove(tmpPath)

	origPath := cfg.Path
	cfg.Path = tmpPath
	defer func() {
		cfg.Path = origPath
	}()

	return mutate(cfg)
}

func (s *Server) wireGuardConfigPathForIface(iface string) (string, error) {
	name := strings.TrimSpace(iface)
	if name == "" {
		name = defaultWireGuardInterfaceName(s.config.WireGuardConfigPath)
	}

	dir := strings.TrimSpace(s.config.WireGuardConfigDir)
	if dir == "" {
		basePath := strings.TrimSpace(s.config.WireGuardConfigPath)
		if basePath == "" {
			basePath = "/etc/wireguard/wg0.conf"
		}
		dir = filepath.Dir(basePath)
	}

	var registry core.WireGuardRegistry
	return registry.InterfacePath(dir, name)
}

func defaultWireGuardInterfaceName(configPath string) string {
	iface := strings.TrimSpace(strings.TrimSuffix(filepath.Base(configPath), filepath.Ext(configPath)))
	if iface == "" {
		return "wg0"
	}
	return iface
}
