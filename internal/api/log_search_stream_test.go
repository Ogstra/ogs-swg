package api

import (
	"compress/gzip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
)

// newSearchTestServer creates a Server with a real LogStore backed by a temp DB.
// The server's config.DatabasePath is set so the cold dir defaults to
// filepath.Join(filepath.Dir(DatabasePath), "logs").
func newSearchTestServer(t *testing.T) (*Server, *core.LogStore, string) {
	t.Helper()
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "stats.db")
	logDBPath := filepath.Join(tmp, "singbox_logs.db")

	store, err := core.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	logStore, err := core.NewLogStore(logDBPath)
	if err != nil {
		t.Fatalf("NewLogStore: %v", err)
	}
	t.Cleanup(func() { logStore.Close() })

	cfg := &core.Config{
		EnableSingbox: true,
		DatabasePath:  dbPath,
	}

	srv := NewServer(store, cfg, nil)
	srv.logStore = logStore
	return srv, logStore, tmp
}

// insertRows inserts n rows into the log store with sequential timestamps.
// The line format is "YYYY/MM/DD HH:MM:SS LEVEL msg <i>".
func insertRows(t *testing.T, ls *core.LogStore, count int, tsStart int64, levelWord, msgPrefix string) {
	t.Helper()
	for i := 0; i < count; i++ {
		ts := tsStart + int64(i)*1000
		line := fmt.Sprintf("2024/01/01 12:00:%02d %s %s %d", i%60, levelWord, msgPrefix, i)
		if err := ls.InsertLogs(context.Background(), ts, []string{line}); err != nil {
			t.Fatalf("InsertLogs: %v", err)
		}
	}
}

// collectChunks is a helper emitChunk that accumulates received lines.
func collectChunks(out *[]string) func([]string, int) error {
	return func(lines []string, _ int) error {
		*out = append(*out, lines...)
		return nil
	}
}

// noopStatus is an emitStatus no-op.
func noopStatus(msg string, n int) error { return nil }

// ---------------------------------------------------------------------------
// TestSearchLogsViaStore_HotOnly — 50 rows, search for "error"
// ---------------------------------------------------------------------------

func TestSearchLogsViaStore_HotOnly(t *testing.T) {
	srv, ls, _ := newSearchTestServer(t)

	now := time.Now().UnixMilli()
	// 30 "error" rows and 20 "info" rows
	for i := 0; i < 30; i++ {
		ts := now + int64(i)*1000
		line := fmt.Sprintf("2024/01/01 12:00:%02d ERROR something went wrong %d", i%60, i)
		if err := ls.InsertLogs(context.Background(), ts, []string{line}); err != nil {
			t.Fatalf("InsertLogs error row: %v", err)
		}
	}
	for i := 0; i < 20; i++ {
		ts := now + int64(30+i)*1000
		line := fmt.Sprintf("2024/01/01 12:00:%02d INFO all fine %d", (30+i)%60, i)
		if err := ls.InsertLogs(context.Background(), ts, []string{line}); err != nil {
			t.Fatalf("InsertLogs info row: %v", err)
		}
	}

	opts := logSearchOptions{
		query:     compileLogQuery("error"),
		limit:     200,
		chunkSize: 10,
	}

	var received []string
	summary, err := srv.searchLogsViaStore(context.Background(), opts, collectChunks(&received), noopStatus)
	if err != nil {
		t.Fatalf("searchLogsViaStore: %v", err)
	}
	if summary.matched != 30 {
		t.Errorf("matched=%d want 30", summary.matched)
	}
	if len(received) != 30 {
		t.Errorf("received %d lines want 30", len(received))
	}
	// All received lines must contain "error" (case-insensitive).
	for _, line := range received {
		lower := line
		found := false
		for i := 0; i+5 <= len(lower); i++ {
			if lower[i:i+5] == "ERROR" || lower[i:i+5] == "error" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("received line without 'error': %q", line)
		}
	}
}

// ---------------------------------------------------------------------------
// TestSearchLogsViaStore_WithColdSegment
// ---------------------------------------------------------------------------

