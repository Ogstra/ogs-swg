//go:build linux && (!cgo || !systemd_journal)

package sys

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// journalRead reads logs through journalctl when sdjournal support is not enabled.
func journalRead(ctx context.Context, unit string, limit int, filter string) ([]string, error) {
	if limit <= 0 {
		limit = 100
	}

	unitFull := unit
	if !strings.Contains(unitFull, ".") {
		unitFull += ".service"
	}

	var cmd *exec.Cmd
	if filter != "" {
		// NOTE for search: --merge includes all rotated journal segments (system@*.journal).
		// Without it, journalctl may only scan the active journal file, missing older entries.
		// We omit -n here to let Go-level filtering collect up to limit matches across
		// the full history.
		cmd = exec.CommandContext(ctx, "journalctl", "-u", unitFull, "--no-pager", "--merge", "-o", "cat")
	} else {
		// Tail: only need recent lines, -n is safe and efficient here.
		cmd = exec.CommandContext(ctx, "journalctl", "-u", unitFull, "-n", strconv.Itoa(limit), "--no-pager", "-o", "cat")
	}

	out, err := cmd.CombinedOutput()
	if ctx != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return nil, fmt.Errorf("journalctl failed: %v", err)
		}
		return nil, fmt.Errorf("journalctl failed: %v: %s", err, msg)
	}

	q := strings.ToLower(filter)
	rawLines := strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n")
	lines := make([]string, 0, limit)
	for _, line := range rawLines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if filter != "" && !strings.Contains(strings.ToLower(line), q) {
			continue
		}
		lines = append(lines, line)
		if filter != "" && len(lines) >= limit {
			break
		}
	}

	if filter == "" && len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return lines, nil
}
