package sys

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
)

// SyncWireGuard assumes config content has already been persisted by the caller.
// In docker_local mode peer-only updates are applied through wg syncconf on the host.
func (e *DockerLocalExecutor) SyncWireGuard(ctx context.Context, interfaceName string, configContent []byte) error {
	if len(configContent) == 0 {
		return fmt.Errorf("wireguard syncconf content is empty")
	}
	iface := normalizeWireGuardInterfaceName(interfaceName)
	encoded := base64.StdEncoding.EncodeToString(configContent)
	script := fmt.Sprintf(
		"set -eu; umask 077; tmp=$(mktemp /tmp/wg-sync-XXXXXX.conf); trap 'rm -f \"$tmp\"' EXIT; printf '%%s' '%s' | base64 -d > \"$tmp\"; wg syncconf %s \"$tmp\"",
		encoded,
		iface,
	)

	restartCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	output, err := runViaSystemdRun(restartCtx, "/bin/sh", "-lc", script)
	if err != nil {
		return fmt.Errorf("host wg syncconf %s failed: %v, output: %s", iface, err, string(output))
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