func TestSearchLogsViaStore_WithColdSegment(t *testing.T) {
	srv, ls, _ := newSearchTestServer(t)

	// Cold dir: filepath.Join(filepath.Dir(DatabasePath), "logs")
	coldDir := filepath.Join(filepath.Dir(srv.config.DatabasePath), "logs")
	if err := os.MkdirAll(coldDir, 0o755); err != nil {
		t.Fatalf("MkdirAll coldDir: %v", err)
	}

	// Create timestamps: cold lines are older, hot lines are newer.
	coldBaseMs := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	hotBaseMs := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC).UnixMilli()

	// --- Write cold segment: 20 lines, 5 contain "coldmatch" ---
	segFilename := "singbox_20230101-20230120.log.gz"
	segPath := filepath.Join(coldDir, segFilename)
	{
		f, err := os.Create(segPath)
		if err != nil {
			t.Fatalf("create cold seg: %v", err)
		}
		gz := gzip.NewWriter(f)
		for i := 0; i < 20; i++ {
			var line string
			if i%4 == 0 {
				line = fmt.Sprintf("-0000 2023-01-01 00:00:%02d INFO coldmatch entry %d\n", i, i)
			} else {
				line = fmt.Sprintf("-0000 2023-01-01 00:00:%02d INFO regular log entry %d\n", i, i)
			}
			if _, err := gz.Write([]byte(line)); err != nil {
				t.Fatalf("gz write: %v", err)
			}
		}
		if err := gz.Close(); err != nil {
			t.Fatalf("gz close: %v", err)
		}
		if err := f.Close(); err != nil {
			t.Fatalf("f close: %v", err)
		}
	}

	// Register the segment in log_segments.
	segStartTs := coldBaseMs
	segEndTs := coldBaseMs + 19*1000
	_, err := ls.DB().ExecContext(context.Background(),
		`INSERT INTO log_segments(filename, start_ts, end_ts, row_count, size_bytes) VALUES(?,?,?,?,?)`,
		segFilename, segStartTs, segEndTs, 20, 0)
	if err != nil {
		t.Fatalf("insert segment: %v", err)
	}

	// --- Insert hot rows: 10 lines, 3 contain "coldmatch" ---
	for i := 0; i < 10; i++ {
		ts := hotBaseMs + int64(i)*1000
		var line string
		if i%3 == 0 {
			line = fmt.Sprintf("2024/06/01 12:00:%02d INFO coldmatch hot entry %d", i%60, i)
		} else {
			line = fmt.Sprintf("2024/06/01 12:00:%02d INFO unrelated hot entry %d", i%60, i)
		}
		if err := ls.InsertLogs(context.Background(), ts, []string{line}); err != nil {
			t.Fatalf("InsertLogs hot: %v", err)
		}
	}

	// Query with from time in the cold range.
	fromTime := time.UnixMilli(coldBaseMs - 1000)
	opts := logSearchOptions{
		query: compileLogQuery("coldmatch"),
		timeRange: logTimeRange{
			from: &fromTime,
		},
		limit:     200,
		chunkSize: 50,
	}

	var received []string
	var statusMsgs []string
	emitStatus := func(msg string, n int) error {
		statusMsgs = append(statusMsgs, msg)
		return nil
	}

	summary, err := srv.searchLogsViaStore(context.Background(), opts, collectChunks(&received), emitStatus)
	if err != nil {
		t.Fatalf("searchLogsViaStore with cold: %v", err)
	}

	// 5 cold + 4 hot (i=0,3,6,9 → i%3==0 gives 4) = 9 - but actually
	// "coldmatch" in hot: i=0,3,6,9 → 4 lines; in cold: i=0,4,8,12,16 → 5 lines. Total = 9.
	if summary.matched != 9 {
		t.Errorf("matched=%d want 9", summary.matched)
	}
	if len(received) != 9 {
		t.Errorf("received %d lines want 9", len(received))
	}

	// Verify emitStatus was called after the cold segment.
	foundSegStatus := false
	for _, msg := range statusMsgs {
		if msg == "searched "+segFilename {
			foundSegStatus = true
			break
		}
	}
	if !foundSegStatus {
		t.Errorf("emitStatus not called with segment name; got: %v", statusMsgs)
	}
}

