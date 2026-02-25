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

	bin, ok := e.resolveSingboxCheckBinary(ctx)
	if !ok {
		return nil
	}

	output, err := runViaSystemdRun(ctx, bin, "check", "-c", tmpPath)
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if msg == "" {
			msg = err.Error()
		}
		if isMissingExecutableMsg(msg) {
			e.disableSingboxPreValidation([]string{bin}, msg)
			return nil
		}
		return fmt.Errorf("invalid config: %s", msg)
	}
	return nil
}

func isMissingExecutableMsg(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "failed to find executable") ||
		strings.Contains(m, "no such file or directory") ||
		strings.Contains(m, "executable file not found")
}

func (e *DockerLocalExecutor) resolveSingboxCheckBinary(ctx context.Context) (string, bool) {
	e.singboxCheckMu.Lock()
	if e.singboxCheckUnavailable {
		e.singboxCheckMu.Unlock()
		return "", false
	}
	if e.singboxCheckBinary != "" {
		bin := e.singboxCheckBinary
		e.singboxCheckMu.Unlock()
		return bin, true
	}
	e.singboxCheckMu.Unlock()

	candidates := e.resolveSingboxCheckCandidates(ctx)
	lastErrMsg := ""
	for _, bin := range candidates {
		probeCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
		output, err := runViaSystemdRun(probeCtx, bin, "version")
		cancel()

		if err == nil {
			e.singboxCheckMu.Lock()
			e.singboxCheckBinary = bin
			e.singboxCheckMu.Unlock()
			return bin, true
		}

		msg := strings.TrimSpace(string(output))
		if msg == "" {
			msg = err.Error()
		}
		lastErrMsg = msg

		// Non "missing executable" errors often mean the binary exists but the
		// probe command failed; still use this candidate for real validation.
		if !isMissingExecutableMsg(msg) {
			e.singboxCheckMu.Lock()
			e.singboxCheckBinary = bin
			e.singboxCheckMu.Unlock()
			return bin, true
		}
	}

	e.disableSingboxPreValidation(candidates, lastErrMsg)
	return "", false
}

func (e *DockerLocalExecutor) disableSingboxPreValidation(candidates []string, reason string) {
	e.singboxCheckMu.Lock()
	defer e.singboxCheckMu.Unlock()

	if e.singboxCheckUnavailable {
		return
	}
	e.singboxCheckUnavailable = true
	e.singboxCheckBinary = ""

	log.Printf(
		"docker_local: skipping sing-box pre-validation because executable could not be resolved (tried: %s). reason: %s",
		strings.Join(candidates, ", "),
		reason,
	)
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
