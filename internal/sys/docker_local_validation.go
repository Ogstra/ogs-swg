package sys

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
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

	candidates := e.resolveSingboxCheckCandidates(ctx)

	var lastOut []byte
	var lastErr error
	for _, bin := range candidates {
		output, err := runViaSystemdRun(ctx, bin, "check", "-c", tmpPath)
		if err == nil {
			return nil
		}
		lastOut = output
		lastErr = err

		msg := strings.TrimSpace(string(output))
		if msg == "" {
			msg = err.Error()
		}
		if !isMissingExecutableMsg(msg) {
			return fmt.Errorf("invalid config: %s", msg)
		}
	}

	msg := strings.TrimSpace(string(lastOut))
	if msg == "" && lastErr != nil {
		msg = lastErr.Error()
	}

	// In docker_local mode, binary discovery can fail even when service control
	// works (host/container runtime differences). Do not block config saves in
	// this specific case; service restart/apply will still surface real runtime errors.
	log.Printf("docker_local: skipping sing-box pre-validation because executable could not be resolved (tried: %s). last error: %s", strings.Join(candidates, ", "), msg)
	return nil
}

func isMissingExecutableMsg(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "failed to find executable") ||
		strings.Contains(m, "no such file or directory") ||
		strings.Contains(m, "executable file not found")
}

func (e *DockerLocalExecutor) resolveSingboxCheckCandidates(ctx context.Context) []string {
	out := make([]string, 0, 12)
	seen := make(map[string]struct{})
	add := func(v string) {
		v = strings.TrimSpace(v)
		if v == "" {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}

	// Operator override: useful for non-standard installations.
	add(os.Getenv("OGS_SINGBOX_BINARY_PATH"))

	// Prefer the binary path configured in the host service unit.
	for _, unit := range []string{"sing-box", "sing-box.service", "singbox", "singbox.service"} {
		if out, err := runViaSystemdRun(ctx, "systemctl", "--system", "show", "--property", "ExecStart", "--value", unit); err == nil {
			add(extractFirstAbsPath(string(out)))
		}
	}

	// PATH-based lookup in host execution context.
	if out, err := runViaSystemdRun(ctx, "/bin/sh", "-lc", "command -v sing-box || command -v singbox || true"); err == nil {
		add(strings.TrimSpace(string(out)))
	}

	// Static fallbacks.
	for _, v := range []string{
		"sing-box",
		"/usr/local/bin/sing-box",
		"/usr/bin/sing-box",
		"/opt/sing-box/sing-box",
		"singbox",
		"/usr/local/bin/singbox",
		"/usr/bin/singbox",
		"/opt/sing-box/singbox",
	} {
		add(v)
	}

	return out
}

var absPathRe = regexp.MustCompile(`/(?:[^[:space:];"'])+`)

func extractFirstAbsPath(raw string) string {
	return absPathRe.FindString(strings.TrimSpace(raw))
}