func TestSearchLogsViaStore_NoTimeRangeIncludesColdSegments(t *testing.T) {
	srv, ls, _ := newSearchTestServer(t)

	coldDir := filepath.Join(filepath.Dir(srv.config.DatabasePath), "logs")
	if err := os.MkdirAll(coldDir, 0o755); err != nil {
		t.Fatalf("MkdirAll coldDir: %v", err)
	}

	segFilename := "singbox_20230101-20230101.log.gz"
	segPath := filepath.Join(coldDir, segFilename)
	{
		f, err := os.Create(segPath)
		if err != nil {
			t.Fatalf("create cold seg: %v", err)
		}
		gz := gzip.NewWriter(f)
		for _, line := range []string{
			"-0000 2023-01-01 00:00:00 INFO coldopen match",
			"-0000 2023-01-01 00:00:01 INFO unrelated cold",
		} {
			if _, err := gz.Write([]byte(line + "\n")); err != nil {
				t.Fatalf("gz write: %v", err)
			}
		}
		if err := gz.Close(); err != nil {
			t.Fatalf("gz close: %v", err)
		}
		if err := f.Close(); err != nil {
			t.Fatalf("f close: %v", err)
		}
	}

	coldBaseMs := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	if _, err := ls.DB().ExecContext(context.Background(),
		`INSERT INTO log_segments(filename, start_ts, end_ts, row_count, size_bytes) VALUES(?,?,?,?,?)`,
		segFilename, coldBaseMs, coldBaseMs+1000, 2, 0); err != nil {
		t.Fatalf("insert segment: %v", err)
	}
	if err := ls.InsertLogs(context.Background(), time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC).UnixMilli(), []string{
		"2024/06/01 12:00:00 INFO coldopen hot match",
	}); err != nil {
		t.Fatalf("InsertLogs hot: %v", err)
	}

	opts := logSearchOptions{
		query:     compileLogQuery("coldopen"),
		limit:     200,
		chunkSize: 50,
	}

	var received []string
	summary, err := srv.searchLogsViaStore(context.Background(), opts, collectChunks(&received), noopStatus)
	if err != nil {
		t.Fatalf("searchLogsViaStore no range: %v", err)
	}
	if summary.matched != 2 {
		t.Fatalf("matched=%d want 2; no-range search must include cold and hot tiers", summary.matched)
	}
	if len(received) != 2 {
		t.Fatalf("received=%d want 2", len(received))
	}
}

func TestSearchLogsViaStore_UsesConfiguredColdDir(t *testing.T) {
	srv, ls, tmp := newSearchTestServer(t)
	customColdDir := filepath.Join(tmp, "custom-cold")
	srv.config.LogColdDir = customColdDir
	if err := os.MkdirAll(customColdDir, 0o755); err != nil {
		t.Fatalf("MkdirAll customColdDir: %v", err)
	}

	segFilename := "singbox_20230102-20230102.log.gz"
	segPath := filepath.Join(customColdDir, segFilename)
	{
		f, err := os.Create(segPath)
		if err != nil {
			t.Fatalf("create cold seg: %v", err)
		}
		gz := gzip.NewWriter(f)
		if _, err := gz.Write([]byte("-0000 2023-01-02 00:00:00 INFO customcold match\n")); err != nil {
			t.Fatalf("gz write: %v", err)
		}
		if err := gz.Close(); err != nil {
			t.Fatalf("gz close: %v", err)
		}
		if err := f.Close(); err != nil {
			t.Fatalf("f close: %v", err)
		}
	}

	coldBaseMs := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC).UnixMilli()
	if _, err := ls.DB().ExecContext(context.Background(),
		`INSERT INTO log_segments(filename, start_ts, end_ts, row_count, size_bytes) VALUES(?,?,?,?,?)`,
		segFilename, coldBaseMs, coldBaseMs, 1, 0); err != nil {
		t.Fatalf("insert segment: %v", err)
	}

	opts := logSearchOptions{
		query:     compileLogQuery("customcold"),
		limit:     200,
		chunkSize: 50,
	}

	var received []string
	summary, err := srv.searchLogsViaStore(context.Background(), opts, collectChunks(&received), noopStatus)
	if err != nil {
		t.Fatalf("searchLogsViaStore configured cold dir: %v", err)
	}
	if summary.matched != 1 || len(received) != 1 {
		t.Fatalf("matched=%d received=%d want 1 result from configured cold dir", summary.matched, len(received))
	}
}

