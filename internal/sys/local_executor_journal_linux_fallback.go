//go:build linux && (!cgo || !systemd_journal)

package sys

import (
	"bufio"
	"context"
	"fmt"
	"io"
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

func journalWalk(ctx context.Context, unit string, newestFirst bool, visit func(string) error) error {
	unitFull := unit
	if !strings.Contains(unitFull, ".") {
		unitFull += ".service"
	}

	args := []string{"-u", unitFull, "--no-pager", "--merge", "-o", "cat"}
	if newestFirst {
		args = append(args, "--reverse")
	}
	cmd := exec.CommandContext(ctx, "journalctl", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}

	errCh := make(chan error, 1)
	go func() {
		b, readErr := io.ReadAll(stderr)
		msg := strings.TrimSpace(string(b))
		if readErr != nil {
			errCh <- readErr
			return
		}
		if msg != "" {
			errCh <- fmt.Errorf("journalctl failed: %s", msg)
			return
		}
		errCh <- nil
	}()

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		if ctx != nil && ctx.Err() != nil {
			_ = cmd.Wait()
			return ctx.Err()
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if err := visit(line); err != nil {
			_ = cmd.Wait()
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		_ = cmd.Wait()
		return err
	}
	waitErr := cmd.Wait()
	stderrErr := <-errCh
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if waitErr != nil {
		if stderrErr != nil {
			return stderrErr
		}
		return waitErr
	}
	return nil
}
