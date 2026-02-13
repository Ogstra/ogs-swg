package sys

import (
	"context"
	"fmt"
	"time"
)

func (e *SSHExecutor) ValidateSingboxConfig(ctx context.Context, content []byte) error {
	if err := e.ensureConnection(ctx); err != nil {
		return err
	}

	// Remote temp file
	// We can use a randomized name in /tmp
	randName := fmt.Sprintf("/tmp/singbox_check_%d.json", time.Now().UnixNano())

	// Write via SFTP
	f, err := e.sftp.Create(randName)
	if err != nil {
		return fmt.Errorf("remote create failed: %w", err)
	}
	if _, err := f.Write(content); err != nil {
		f.Close()
		return fmt.Errorf("remote write failed: %w", err)
	}
	f.Close()

	// Cleanup
	defer e.sftp.Remove(randName)

	// Run check
	cmd := fmt.Sprintf("sing-box check -c %s", randName)
	out, err := e.runCommand(ctx, cmd)
	if err != nil {
		return fmt.Errorf("remote validation failed: %s", string(out))
	}
	return nil
}
