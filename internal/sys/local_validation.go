package sys

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

func (e *LocalExecutor) ValidateSingboxConfig(ctx context.Context, content []byte) error {
	tmpFile, err := os.CreateTemp("", "singbox_check_*.json")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(content); err != nil {
		return fmt.Errorf("failed to write temp file: %v", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %v", err)
	}

	cmd := exec.CommandContext(ctx, "sing-box", "check", "-c", tmpFile.Name())
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("invalid config: %s", string(output))
	}
	return nil
}
