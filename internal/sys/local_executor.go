package sys

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// AllowedSysctlKeys defines the whitelist of sysctl keys that can be modified.
var AllowedSysctlKeys = map[string]bool{
	"net.ipv4.ip_forward":                true,
	"net.ipv6.conf.all.forwarding":       true,
	"net.core.default_qdisc":             true,
	"net.ipv4.tcp_congestion_control":    true,
	"net.ipv4.conf.all.accept_redirects": true,
	"net.ipv4.conf.all.send_redirects":   true,
	"net.ipv6.conf.all.accept_redirects": true,
}

type LocalExecutor struct{}

func NewLocalExecutor() *LocalExecutor {
	return &LocalExecutor{}
}

func (e *LocalExecutor) RestartService(ctx context.Context, name string) error {
	return runSystemCtl(ctx, "restart", name)
}

func (e *LocalExecutor) StartService(ctx context.Context, name string) error {
	return runSystemCtl(ctx, "start", name)
}

func (e *LocalExecutor) StopService(ctx context.Context, name string) error {
	return runSystemCtl(ctx, "stop", name)
}

func (e *LocalExecutor) IsServiceActive(ctx context.Context, name string) (bool, error) {
	unitName := resolveUnitName(name)
	cmd := exec.CommandContext(ctx, "systemctl", "is-active", unitName)
	if err := cmd.Run(); err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			// standard failure usually means inactive
			if exitError.ExitCode() != 0 {
				return false, nil
			}
		}
		return false, err
	}
	return true, nil
}

func (e *LocalExecutor) WriteConfig(ctx context.Context, path string, content []byte, fileMode os.FileMode) error {
	// Atomic write: write to temp file then rename
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, content, fileMode); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func (e *LocalExecutor) ReadConfig(ctx context.Context, path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (e *LocalExecutor) ApplySysctl(ctx context.Context, key, value string) error {
	if !AllowedSysctlKeys[key] {
		return fmt.Errorf("sysctl key '%s' is not in the whitelist", key)
	}
	cmd := exec.CommandContext(ctx, "sysctl", "-w", fmt.Sprintf("%s=%s", key, value))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to apply sysctl %s: %v, output: %s", key, err, string(output))
	}
	return nil
}

func (e *LocalExecutor) GetSysctl(ctx context.Context, key string) (string, error) {
	if !AllowedSysctlKeys[key] {
		return "", fmt.Errorf("sysctl key '%s' is not in the whitelist", key)
	}
	cmd := exec.CommandContext(ctx, "sysctl", "-n", key)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get sysctl %s: %v", key, err)
	}
	return strings.TrimSpace(string(output)), nil
}

func (e *LocalExecutor) ReadJournal(ctx context.Context, unit string, limit int) ([]string, error) {
	cmd := exec.CommandContext(ctx, "journalctl", "-u", unit, "-n", strconv.Itoa(limit), "--no-pager")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, analyzeJournalError(out, err)
	}
	return parseJournalOutput(out), nil
}

func (e *LocalExecutor) SearchJournal(ctx context.Context, unit, query string, limit int) ([]string, error) {
	// Using --grep might not be available on all systemd versions, falling back to basic grep logic if needed
	// But let's assume modern systemd for now or just fetch and filter.
	// Fetching and filtering is safer for compatibility.
	// We fetch slightly more to filter locally.
	fetchLimit := limit * 5
	if fetchLimit > 5000 {
		fetchLimit = 5000
	}

	cmd := exec.CommandContext(ctx, "journalctl", "-u", unit, "-n", strconv.Itoa(fetchLimit), "--no-pager")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, analyzeJournalError(out, err)
	}

	lines := parseJournalOutput(out)
	var filtered []string
	q := strings.ToLower(query)
	for i := len(lines) - 1; i >= 0 && len(filtered) < limit; i-- {
		if strings.Contains(strings.ToLower(lines[i]), q) {
			filtered = append(filtered, lines[i])
		}
	}
	// Reverse back to chronological order
	for i, j := 0, len(filtered)-1; i < j; i, j = i+1, j-1 {
		filtered[i], filtered[j] = filtered[j], filtered[i]
	}
	return filtered, nil
}

func (e *LocalExecutor) CheckConnectivity(ctx context.Context) error {
	return nil // Local is always connected
}

func (e *LocalExecutor) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, network, addr)
}

func (e *LocalExecutor) Close() error {
	return nil
}

// Helpers

func runSystemCtl(ctx context.Context, action, service string) error {
	unitName := resolveUnitName(service)
	cmd := exec.CommandContext(ctx, "systemctl", action, unitName)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl %s %s failed: %v, output: %s", action, unitName, err, string(output))
	}
	return nil
}

func resolveUnitName(service string) string {
	if service == "wireguard" {
		return "wg-quick@wg0"
	}
	return service
}

func analyzeJournalError(out []byte, err error) error {
	msg := strings.TrimSpace(string(out))
	if msg == "" || strings.Contains(strings.ToLower(msg), "no entries") {
		return nil // Not strictly an error, just empty
	}
	return err
}

func parseJournalOutput(out []byte) []string {
	data := strings.TrimSpace(string(out))
	if data == "" {
		return []string{}
	}
	return strings.Split(data, "\n")
}
