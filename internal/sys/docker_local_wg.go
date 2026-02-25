package sys

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/Ogstra/ogs-swg/internal/core"
)

// SyncWireGuard writes the config to a container-local temp file, then uses
// nsenter -n to run wg syncconf inside the host's network namespace.
// Because only the network namespace is entered, the container's filesystem
// (including /tmp) remains visible to the command.
func (e *DockerLocalExecutor) SyncWireGuard(ctx context.Context, interfaceName string, configContent []byte) error {
	tmpFile, err := os.CreateTemp("", "wg-sync-*.conf")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %v", err)
	}
	defer func() {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
	}()

	if _, err := tmpFile.Write(configContent); err != nil {
		return fmt.Errorf("failed to write temp file: %v", err)
	}
	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync temp file: %v", err)
	}

	cmd := exec.CommandContext(ctx, "nsenter", "-t", "1", "-n", "--",
		"wg", "syncconf", interfaceName, tmpFile.Name())
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("nsenter wg syncconf failed: %v, output: %s", err, string(output))
	}
	return nil
}

// GetWireGuardStats runs wg show all dump inside the host's network namespace
// and parses the output. Reuses parseWGDumpStats/parseWGTextStats from ssh_executor.go.
func (e *DockerLocalExecutor) GetWireGuardStats(ctx context.Context) (map[string]core.PeerStats, error) {
	out, err := exec.CommandContext(ctx, "nsenter", "-t", "1", "-n", "--", "wg", "show", "all", "dump").CombinedOutput()
	if err == nil {
		return parseWGDumpStats(out), nil
	}

	// Fallback: wg show (text format) for environments where dump is unavailable.
	textOut, textErr := exec.CommandContext(ctx, "nsenter", "-t", "1", "-n", "--", "wg", "show").CombinedOutput()
	if textErr != nil {
		return nil, fmt.Errorf("failed to execute wg show (dump=%v, text=%v)", err, textErr)
	}
	return parseWGTextStats(textOut), nil
}
