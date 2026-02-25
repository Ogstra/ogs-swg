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
// (bind mounts expose host paths at the same container paths). Host-level
// commands run via systemd-run through the host manager.
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

// Service management: run via host systemd manager (systemd-run).

func (e *DockerLocalExecutor) RestartService(ctx context.Context, name string) error {
	return e.runSystemctl(ctx, "restart", name)
}

func (e *DockerLocalExecutor) StartService(ctx context.Context, name string) error {
	return e.runSystemctl(ctx, "start", name)
}

func (e *DockerLocalExecutor) StopService(ctx context.Context, name string) error {
	return e.runSystemctl(ctx, "stop", name)
}

func (e *DockerLocalExecutor) IsServiceActive(ctx context.Context, name string) (bool, error) {
	unit := resolveUnitName(name)
	out, err := runViaSystemdRun(ctx, "systemctl", "--system", "is-active", "--quiet", unit)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			// systemctl is-active exits 0=active, 3=inactive/failed, 4=not found.
			// Any other non-zero code (e.g. 1) means systemctl/systemd-run failed.
			code := exitErr.ExitCode()
			if code == 3 || code == 4 {
				return false, nil // service is inactive or unit not found — not an error
			}
			// code 1 (or other unexpected): command printed an error; surface it.
			return false, fmt.Errorf("host systemctl is-active %s (exit %d): %s", unit, code, strings.TrimSpace(string(out)))
		}
		return false, err
	}
	return true, nil
}

// Sysctl: execute on host via systemd-run so /proc/sys is host-scoped.

func (e *DockerLocalExecutor) ApplySysctl(ctx context.Context, key, value string) error {
	if !AllowedSysctlKeys[key] {
		return fmt.Errorf("sysctl key '%s' is not in the whitelist", key)
	}
	output, err := runViaSystemdRun(ctx, "sysctl", "-w", fmt.Sprintf("%s=%s", key, value))
	if err != nil {
		return fmt.Errorf("failed to apply sysctl %s: %v, output: %s", key, err, string(output))
	}
	return nil
}

func (e *DockerLocalExecutor) GetSysctl(ctx context.Context, key string) (string, error) {
	if !AllowedSysctlKeys[key] {
		return "", fmt.Errorf("sysctl key '%s' is not in the whitelist", key)
	}
	output, err := runViaSystemdRun(ctx, "sysctl", "-n", key)
	if err != nil {
		return "", fmt.Errorf("failed to get sysctl %s: %v", key, err)
	}
	return strings.TrimSpace(string(output)), nil
}

// Journal: query from host context via systemd-run.

func (e *DockerLocalExecutor) ReadJournal(ctx context.Context, unit string, limit int) ([]string, error) {
	out, err := runViaSystemdRun(ctx, "journalctl", "--system", "-u", unit, "-n", strconv.Itoa(limit), "--no-pager")
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
	out, err := runViaSystemdRun(ctx, "journalctl", "--system", "-u", unit, "-n", strconv.Itoa(fetchLimit), "--no-pager")
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

func (e *DockerLocalExecutor) runSystemctl(ctx context.Context, action, name string) error {
	unit := resolveUnitName(name)
	output, err := runViaSystemdRun(ctx, "systemctl", "--system", action, unit)
	if err != nil {
		return fmt.Errorf("host systemctl %s %s failed: %v, output: %s", action, unit, err, string(output))
	}
	return nil
}

func runViaSystemdRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	runnerArgs := []string{"--wait", "--pipe", "--collect", "--quiet", name}
	runnerArgs = append(runnerArgs, args...)
	return exec.CommandContext(ctx, "systemd-run", runnerArgs...).CombinedOutput()
}
