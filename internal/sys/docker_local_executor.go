package sys

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/Ogstra/ogs-swg/internal/core"
)

// DockerLocalExecutor runs inside a Docker container co-located with the host's
// singbox and wg-quick systemd services. File operations delegate to LocalExecutor
// (bind mounts expose host paths at the same container paths). Host-level
// commands run via systemd-run through the host manager.
type DockerLocalExecutor struct {
	local  *LocalExecutor
	config *core.Config
	// hostNetworkMode disables loopback-to-gateway rewrite when the container
	// already shares the host network namespace (network_mode: host).
	hostNetworkMode bool

	singboxCheckMu          sync.Mutex
	singboxCheckBinary      string
	singboxCheckUnavailable bool

	hostGatewayMu       sync.Mutex
	hostGatewayAddr     string
	hostGatewayResolved bool
}

func NewDockerLocalExecutor(cfg *core.Config) *DockerLocalExecutor {
	return &DockerLocalExecutor{
		local:           NewLocalExecutor(),
		config:          cfg,
		hostNetworkMode: parseBoolLike(os.Getenv("OGS_DOCKER_LOCAL_HOST_NETWORK")),
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

func (e *DockerLocalExecutor) RestartWireGuard(ctx context.Context, interfaceName string) error {
	return e.runSystemctl(ctx, "restart", "wireguard", interfaceName)
}

func (e *DockerLocalExecutor) ListWireGuardInterfaces(ctx context.Context) ([]string, error) {
	out, err := runViaSystemdRun(
		ctx,
		"/bin/sh",
		"-lc",
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin; wg show interfaces",
	)
	if err != nil {
		return nil, fmt.Errorf("host wg show interfaces failed: %v, output: %s", err, string(out))
	}
	return parseWireGuardInterfaces(out), nil
}

func (e *DockerLocalExecutor) EnableWireGuardInterface(ctx context.Context, interfaceName string) error {
	return e.runSystemctl(ctx, "start", "wireguard", interfaceName)
}

func (e *DockerLocalExecutor) DisableWireGuardInterface(ctx context.Context, interfaceName string) error {
	return e.runSystemctl(ctx, "stop", "wireguard", interfaceName)
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
	target := addr
	if !e.hostNetworkMode {
		if host, port, err := net.SplitHostPort(addr); err == nil && isLoopbackHost(host) {
			if gw, gwErr := e.resolveHostGatewayAddr(); gwErr == nil && gw != "" {
				target = net.JoinHostPort(gw, port)
			}
		}
	}
	conn, err := e.local.Dial(ctx, network, target)
	if err == nil || target == addr {
		return conn, err
	}
	// In mixed deployments we may run with host networking but without the
	// explicit host-network flag yet. Retry original loopback address before
	// failing hard.
	if isRetryableDialErr(err) {
		return e.local.Dial(ctx, network, addr)
	}
	return nil, err
}

func (e *DockerLocalExecutor) Close() error {
	return nil
}

// helpers

func (e *DockerLocalExecutor) runSystemctl(ctx context.Context, action, name string, interfaceName ...string) error {
	unit := resolveUnitName(name, interfaceName...)
	args := []string{"--system", action}
	switch action {
	case "restart", "start", "stop":
		// Avoid blocking API requests on long-running unit jobs.
		args = append(args, "--no-block")
	}
	args = append(args, unit)

	output, err := runViaSystemdRun(ctx, "systemctl", args...)
	if err != nil {
		return fmt.Errorf("host systemctl %s %s failed: %v, output: %s", action, unit, err, string(output))
	}
	return nil
}

func runViaSystemdRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	runnerArgs := []string{"--wait", "--pipe", "--collect", "--quiet", name}
	runnerArgs = append(runnerArgs, args...)
	out, err := exec.CommandContext(ctx, "systemd-run", runnerArgs...).CombinedOutput()
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	return out, err
}

func isLoopbackHost(host string) bool {
	h := strings.TrimSpace(strings.Trim(host, "[]"))
	if h == "" || strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func (e *DockerLocalExecutor) resolveHostGatewayAddr() (string, error) {
	e.hostGatewayMu.Lock()
	defer e.hostGatewayMu.Unlock()

	if e.hostGatewayResolved {
		if e.hostGatewayAddr == "" {
			return "", fmt.Errorf("host gateway not found")
		}
		return e.hostGatewayAddr, nil
	}
	e.hostGatewayResolved = true

	data, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return "", err
	}

	lines := strings.Split(string(data), "\n")
	for _, ln := range lines[1:] { // skip header
		fields := strings.Fields(ln)
		if len(fields) < 3 {
			continue
		}
		if fields[1] != "00000000" { // default route
			continue
		}
		gwHex := fields[2]
		v, err := strconv.ParseUint(gwHex, 16, 32)
		if err != nil {
			continue
		}
		ip := net.IPv4(byte(v), byte(v>>8), byte(v>>16), byte(v>>24)).String()
		if ip != "" && ip != "0.0.0.0" {
			e.hostGatewayAddr = ip
			return ip, nil
		}
	}
	return "", fmt.Errorf("default route gateway not found")
}

func parseBoolLike(v string) bool {
	s := strings.TrimSpace(strings.ToLower(v))
	switch s {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func isRetryableDialErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "no route to host") ||
		strings.Contains(msg, "network is unreachable") ||
		strings.Contains(msg, "i/o timeout")
}

func analyzeJournalError(out []byte, err error) error {
	if s := strings.TrimSpace(string(out)); s != "" {
		return fmt.Errorf("journalctl: %v: %s", err, s)
	}
	return fmt.Errorf("journalctl: %v", err)
}

func parseJournalOutput(out []byte) []string {
	var lines []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "-- ") {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}
