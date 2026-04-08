//go:build linux && cgo && systemd_journal

package sys

import (
	"context"
	"fmt"
	"strings"

	"github.com/coreos/go-systemd/v22/sdjournal"
)

// NOTE: this implementation is opt-in and requires:
//   - CGO enabled
//   - libsystemd headers (e.g. libsystemd-dev)
//   - build tag: systemd_journal
//
// Default builds use local_executor_journal_linux_fallback.go.

// journalRead reads up to limit matching lines from the systemd journal for unit.
// If filter is non-empty, only lines containing filter (case-insensitive) are returned.
func journalRead(ctx context.Context, unit string, limit int, filter string) ([]string, error) {
	j, err := sdjournal.NewJournal()
	if err != nil {
		return nil, fmt.Errorf("failed to open journal: %v", err)
	}
	defer j.Close()

	unitFull := unit
	if !strings.Contains(unitFull, ".") {
		unitFull = unitFull + ".service"
	}
	if err := j.AddMatch(sdjournal.SD_JOURNAL_FIELD_SYSTEMD_UNIT + "=" + unitFull); err != nil {
		return nil, err
	}
	if err := j.SeekTail(); err != nil {
		return nil, err
	}

	fetchLimit := limit
	if filter != "" {
		fetchLimit = limit * 5
		if fetchLimit > 5000 {
			fetchLimit = 5000
		}
	}

	q := strings.ToLower(filter)
	var lines []string
	for i := 0; i < fetchLimit && len(lines) < limit; i++ {
		if ctx != nil && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		n, err := j.Previous()
		if err != nil || n == 0 {
			break
		}
		entry, err := j.GetEntry()
		if err != nil {
			continue
		}
		msg := entry.Fields[sdjournal.SD_JOURNAL_FIELD_MESSAGE]
		if filter != "" && !strings.Contains(strings.ToLower(msg), q) {
			continue
		}
		lines = append(lines, msg)
	}

	// Reverse to chronological order
	for i, j2 := 0, len(lines)-1; i < j2; i, j2 = i+1, j2-1 {
		lines[i], lines[j2] = lines[j2], lines[i]
	}
	return lines, nil
}
