package sys

import (
	"context"
	"fmt"
	"strings"

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

	output, err := runViaSystemdRun(ctx, "systemctl", "--system", "restart", unit)
	if err != nil {
		return fmt.Errorf("host systemctl restart %s failed: %v, output: %s", unit, err, string(output))
	}
	return nil
}

// GetWireGuardStats runs WireGuard commands on the host via systemd-run and
// parses the output. If host execution is unavailable, return empty stats.
func (e *DockerLocalExecutor) GetWireGuardStats(ctx context.Context) (map[string]core.PeerStats, error) {
	out, err := runViaSystemdRun(ctx, "wg", "show", "all", "dump")
	if err == nil {
		return parseWGDumpStats(out), nil
	}

	textOut, textErr := runViaSystemdRun(ctx, "wg", "show")
	if textErr != nil {
		return map[string]core.PeerStats{}, nil
	}
	return parseWGTextStats(textOut), nil
}
