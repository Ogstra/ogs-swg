package sys

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ValidateSingboxConfig writes a temp file into the same directory as the
// sing-box config (which is bind-mounted and visible on the host), then asks
// host systemd to run `sing-box check` through systemd-run.
func (e *DockerLocalExecutor) ValidateSingboxConfig(ctx context.Context, content []byte) error {
	dir := filepath.Dir(e.config.SingboxConfigPath)
	tmpPath := filepath.Join(dir, fmt.Sprintf("ogs-validate-%d.json", time.Now().UnixNano()))

	if err := os.WriteFile(tmpPath, content, 0600); err != nil {
		return fmt.Errorf("failed to write validation temp file: %v", err)
	}
	defer os.Remove(tmpPath)

	output, err := runViaSystemdRun(ctx, "sing-box", "check", "-c", tmpPath)
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if msg == "" {
			return fmt.Errorf("failed to run host sing-box check: %v", err)
		}
		return fmt.Errorf("invalid config: %s", msg)
	}
	return nil
}
