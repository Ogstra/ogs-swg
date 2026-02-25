package sys

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
)

// SyncWireGuard assumes config content has already been persisted by the caller.
// In docker_local mode we reload wg-quick for the target interface through host
// systemd (via the host D-Bus bind mount).
func (e *DockerLocalExecutor) SyncWireGuard(ctx context.Context, interfaceName string, configContent []byte) error {
	_ = configContent

	iface := strings.TrimSpace(strings.TrimSuffix(interfaceName, ".conf"))
	if iface == "" {
		iface = "wg0"
	}
	unit := fmt.Sprintf("wg-quick@%s", iface)

	restartCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	output, err := runViaSystemdRun(restartCtx, "systemctl", "--system", "restart", "--no-block", unit)
	if err != nil {
		return fmt.Errorf("host systemctl restart %s failed: %v, output: %s", unit, err, string(output))
	}
	return nil
}

// GetWireGuardStats runs WireGuard commands on the host via systemd-run and
// parses the output. If host execution is unavailable, return empty stats.
func (e *DockerLocalExecutor) GetWireGuardStats(ctx context.Context) (map[string]core.PeerStats, error) {
	wgBin, err := e.resolveWGBinary(ctx)
	if err != nil {
		return nil, err
	}

	statsCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := runViaSystemdRun(statsCtx, wgBin, "show", "all", "dump")
	if err == nil {
		return parseWGDumpStats(out), nil
	}

	textCtx, textCancel := context.WithTimeout(ctx, 3*time.Second)
	defer textCancel()
	textOut, textErr := runViaSystemdRun(textCtx, wgBin, "show")
	if textErr != nil {
		return nil, fmt.Errorf(
			"failed to execute host wg show (dump err: %v, dump out: %s; text err: %v, text out: %s)",
			err,
			strings.TrimSpace(string(out)),
			textErr,
			strings.TrimSpace(string(textOut)),
		)
	}
	return parseWGTextStats(textOut), nil
}

func (e *DockerLocalExecutor) resolveWGBinary(ctx context.Context) (string, error) {
	e.wgBinaryMu.Lock()
	path := e.wgBinaryPath
	e.wgBinaryMu.Unlock()
	if path != "" {
		return path, nil
	}

	candidates := []string{
		strings.TrimSpace(os.Getenv("OGS_WG_BINARY_PATH")),
		"/usr/bin/wg",
		"/usr/local/bin/wg",
		"/usr/sbin/wg",
		"/sbin/wg",
		"/bin/wg",
		"wg",
	}
	if out, err := runViaSystemdRun(ctx, "/bin/sh", "-lc", "command -v wg || true"); err == nil {
		if v := strings.TrimSpace(string(out)); v != "" {
			candidates = append([]string{v}, candidates...)
		}
	}

	var lastMsg string
	for _, bin := range candidates {
		if bin == "" {
			continue
		}
		probeCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
		out, err := runViaSystemdRun(probeCtx, bin, "--version")
		cancel()
		if err == nil {
			e.wgBinaryMu.Lock()
			e.wgBinaryPath = bin
			e.wgBinaryMu.Unlock()
			return bin, nil
		}
		lastMsg = strings.TrimSpace(string(out))
		if lastMsg == "" {
			lastMsg = err.Error()
		}
		if !isMissingExecutableMsg(lastMsg) {
			e.wgBinaryMu.Lock()
			e.wgBinaryPath = bin
			e.wgBinaryMu.Unlock()
			return bin, nil
		}
	}

	return "", fmt.Errorf("wg binary not found on host (tried: %s). last error: %s", strings.Join(candidates, ", "), lastMsg)
}
