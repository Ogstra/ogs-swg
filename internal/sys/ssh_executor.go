package sys

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
	"github.com/alitto/pond"
	"github.com/kballard/go-shellquote"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// SSHExecutor implements SystemExecutor for remote hosts via SSH.
type SSHExecutor struct {
	config *core.Config
	client *ssh.Client
	sftp   *sftp.Client
	mu     sync.Mutex
	pool   *pond.WorkerPool
}

func NewSSHExecutor(cfg *core.Config) *SSHExecutor {
	return &SSHExecutor{
		config: cfg,
		pool:   pond.New(10, 100),
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
		HostKeyCallback: e.hostKeyCallback(),
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

func (e *SSHExecutor) hostKeyCallback() ssh.HostKeyCallback {
	if e.config.SSHInsecureIgnoreHostKey {
		return ssh.InsecureIgnoreHostKey()
	}

	paths := make([]string, 0, 3)
	if p := strings.TrimSpace(e.config.SSHKnownHostsPath); p != "" {
		paths = append(paths, p)
	}

	if home, err := os.UserHomeDir(); err == nil && home != "" {
		paths = append(paths, filepath.Join(home, ".ssh", "known_hosts"))
	}
	paths = append(paths, "/etc/ssh/ssh_known_hosts")

	existing := make([]string, 0, len(paths))
	for _, p := range paths {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			existing = append(existing, p)
		}
	}

	if len(existing) == 0 {
		return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			return fmt.Errorf("ssh host key verification enabled but no known_hosts file found (set ssh_known_hosts_path or OGS_SSH_KNOWN_HOSTS)")
		}
	}

	cb, err := knownhosts.New(existing...)
	if err != nil {
		return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			return fmt.Errorf("failed to load known_hosts: %w", err)
		}
	}
	return cb
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

	var output []byte
	var combinedErr error

	done := make(chan struct{})
	e.pool.Submit(func() {
		defer close(done)
		output, combinedErr = session.CombinedOutput(cmdStr)
	})

	select {
	case <-done:
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return output, combinedErr
	case <-ctx.Done():
		return nil, ctx.Err()
	}
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
		// Fallback for protected paths (e.g. /etc/wireguard): upload to /tmp then install via sudo.
		tmpPath = fmt.Sprintf("/tmp/ogs-swg-%d.tmp", time.Now().UnixNano())
		tf, tErr := e.sftp.Create(tmpPath)
		if tErr != nil {
			return fmt.Errorf("sftp create failed: %w (tmp fallback failed: %v)", err, tErr)
		}
		if _, wErr := tf.Write(content); wErr != nil {
			tf.Close()
			_ = e.sftp.Remove(tmpPath)
			return fmt.Errorf("sftp write failed: %w", wErr)
		}
		_ = tf.Chmod(0600)
		_ = tf.Close()

		modeStr := fmt.Sprintf("%04o", fileMode.Perm())
		cmd := shellquote.Join("sudo", "install", "-m", modeStr, tmpPath, path)
		if out, cErr := e.runCommand(ctx, cmd); cErr != nil {
			_ = e.sftp.Remove(tmpPath)
			return fmt.Errorf("sudo install failed: %v, output: %s", cErr, string(out))
		}
		_ = e.sftp.Remove(tmpPath)
		return nil
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
		// Fallback for protected paths where direct SFTP read is denied.
		cmd := shellquote.Join("sudo", "cat", path)
		out, cmdErr := e.runCommand(ctx, cmd)
		if cmdErr != nil {
			return nil, fmt.Errorf("sftp open failed: %v; sudo cat failed: %v", err, cmdErr)
		}
		return out, nil
	}
	defer f.Close()

	return io.ReadAll(f)
}

// Sysctl Management (Whitelist enforcement still applies)

