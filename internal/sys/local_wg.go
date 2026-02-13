package sys

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

func (e *LocalExecutor) SyncWireGuard(ctx context.Context, interfaceName string, configContent []byte) error {
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
	// Important: Sync to disk before passing to wg command
	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync temp file: %v", err)
	}

	cmd := exec.CommandContext(ctx, "wg", "syncconf", interfaceName, tmpFile.Name())
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("wg syncconf failed: %v, output: %s", err, string(output))
	}
	return nil
}
