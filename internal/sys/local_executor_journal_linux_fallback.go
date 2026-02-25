//go:build linux && (!cgo || !systemd_journal)

package sys

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// journalRead reads logs through journalctl when sdjournal support is not enabled.
func journalRead(unit string, limit int, filter string) ([]string, error) {
	if limit <= 0 {
		limit = 100
	}

	unitFull := unit
	if !strings.Contains(unitFull, ".") {
		unitFull += ".service"
	}

	fetchLimit := limit
	if filter != "" {
		fetchLimit = limit * 5
		if fetchLimit > 5000 {
			fetchLimit = 5000
		}
	}

	cmd := exec.Command("journalctl", "-u", unitFull, "-n", strconv.Itoa(fetchLimit), "--no-pager", "-o", "cat")
	out, err := cmd.CombinedOutput()
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
	}

	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return lines, nil
}
