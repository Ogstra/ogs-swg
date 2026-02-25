package sys

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// ValidateSingboxConfig writes a temp file into the same directory as the
// sing-box config (which is bind-mounted, so the path is identical on the
// host). nsenter -m then enters the host's mount namespace where both the
// sing-box binary and the temp file are visible at the same path.
func (e *DockerLocalExecutor) ValidateSingboxConfig(ctx context.Context, content []byte) error {
	dir := filepath.Dir(e.config.SingboxConfigPath)
	tmpPath := filepath.Join(dir, fmt.Sprintf("ogs-validate-%d.json", time.Now().UnixNano()))

	if err := os.WriteFile(tmpPath, content, 0600); err != nil {
		return fmt.Errorf("failed to write validation temp file: %v", err)
	}
	defer os.Remove(tmpPath)

	cmd := exec.CommandContext(ctx, "nsenter", "-t", "1", "-m", "--", "sing-box", "check", "-c", tmpPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("invalid config: %s", string(output))
	}
	return nil
}