func (e *SSHExecutor) ApplySysctl(ctx context.Context, key, value string) error {
	if !AllowedSysctlKeys[key] {
		return fmt.Errorf("sysctl key '%s' is not in the whitelist", key)
	}
	cmd := shellquote.Join("sudo", "sysctl", "-w", fmt.Sprintf("%s=%s", key, value))
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
	cmd := shellquote.Join("sudo", "sysctl", "-n", key)
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

func (e *SSHExecutor) GetWireGuardStats(ctx context.Context) (map[string]core.PeerStats, error) {
	if err := e.ensureConnection(ctx); err != nil {
		return nil, err
	}

	// Dump all stats: interface public-key preshared-key endpoint allowed-ips latest-handshake transfer-rx transfer-tx persistent-keepalive
	output, err := e.runCommand(ctx, "sudo wg show all dump")
	if err == nil {
		return parseWGDumpStats(output), nil
	}

	// Fallback for stricter sudoers setups that allow only `wg show`.
	textOut, textErr := e.runCommand(ctx, "sudo wg show")
	if textErr != nil {
		return nil, fmt.Errorf("failed to execute wg show (dump=%v, text=%v)", err, textErr)
	}
	return parseWGTextStats(textOut), nil
}

func parseWGDumpStats(output []byte) map[string]core.PeerStats {
	stats := make(map[string]core.PeerStats)
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		parts := strings.Fields(line)
		// Peer line format:
		// <intf> <pubkey> <psk> <endpoint> <allowed-ips> <handshake> <rx> <tx> <keepalive>
		if len(parts) < 8 {
			continue
		}
		pubKey := parts[1]
		if len(pubKey) < 40 {
			continue
		}
		endpoint := parts[3]
		if endpoint == "(none)" {
			endpoint = ""
		}
		latestHandshake, _ := strconv.ParseInt(parts[5], 10, 64)
		transferRx, _ := strconv.ParseInt(parts[6], 10, 64)
		transferTx, _ := strconv.ParseInt(parts[7], 10, 64)
		stats[pubKey] = core.PeerStats{
			PublicKey:       pubKey,
			Endpoint:        endpoint,
			LatestHandshake: latestHandshake,
			TransferRx:      transferRx,
			TransferTx:      transferTx,
		}
	}
	return stats
}

func parseWGTextStats(output []byte) map[string]core.PeerStats {
	stats := make(map[string]core.PeerStats)
	lines := strings.Split(string(output), "\n")
	var currentPeer string

	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "peer: ") {
			currentPeer = strings.TrimSpace(strings.TrimPrefix(line, "peer: "))
			if currentPeer != "" {
				if _, ok := stats[currentPeer]; !ok {
					stats[currentPeer] = core.PeerStats{PublicKey: currentPeer}
				}
			}
			continue
		}
		if currentPeer == "" {
			continue
		}

		ps := stats[currentPeer]
		switch {
		case strings.HasPrefix(line, "endpoint: "):
			v := strings.TrimSpace(strings.TrimPrefix(line, "endpoint: "))
			if v == "(none)" {
				v = ""
			}
			ps.Endpoint = v
		case strings.HasPrefix(line, "latest handshake: "):
			v := strings.TrimSpace(strings.TrimPrefix(line, "latest handshake: "))
			ps.LatestHandshake = parseWGRelativeHandshake(v)
		case strings.HasPrefix(line, "transfer: "):
			v := strings.TrimSpace(strings.TrimPrefix(line, "transfer: "))
			parts := strings.Split(v, ",")
			if len(parts) >= 2 {
				rxText := strings.TrimSpace(strings.TrimSuffix(parts[0], "received"))
				txText := strings.TrimSpace(strings.TrimSuffix(parts[1], "sent"))
				ps.TransferRx = parseWGHumanBytes(rxText)
				ps.TransferTx = parseWGHumanBytes(txText)
			}
		}
		stats[currentPeer] = ps
	}
	return stats
}

func parseWGHumanBytes(v string) int64 {
	fields := strings.Fields(strings.TrimSpace(v))
	if len(fields) < 2 {
		return 0
	}
	n, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || n < 0 {
		return 0
	}
	unit := strings.ToUpper(strings.TrimSpace(fields[1]))
	mul := float64(1)
	switch unit {
	case "B", "BYTE", "BYTES":
		mul = 1
	case "KIB":
		mul = 1024
	case "MIB":
		mul = 1024 * 1024
	case "GIB":
		mul = 1024 * 1024 * 1024
	case "TIB":
		mul = 1024 * 1024 * 1024 * 1024
	}
	return int64(n * mul)
}

func parseWGRelativeHandshake(v string) int64 {
	v = strings.TrimSpace(strings.TrimSuffix(v, "ago"))
	if v == "" || strings.EqualFold(v, "(none)") || strings.EqualFold(v, "never") {
		return 0
	}
	total := int64(0)
	for _, token := range strings.Split(v, ",") {
		token = strings.TrimSpace(token)
		parts := strings.Fields(token)
		if len(parts) < 2 {
			continue
		}
		n, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil || n < 0 {
			continue
		}
		unit := strings.ToLower(parts[1])
		switch {
		case strings.HasPrefix(unit, "sec"):
			total += n
		case strings.HasPrefix(unit, "min"):
			total += n * 60
		case strings.HasPrefix(unit, "hour"):
			total += n * 3600
		case strings.HasPrefix(unit, "day"):
			total += n * 86400
		case strings.HasPrefix(unit, "week"):
			total += n * 7 * 86400
		}
	}
	if total <= 0 {
		return 0
	}
	return time.Now().Unix() - total
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
	if e.pool != nil {
		e.pool.StopAndWait()
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
