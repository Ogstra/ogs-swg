package core

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var wireGuardInterfaceNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,15}$`)

type WireGuardRegistry struct{}

func (WireGuardRegistry) InterfacePath(dir, name string) (string, error) {
	cleanDir := filepath.Clean(strings.TrimSpace(dir))
	if cleanDir == "" || cleanDir == "." {
		return "", errors.New("wireguard config directory is required")
	}

	iface := strings.TrimSpace(name)
	if !wireGuardInterfaceNamePattern.MatchString(iface) {
		return "", fmt.Errorf("invalid wireguard interface name: %q", name)
	}

	return filepath.Join(cleanDir, iface+".conf"), nil
}

func (r WireGuardRegistry) DiscoverInterfaces(dir string) ([]string, error) {
	cleanDir := filepath.Clean(strings.TrimSpace(dir))
	if cleanDir == "" || cleanDir == "." {
		return nil, errors.New("wireguard config directory is required")
	}

	matches, err := filepath.Glob(filepath.Join(cleanDir, "*.conf"))
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, p := range matches {
		base := filepath.Base(p)
		name := strings.TrimSuffix(base, filepath.Ext(base))
		if !wireGuardInterfaceNamePattern.MatchString(name) {
			continue
		}
		hasInterface, err := hasInterfaceHeader(p)
		if err != nil || !hasInterface {
			continue
		}
		if _, err := LoadWireGuardConfig(p); err != nil {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}

	sort.Strings(names)
	return names, nil
}

func (r WireGuardRegistry) LoadInterface(dir, name string) (*WireGuardConfig, error) {
	path, err := r.InterfacePath(dir, name)
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(path); err != nil {
		return nil, err
	}

	hasInterface, err := hasInterfaceHeader(path)
	if err != nil {
		return nil, err
	}
	if !hasInterface {
		return nil, fmt.Errorf("wireguard config %s is missing [Interface] section", path)
	}

	return LoadWireGuardConfig(path)
}

func hasInterfaceHeader(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.EqualFold(line, "[Interface]") {
			return true, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return false, err
	}
	return false, nil
}
