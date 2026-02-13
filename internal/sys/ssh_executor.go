package sys

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// SSHExecutor implements SystemExecutor for remote hosts via SSH.
type SSHExecutor struct {
	config *core.Config
	client *ssh.Client
	sftp   *sftp.Client
	mu     sync.Mutex
}

func NewSSHExecutor(cfg *core.Config) *SSHExecutor {
	return &SSHExecutor{
		config: cfg,
	}
}

// ensureConnection ensures that we have an active SSH and SFTP connection.
// It uses a mutex to prevent race conditions during reconnection.
func (e *SSHExecutor) ensureConnection(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.client != nil {
		// Check if connection is alive
		_, _, err := e.client.SendRequest("keepalive@openssh.com", true, nil)
		if err == nil {
			if e.sftp == nil {
				// SFTP might have died or not initialized
				sftpClient, err := sftp.NewClient(e.client)
				if err != nil {
					return fmt.Errorf("failed to recreate SFTP client: %w", err)
				}
				e.sftp = sftpClient
			}
			return nil
		}
		// Connection dead, close and reconnect
		e.client.Close()
		e.client = nil
		if e.sftp != nil {
			e.sftp.Close()
			e.sftp = nil
		}
	}

	// Connect
	signer, err := parsePrivateKey(e.config.SSHKeyPath, e.config.SSHKeyPassphrase)
	if err != nil {
		return fmt.Errorf("failed to parse private key: %w", err)
	}

	sshConfig := &ssh.ClientConfig{
		User: e.config.SSHUser,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // TODO: Implement known_hosts verification in production
		Timeout:         10 * time.Second,
	}

	addr := fmt.Sprintf("%s:%d", e.config.SSHHost, e.config.SSHPort)
	client, err := ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		return fmt.Errorf("failed to dial SSH %s: %w", addr, err)
	}

	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		client.Close()
		return fmt.Errorf("failed to create SFTP client: %w", err)
	}

	e.client = client
	e.sftp = sftpClient
	return nil
}

func parsePrivateKey(keyPath, passphrase string) (ssh.Signer, error) {
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, err
	}
	if passphrase != "" {
		return ssh.ParsePrivateKeyWithPassphrase(key, []byte(passphrase))
	}
	return ssh.ParsePrivateKey(key)
}

func (e *SSHExecutor) runCommand(ctx context.Context, cmdStr string) ([]byte, error) {
	if err := e.ensureConnection(ctx); err != nil {
		return nil, err
	}

	// We use the mutex broadly for now, but strictly we could lock only ensureConnection.
	// However, concurrent sessions on one client are fine.
	session, err := e.client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("failed to create SSH session: %w", err)
	}
	defer session.Close()

	// Handle context cancellation
	go func() {
		<-ctx.Done()
		session.Close()
	}()

	output, err := session.CombinedOutput(cmdStr)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return output, err
}

// Service Management (wrappers around systemctl)

func (e *SSHExecutor) RestartService(ctx context.Context, name string) error {
	unit := resolveUnitName(name)
	// Safe Reload Logic: Not strictly implemented here yet, assuming atomic config writes handled risk.
	// Implementing rollback requires more state (previous config).
	// For now, standard restart.
	_, err := e.runCommand(ctx, fmt.Sprintf("sudo systemctl restart %s", unit))
	return err
}

func (e *SSHExecutor) StartService(ctx context.Context, name string) error {
	unit := resolveUnitName(name)
	_, err := e.runCommand(ctx, fmt.Sprintf("sudo systemctl start %s", unit))
	return err
}

func (e *SSHExecutor) StopService(ctx context.Context, name string) error {
	unit := resolveUnitName(name)
	_, err := e.runCommand(ctx, fmt.Sprintf("sudo systemctl stop %s", unit))
	return err
}

