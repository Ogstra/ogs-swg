package sys

import (
	"context"
	"fmt"
	"time"
)

func (e *SSHExecutor) SyncWireGuard(ctx context.Context, interfaceName string, configContent []byte) error {
	if err := e.ensureConnection(ctx); err != nil {
		return err
	}

	// Remote temp file
	randName := fmt.Sprintf("/tmp/wg-sync-%d.conf", time.Now().UnixNano())

	// Write via SFTP
	f, err := e.sftp.Create(randName)
	if err != nil {
		return fmt.Errorf("remote create failed: %w", err)
	}
	if _, err := f.Write(configContent); err != nil {
		f.Close()
		return fmt.Errorf("remote write failed: %w", err)
	}
	f.Close()
	defer e.sftp.Remove(randName)

	// Safe Reload Logic:
	// 1. Schedule rollback (system reboot in 5 mins)
	// 2. Apply config
	// 3. Check connectivity
	// 4. Cancel rollback

	// Schedule rollback
	rollbackCmd := "sudo shutdown -r +5 'WireGuard config change rollback due to connectivity loss'"
	if _, err := e.runCommand(ctx, rollbackCmd); err != nil {
		// If shutdown command fails, abort. Safety first.
		return fmt.Errorf("failed to schedule rollback: %v", err)
	}

	// Apply config
	// Using sudo for wg syncconf
	cmd := fmt.Sprintf("sudo wg syncconf %s %s", interfaceName, randName)
	out, err := e.runCommand(ctx, cmd)
	if err != nil {
		// Try to cancel rollback immediately since applying failed
		e.runCommand(ctx, "sudo shutdown -c")
		return fmt.Errorf("remote wg syncconf failed: %v, output: %s", err, string(out))
	}

	// Check connectivity
	// We might lose connection here if config broke routing.
	// We try a simple command.
	if err := e.ensureConnection(ctx); err != nil {
		// Connection lost. Let rollback happen.
		// Return error indicating potential lockout
		return fmt.Errorf("connection lost after applying config; rollback pending in 5m: %v", err)
	}

	// Confirm success, cancel rollback
	if _, err := e.runCommand(ctx, "sudo shutdown -c"); err != nil {
		return fmt.Errorf("config applied but failed to cancel rollback: %v", err)
	}

	return nil
}