// ---------------------------------------------------------------------------
// TestWalkColdSegmentReverse_Order
// ---------------------------------------------------------------------------

func TestWalkColdSegmentReverse_Order(t *testing.T) {
	tmp := t.TempDir()
	segPath := filepath.Join(tmp, "test.log.gz")

	lines := []string{"line-1", "line-2", "line-3", "line-4", "line-5"}
	f, err := os.Create(segPath)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	gz := gzip.NewWriter(f)
	for _, l := range lines {
		_, _ = gz.Write([]byte(l + "\n"))
	}
	_ = gz.Close()
	_ = f.Close()

	var visited []string
	if err := walkColdSegmentReverse(context.Background(), segPath, func(line string) error {
		visited = append(visited, line)
		return nil
	}); err != nil {
		t.Fatalf("walkColdSegmentReverse: %v", err)
	}

	want := []string{"line-5", "line-4", "line-3", "line-2", "line-1"}
	if len(visited) != len(want) {
		t.Fatalf("visited %d lines, want %d", len(visited), len(want))
	}
	for i, w := range want {
		if visited[i] != w {
			t.Errorf("visited[%d]=%q want %q", i, visited[i], w)
		}
	}
}

// ---------------------------------------------------------------------------
// TestWalkColdSegmentReverse_CancelPropagates
// ---------------------------------------------------------------------------

func TestWalkColdSegmentReverse_CancelPropagates(t *testing.T) {
	tmp := t.TempDir()
	segPath := filepath.Join(tmp, "test.log.gz")

	f, err := os.Create(segPath)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	gz := gzip.NewWriter(f)
	for i := 0; i < 10; i++ {
		_, _ = gz.Write([]byte(fmt.Sprintf("line-%d\n", i)))
	}
	_ = gz.Close()
	_ = f.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before walk

	err = walkColdSegmentReverse(ctx, segPath, func(line string) error {
		return nil
	})
	// Should return context error quickly (within first 1000-line check at i=9,8,...,0)
	// With only 10 lines, the check fires at i=0 (i%1000==0).
	if err == nil {
		t.Error("expected context error, got nil")
	}
}

// TestSearchLogsViaStore_SubstringMatchNotFTS5
// Verifies that searching for a bare word matches it as a substring, not just as
// an FTS5 full-word token. "git" must match a line containing "github" even though
// FTS5 would tokenize "github" as a single token that does not equal "git".
func TestSearchLogsViaStore_SubstringMatchNotFTS5(t *testing.T) {
	srv, ls, _ := newSearchTestServer(t)

	now := time.Now().UnixMilli()
	lines := []string{
		"2024/01/01 12:00:01 INFO connecting to github.com/user/repo",
		"2024/01/01 12:00:02 INFO unrelated log line",
	}
	if err := ls.InsertLogs(context.Background(), now, lines); err != nil {
		t.Fatalf("InsertLogs: %v", err)
	}

	opts := logSearchOptions{
		query:     compileLogQuery("git"),
		limit:     200,
		chunkSize: 10,
	}

	var received []string
	summary, err := srv.searchLogsViaStore(context.Background(), opts, collectChunks(&received), noopStatus)
	if err != nil {
		t.Fatalf("searchLogsViaStore: %v", err)
	}
	if summary.matched != 1 {
		t.Errorf("matched=%d want 1 — 'git' should match 'github' as substring", summary.matched)
	}
	if len(received) != 1 || !contains(received[0], "github.com") {
		t.Errorf("expected github.com line in results, got: %v", received)
	}
}