func (e *SSHExecutor) IsServiceActive(ctx context.Context, name string) (bool, error) {
	unit := resolveUnitName(name)
	// systemctl is-active returns 0 if active, non-zero otherwise
	_, err := e.runCommand(ctx, fmt.Sprintf("sudo systemctl is-active %s", unit))
	if err != nil {
		// Differentiate between connection error and "inactive" exit code
		if _, ok := err.(*ssh.ExitError); ok {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// File Management (SFTP)

func (e *SSHExecutor) WriteConfig(ctx context.Context, path string, content []byte, fileMode os.FileMode) error {
	if err := e.ensureConnection(ctx); err != nil {
		return err
	}

	// Write to temp file
	tmpPath := path + ".tmp"
	f, err := e.sftp.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("sftp create failed: %w", err)
	}
	if _, err := f.Write(content); err != nil {
		f.Close()
		return fmt.Errorf("sftp write failed: %w", err)
	}
	f.Chmod(fileMode)
	f.Close()

	// Atomic Rename
	if err := e.sftp.PosixRename(tmpPath, path); err != nil {
		// Rename failed, maybe target exists? Try standard Rename
		if rErr := e.sftp.Rename(tmpPath, path); rErr != nil {
			return fmt.Errorf("sftp rename failed: %w", rErr)
		}
	}
	return nil
}

func (e *SSHExecutor) ReadConfig(ctx context.Context, path string) ([]byte, error) {
	if err := e.ensureConnection(ctx); err != nil {
		return nil, err
	}

	f, err := e.sftp.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return io.ReadAll(f)
}

// Sysctl Management (Whitelist enforcement still applies)

func (e *SSHExecutor) ApplySysctl(ctx context.Context, key, value string) error {
	if !AllowedSysctlKeys[key] {
		return fmt.Errorf("sysctl key '%s' is not in the whitelist", key)
	}
	// Using sudo for sysctl
	cmd := fmt.Sprintf("sudo sysctl -w %s='%s'", key, value)
	out, err := e.runCommand(ctx, cmd)
	if err != nil {
		return fmt.Errorf("remote sysctl failed: %v, output: %s", err, string(out))
	}
	return nil
}

func (e *SSHExecutor) GetSysctl(ctx context.Context, key string) (string, error) {
	if !AllowedSysctlKeys[key] {
		return "", fmt.Errorf("sysctl key '%s' is not in the whitelist", key)
	}
	cmd := fmt.Sprintf("sudo sysctl -n %s", key)
	out, err := e.runCommand(ctx, cmd)
	if err != nil {
		return "", err
	}
	return string(bytes.TrimSpace(out)), nil
}

// Logs

func (e *SSHExecutor) ReadJournal(ctx context.Context, unit string, limit int) ([]string, error) {
	cmd := fmt.Sprintf("sudo journalctl -u %s -n %d --no-pager", unit, limit)
	out, err := e.runCommand(ctx, cmd)
	if err != nil {
		return nil, analyzeJournalError(out, err)
	}
	return parseJournalOutput(out), nil
}

func (e *SSHExecutor) SearchJournal(ctx context.Context, unit, query string, limit int) ([]string, error) {
	// Remote grep using pipe or journalctl grep if available.
	// Safer to fetch and filter if we want consistency with LocalExecutor,
	// but strictly for performance on remote, server-side filtering is better.
	// Let's rely on client-side filtering (fetch recent N lines) to avoid shell injection risks in query.
	fetchLimit := limit * 5
	if fetchLimit > 5000 {
		fetchLimit = 5000
	}
	cmd := fmt.Sprintf("sudo journalctl -u %s -n %d --no-pager", unit, fetchLimit)
	out, err := e.runCommand(ctx, cmd)
	if err != nil {
		return nil, analyzeJournalError(out, err)
	}

	lines := parseJournalOutput(out)
	var filtered []string
	q := query // already string, but local filtering ignores lower/upper case usually
	for i := len(lines) - 1; i >= 0 && len(filtered) < limit; i-- {
		if containsIgnoreCase(lines[i], q) {
			filtered = append(filtered, lines[i])
		}
	}
	// Reverse back
	for i, j := 0, len(filtered)-1; i < j; i, j = i+1, j-1 {
		filtered[i], filtered[j] = filtered[j], filtered[i]
	}
	return filtered, nil
}

func (e *SSHExecutor) CheckConnectivity(ctx context.Context) error {
	return e.ensureConnection(ctx)
}

func (e *SSHExecutor) Dial(ctx context.Context, network, addr string) (net.Conn, error) {
	if err := e.ensureConnection(ctx); err != nil {
		return nil, err
	}
	// ssh.Client.Dial doesn't support context directly for cancellation during dial,
	// but we can check context before dialing.
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	// TODO: Wrap with context cancellation if needed (conn.Close() on ctx.Done())
	return e.client.Dial(network, addr)
}

func (e *SSHExecutor) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.sftp != nil {
		e.sftp.Close()
	}
	if e.client != nil {
		e.client.Close()
		e.client = nil
	}
	return nil
}

// Utils

func containsIgnoreCase(s, substr string) bool {
	// Simple implementation
	return bytes.Contains(
		bytes.ToLower([]byte(s)),
		bytes.ToLower([]byte(substr)),
	)
}
