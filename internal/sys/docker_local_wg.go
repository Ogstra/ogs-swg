package sys

import (
	"context"
	"fmt"
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
	statsCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := runViaSystemdRun(statsCtx, "/bin/sh", "-lc", "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin; wg show all dump")
	if err == nil {
		return parseWGDumpStats(out), nil
	}

	textCtx, textCancel := context.WithTimeout(ctx, 3*time.Second)
	defer textCancel()
	textOut, textErr := runViaSystemdRun(textCtx, "/bin/sh", "-lc", "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin; wg show")
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
