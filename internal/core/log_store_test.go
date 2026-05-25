package core

import (
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestLogStore(t *testing.T) (*LogStore, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "singbox_logs.db")
	ls, err := NewLogStore(dbPath)
	if err != nil {
		t.Fatalf("NewLogStore: %v", err)
	}
	t.Cleanup(func() { ls.Close() })
	return ls, dir
}

func TestLogStore_OpenClose(t *testing.T) {
	ls, _ := newTestLogStore(t)
	if ls.db == nil {
		t.Fatal("db is nil after NewLogStore")
	}
}

func TestLogStore_SchemaExists(t *testing.T) {
	ls, _ := newTestLogStore(t)
	ctx := context.Background()
	tables := []string{"singbox_logs", "singbox_logs_fts", "log_segments"}
	for _, tbl := range tables {
		var name string
		err := ls.db.QueryRowContext(ctx,
			`SELECT name FROM sqlite_master WHERE type IN ('table','shadow') AND name = ?`, tbl,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", tbl, err)
		}
	}
}

func TestLogStore_PragmasApplied(t *testing.T) {
	ls, _ := newTestLogStore(t)
	ctx := context.Background()

	var mode string
	if err := ls.db.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("expected journal_mode=wal, got %q", mode)
	}
}

func TestLogStore_InsertLogs_TailHot(t *testing.T) {
	ls, _ := newTestLogStore(t)
	ctx := context.Background()

	ts := time.Now().UnixMilli()
	lines := []string{
		"2024/01/01 00:00:01 INFO sing-box started",
		"2024/01/01 00:00:02 ERROR connection refused",
		"2024/01/01 00:00:03 WARN rate limit exceeded",
	}
	if err := ls.InsertLogs(ctx, ts, lines); err != nil {
		t.Fatalf("InsertLogs: %v", err)
	}

	rows, err := ls.TailHot(ctx, 10)
	if err != nil {
		t.Fatalf("TailHot: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}
	// Chronological order: first row has lowest id.
	if rows[0].ID >= rows[1].ID || rows[1].ID >= rows[2].ID {
		t.Errorf("rows not in chronological order: ids %d %d %d", rows[0].ID, rows[1].ID, rows[2].ID)
	}
	if rows[0].Raw != lines[0] {
		t.Errorf("first row raw = %q, want %q", rows[0].Raw, lines[0])
	}
}

func TestLogStore_TailHot_Limit(t *testing.T) {
	ls, _ := newTestLogStore(t)
	ctx := context.Background()

	ts := time.Now().UnixMilli()
	for i := 0; i < 10; i++ {
		if err := ls.InsertLogs(ctx, ts+int64(i), []string{"line"}); err != nil {
			t.Fatalf("InsertLogs: %v", err)
		}
	}
	rows, err := ls.TailHot(ctx, 5)
	if err != nil {
		t.Fatalf("TailHot: %v", err)
	}
	if len(rows) != 5 {
		t.Errorf("expected 5 rows, got %d", len(rows))
	}
}

func TestLogStore_PollAfterID(t *testing.T) {
	ls, _ := newTestLogStore(t)
	ctx := context.Background()

	ts := time.Now().UnixMilli()
	lines := []string{"first", "second", "third", "fourth"}
	if err := ls.InsertLogs(ctx, ts, lines); err != nil {
		t.Fatalf("InsertLogs: %v", err)
	}

	all, err := ls.TailHot(ctx, 10)
	if err != nil {
		t.Fatalf("TailHot: %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("expected 4 rows, got %d", len(all))
	}

	// Poll after the second row's id.
	afterID := all[1].ID
	polled, err := ls.PollAfterID(ctx, afterID, 100)
	if err != nil {
		t.Fatalf("PollAfterID: %v", err)
	}
	if len(polled) != 2 {
		t.Fatalf("expected 2 rows after id %d, got %d", afterID, len(polled))
	}
	for _, r := range polled {
		if r.ID <= afterID {
			t.Errorf("row id %d is not > afterID %d", r.ID, afterID)
		}
	}
}

func TestLogStore_WalkHot_FTS5Match(t *testing.T) {
	ls, _ := newTestLogStore(t)
	ctx := context.Background()

	ts := time.Now().UnixMilli()
	lines := []string{
		"2024/01/01 00:00:01 INFO connection established",
		"2024/01/01 00:00:02 ERROR connection refused",
		"2024/01/01 00:00:03 INFO everything is fine",
	}
	if err := ls.InsertLogs(ctx, ts, lines); err != nil {
		t.Fatalf("InsertLogs: %v", err)
	}

	var matched []LogRow
	err := ls.WalkHot(ctx, "error", 0, 0, func(r LogRow) error {
		matched = append(matched, r)
		return nil
	})
	if err != nil {
		t.Fatalf("WalkHot FTS5: %v", err)
	}
	if len(matched) != 1 {
		t.Fatalf("expected 1 match for 'error', got %d", len(matched))
	}
	if !contains(matched[0].Raw, "ERROR") {
		t.Errorf("matched row does not contain ERROR: %q", matched[0].Raw)
	}
}

func TestLogStore_WalkHot_TimeRange(t *testing.T) {
	ls, _ := newTestLogStore(t)
	ctx := context.Background()

	base := time.Now().UnixMilli()
	// Insert three groups at different times.
	_ = ls.InsertLogs(ctx, base-3000, []string{"old line"})
	_ = ls.InsertLogs(ctx, base-1000, []string{"mid line"})
	_ = ls.InsertLogs(ctx, base, []string{"new line"})

	// Walk only the middle and new lines.
	fromMs := base - 2000
	toMs := base + 1000
	var matched []LogRow
	err := ls.WalkHot(ctx, "", fromMs, toMs, func(r LogRow) error {
		matched = append(matched, r)
		return nil
	})
	if err != nil {
		t.Fatalf("WalkHot time range: %v", err)
	}
	if len(matched) != 2 {
		t.Fatalf("expected 2 rows in time range, got %d", len(matched))
	}
}

func TestLogStore_WalkHot_SpecialChars(t *testing.T) {
	ls, _ := newTestLogStore(t)
	ctx := context.Background()

	ts := time.Now().UnixMilli()
	lines := []string{
		"2024/01/01 00:00:01 INFO [user-1] inbound connection",
		"2024/01/01 00:00:02 INFO other line",
	}
	if err := ls.InsertLogs(ctx, ts, lines); err != nil {
		t.Fatalf("InsertLogs: %v", err)
	}

	// This must NOT crash, even though [user-1] contains FTS5 special chars.
	var matched []LogRow
	err := ls.WalkHot(ctx, "[user-1]", 0, 0, func(r LogRow) error {
		matched = append(matched, r)
		return nil
	})
	if err != nil {
		t.Fatalf("WalkHot special chars crashed: %v", err)
	}
	// Should find the line with [user-1].
	if len(matched) != 1 {
		t.Fatalf("expected 1 match for '[user-1]', got %d", len(matched))
	}
}

func TestLogStore_WalkHot_StopEarly(t *testing.T) {
	ls, _ := newTestLogStore(t)
	ctx := context.Background()

	ts := time.Now().UnixMilli()
	lines := []string{"line1", "line2", "line3", "line4", "line5"}
	if err := ls.InsertLogs(ctx, ts, lines); err != nil {
		t.Fatalf("InsertLogs: %v", err)
	}

	var count int
	err := ls.WalkHot(ctx, "", 0, 0, func(r LogRow) error {
		count++
		if count >= 2 {
			return ErrStopLogWalk
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkHot stop early returned error: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 visits before stop, got %d", count)
	}
}

func TestLogStore_ExportToCold(t *testing.T) {
	ls, dir := newTestLogStore(t)
	ctx := context.Background()

	coldDir := filepath.Join(dir, "cold")

	base := time.Now().UnixMilli()
	lines := []string{
		"2024/01/01 INFO first log line",
		"2024/01/01 INFO second log line",
		"2024/01/01 ERROR third log line",
	}
	if err := ls.InsertLogs(ctx, base, lines); err != nil {
		t.Fatalf("InsertLogs: %v", err)
	}

	all, _ := ls.TailHot(ctx, 100)
	if len(all) == 0 {
		t.Fatal("no rows inserted")
	}
	maxID := all[len(all)-1].ID

	seg, err := ls.ExportToCold(ctx, coldDir, maxID)
	if err != nil {
		t.Fatalf("ExportToCold: %v", err)
	}
	if seg == nil {
		t.Fatal("ExportToCold returned nil segment")
	}

	// Rows removed from hot tier.
	remaining, _ := ls.TailHot(ctx, 100)
	if len(remaining) != 0 {
		t.Errorf("expected 0 rows after export, got %d", len(remaining))
	}

	// Segment record inserted.
	if seg.RowCount != int64(len(lines)) {
		t.Errorf("segment row_count = %d, want %d", seg.RowCount, len(lines))
	}
	if seg.StartTs != base || seg.EndTs != base {
		t.Errorf("segment ts mismatch: start=%d end=%d base=%d", seg.StartTs, seg.EndTs, base)
	}

	// .gz file exists and contains the lines.
	gzPath := filepath.Join(coldDir, seg.Filename)
	f, err := os.Open(gzPath)
	if err != nil {
		t.Fatalf("open gz file: %v", err)
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer gr.Close()
	data, err := os.ReadFile(gzPath)
	if err != nil {
		t.Fatalf("read gz: %v", err)
	}
	if len(data) == 0 {
		t.Error("gz file is empty")
	}
}

func TestLogStore_SegmentsInRange(t *testing.T) {
	ls, dir := newTestLogStore(t)
	ctx := context.Background()

	coldDir := filepath.Join(dir, "cold")

	base := time.Now().UnixMilli()
	_ = ls.InsertLogs(ctx, base, []string{"a line"})
	all, _ := ls.TailHot(ctx, 100)
	maxID := all[len(all)-1].ID

	seg, err := ls.ExportToCold(ctx, coldDir, maxID)
	if err != nil {
		t.Fatalf("ExportToCold: %v", err)
	}

	// Query with overlapping range.
	segs, err := ls.SegmentsInRange(ctx, base-1000, base+1000)
	if err != nil {
		t.Fatalf("SegmentsInRange: %v", err)
	}
	if len(segs) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segs))
	}
	if segs[0].Filename != seg.Filename {
		t.Errorf("segment filename mismatch: got %q want %q", segs[0].Filename, seg.Filename)
	}

	// Query with non-overlapping range returns nothing.
	segs2, err := ls.SegmentsInRange(ctx, base+5000, base+10000)
	if err != nil {
		t.Fatalf("SegmentsInRange non-overlap: %v", err)
	}
	if len(segs2) != 0 {
		t.Errorf("expected 0 segments outside range, got %d", len(segs2))
	}
}

func TestLogStore_CheckRetention_Size(t *testing.T) {
	ls, dir := newTestLogStore(t)
	ctx := context.Background()

	coldDir := filepath.Join(dir, "cold")

	ts := time.Now().UnixMilli()
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = "some log line that takes up a bit of space for testing retention"
	}
	if err := ls.InsertLogs(ctx, ts, lines); err != nil {
		t.Fatalf("InsertLogs: %v", err)
	}

	// Set maxMB to 0 so any size exceeds the threshold.
	seg, err := ls.CheckRetention(ctx, "size", 0, 0, "days", coldDir)
	if err != nil {
		t.Fatalf("CheckRetention size: %v", err)
	}
	if seg == nil {
		t.Fatal("expected export to happen, got nil segment")
	}
}

func TestLogStore_CheckRetention_Time_Zero(t *testing.T) {
	ls, dir := newTestLogStore(t)
	ctx := context.Background()

	coldDir := filepath.Join(dir, "cold")

	ts := time.Now().UnixMilli()
	_ = ls.InsertLogs(ctx, ts, []string{"line"})

	// days=0 should not export anything.
	seg, err := ls.CheckRetention(ctx, "time", 200, 0, "days", coldDir)
	if err != nil {
		t.Fatalf("CheckRetention time days=0: %v", err)
	}
	if seg != nil {
		t.Error("expected no export with days=0")
	}
}

func TestLogStore_OldestHotTs(t *testing.T) {
	ls, _ := newTestLogStore(t)
	ctx := context.Background()

	// Empty store returns 0.
	ts0, err := ls.OldestHotTs(ctx)
	if err != nil {
		t.Fatalf("OldestHotTs empty: %v", err)
	}
	if ts0 != 0 {
		t.Errorf("expected 0 for empty store, got %d", ts0)
	}

	base := time.Now().UnixMilli()
	_ = ls.InsertLogs(ctx, base+1000, []string{"later"})
	_ = ls.InsertLogs(ctx, base, []string{"earlier"})

	oldest, err := ls.OldestHotTs(ctx)
	if err != nil {
		t.Fatalf("OldestHotTs: %v", err)
	}
	if oldest != base {
		t.Errorf("OldestHotTs = %d, want %d", oldest, base)
	}
}

func TestLogStore_LogDBPathFor(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"data/stats.db", "data/singbox_logs.db"},
		{"/var/data/app.db", "/var/data/singbox_logs.db"},
	}
	for _, c := range cases {
		got := LogDBPathFor(c.input)
		if got != c.want {
			t.Errorf("LogDBPathFor(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestLogStore_ParseLogMeta(t *testing.T) {
	cases := []struct {
		line      string
		wantLevel string
		wantUser  string
	}{
		{"2024/01/01 INFO sing-box started", "INFO", ""},
		{"2024/01/01 ERROR connection refused", "ERROR", ""},
		{"2024/01/01 WARN rate limit exceeded", "WARN", ""},
		{"2024/01/01 WARNING rate limit exceeded", "WARN", ""},
		{"2024/01/01 DEBUG tcp: [user1] inbound connection from 1.2.3.4", "DEBUG", "[user1]"},
		{"plain line with no level", "", ""},
	}
	for _, c := range cases {
		lvl, usr := parseLogMeta(c.line)
		if lvl != c.wantLevel {
			t.Errorf("parseLogMeta(%q) level = %q, want %q", c.line, lvl, c.wantLevel)
		}
		if usr != c.wantUser {
			t.Errorf("parseLogMeta(%q) user = %q, want %q", c.line, usr, c.wantUser)
		}
	}
}

// contains is a simple substring helper for tests.
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
