package sys

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/Ogstra/ogs-swg/internal/core"
)

// DockerLocalExecutor runs inside a Docker container co-located with the host's
// singbox and wg-quick systemd services. File operations delegate to LocalExecutor
// (bind mounts expose host paths at the same container paths). Commands that
// require host namespaces are wrapped with nsenter -t 1.
type DockerLocalExecutor struct {
	local  *LocalExecutor
	config *core.Config
}

func NewDockerLocalExecutor(cfg *core.Config) *DockerLocalExecutor {
	return &DockerLocalExecutor{
		local:  NewLocalExecutor(),
		config: cfg,
	}
}

// File operations: bind mounts make host paths identical inside the container.

func (e *DockerLocalExecutor) WriteConfig(ctx context.Context, path string, content []byte, fileMode os.FileMode) error {
	return e.local.WriteConfig(ctx, path, content, fileMode)
}

func (e *DockerLocalExecutor) ReadConfig(ctx context.Context, path string) ([]byte, error) {
	return e.local.ReadConfig(ctx, path)
}

// Service management: enter host mount + UTS + network namespaces so systemctl
// can reach the host's systemd socket.

func (e *DockerLocalExecutor) RestartService(ctx context.Context, name string) error {
	return e.runNsenterSystemctl(ctx, "restart", name)
}

func (e *DockerLocalExecutor) StartService(ctx context.Context, name string) error {
	return e.runNsenterSystemctl(ctx, "start", name)
}

func (e *DockerLocalExecutor) StopService(ctx context.Context, name string) error {
	return e.runNsenterSystemctl(ctx, "stop", name)
}

func (e *DockerLocalExecutor) IsServiceActive(ctx context.Context, name string) (bool, error) {
	unit := resolveUnitName(name)
	cmd := exec.CommandContext(ctx, "nsenter", "-t", "1", "-m", "-u", "-n", "--", "systemctl", "is-active", unit)
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() != 0 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Sysctl: enter host mount namespace to reach /proc/sys on the host.

func (e *DockerLocalExecutor) ApplySysctl(ctx context.Context, key, value string) error {
	if !AllowedSysctlKeys[key] {
		return fmt.Errorf("sysctl key '%s' is not in the whitelist", key)
	}
	cmd := exec.CommandContext(ctx, "nsenter", "-t", "1", "-m", "--", "sysctl", "-w", fmt.Sprintf("%s=%s", key, value))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to apply sysctl %s: %v, output: %s", key, err, string(output))
	}
	return nil
}

func (e *DockerLocalExecutor) GetSysctl(ctx context.Context, key string) (string, error) {
	if !AllowedSysctlKeys[key] {
		return "", fmt.Errorf("sysctl key '%s' is not in the whitelist", key)
	}
	cmd := exec.CommandContext(ctx, "nsenter", "-t", "1", "-m", "--", "sysctl", "-n", key)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get sysctl %s: %v", key, err)
	}
	return strings.TrimSpace(string(output)), nil
}

// Journal: enter host mount + UTS namespaces to reach the host's journal files.

func (e *DockerLocalExecutor) ReadJournal(ctx context.Context, unit string, limit int) ([]string, error) {
	cmd := exec.CommandContext(ctx, "nsenter", "-t", "1", "-m", "-u", "--",
		"journalctl", "-u", unit, "-n", strconv.Itoa(limit), "--no-pager")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, analyzeJournalError(out, err)
	}
	return parseJournalOutput(out), nil
}

func (e *DockerLocalExecutor) SearchJournal(ctx context.Context, unit, query string, limit int) ([]string, error) {
	fetchLimit := limit * 5
	if fetchLimit > 5000 {
		fetchLimit = 5000
	}
	cmd := exec.CommandContext(ctx, "nsenter", "-t", "1", "-m", "-u", "--",
		"journalctl", "-u", unit, "-n", strconv.Itoa(fetchLimit), "--no-pager")
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
	for i, j := 0, len(filtered)-1; i < j; i, j = i+1, j-1 {
		filtered[i], filtered[j] = filtered[j], filtered[i]
	}
	return filtered, nil
}

// Connectivity: same host, always reachable.

func (e *DockerLocalExecutor) CheckConnectivity(ctx context.Context) error {
	return nil
}

func (e *DockerLocalExecutor) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	return e.local.Dial(ctx, network, addr)
}

func (e *DockerLocalExecutor) Close() error {
	return nil
}

// helpers

func (e *DockerLocalExecutor) runNsenterSystemctl(ctx context.Context, action, name string) error {
	unit := resolveUnitName(name)
	cmd := exec.CommandContext(ctx, "nsenter", "-t", "1", "-m", "-u", "-n", "--", "systemctl", action, unit)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("nsenter systemctl %s %s failed: %v, output: %s", action, unit, err, string(output))
	}
	return nil
}
