package core

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newIngesterLogStore(t *testing.T) (*LogStore, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test_logs.db")
	store, err := NewLogStore(dbPath)
	if err != nil {
		t.Fatalf("NewLogStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store, dir
}

func TestLogIngester_IngestsAppendedLines(t *testing.T) {
	store, dir := newIngesterLogStore(t)

	logPath := filepath.Join(dir, "access.log")
	// Start with an empty file so the ingester initialises lastSize = 0.
	if err := os.WriteFile(logPath, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}

	ingester := NewLogIngester(logPath, store)
	ingester.Start()
	defer ingester.Stop()

	// Give the first tick a moment to record lastSize = 0 (empty file).
	time.Sleep(150 * time.Millisecond)

	content := "INFO sing-box line one\nINFO sing-box line two\n"
	if err := os.WriteFile(logPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// Wait for at least one poll cycle (1 s + margin).
	time.Sleep(1500 * time.Millisecond)

	rows, err := store.TailHot(context.Background(), 10)
	if err != nil {
		t.Fatalf("TailHot: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows in LogStore, got %d", len(rows))
	}
	if rows[0].Raw != "INFO sing-box line one" {
		t.Errorf("unexpected first row: %q", rows[0].Raw)
	}
	if rows[1].Raw != "INFO sing-box line two" {
		t.Errorf("unexpected second row: %q", rows[1].Raw)
	}
}

func TestLogIngester_HandlesRotation(t *testing.T) {
	store, dir := newIngesterLogStore(t)

	logPath := filepath.Join(dir, "access.log")
	// Start empty so ingester records lastSize = 0 and will ingest from the start.
	if err := os.WriteFile(logPath, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}

	ingester := NewLogIngester(logPath, store)
	ingester.Start()
	defer ingester.Stop()

	// Let one tick fire to latch lastSize = 0.
	time.Sleep(150 * time.Millisecond)

	// Write pre-rotation lines.
	initial := "pre-rotation line one\npre-rotation line two\n"
	if err := os.WriteFile(logPath, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}

	// Allow poll to consume initial content.
	time.Sleep(1500 * time.Millisecond)

	// Simulate rotation: truncate and write new content (file shrinks → smaller size).
	if err := os.WriteFile(logPath, []byte("post-rotation line\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Wait for rotation detection and new-line ingestion.
	time.Sleep(1500 * time.Millisecond)

	rows, err := store.TailHot(context.Background(), 10)
	if err != nil {
		t.Fatalf("TailHot: %v", err)
	}
	// All 3 lines must be present: 2 pre-rotation + 1 post-rotation.
	if len(rows) < 3 {
		t.Fatalf("expected at least 3 rows after rotation, got %d", len(rows))
	}
	last := rows[len(rows)-1].Raw
	if last != "post-rotation line" {
		t.Errorf("expected last row to be post-rotation line, got %q", last)
	}
}

func TestLogIngester_GetActiveUsers(t *testing.T) {
	store, dir := newIngesterLogStore(t)

	logPath := filepath.Join(dir, "access.log")
	if err := os.WriteFile(logPath, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}

	ingester := NewLogIngester(logPath, store)
	ingester.Start()
	defer ingester.Stop()

	time.Sleep(150 * time.Millisecond)

	line := "INFO [tcp] email: alice  inbound connection\n"
	if err := os.WriteFile(logPath, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}

	time.Sleep(1500 * time.Millisecond)

	users := ingester.GetActiveUsers(60)
	if len(users) != 1 || users[0] != "alice" {
		t.Errorf("expected [alice], got %v", users)
	}
}

func TestLogIngester_StartsFromEOF(t *testing.T) {
	store, dir := newIngesterLogStore(t)

	logPath := filepath.Join(dir, "access.log")
	// Pre-populate with 5 lines that must NOT be ingested (D-14).
	pre := "old line 1\nold line 2\nold line 3\nold line 4\nold line 5\n"
	if err := os.WriteFile(logPath, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}

	ingester := NewLogIngester(logPath, store)
	ingester.Start()
	defer ingester.Stop()

	// Give the ingester one tick to latch EOF.
	time.Sleep(1500 * time.Millisecond)

	// Append 2 new lines.
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("new line 1\nnew line 2\n"); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	_ = f.Close()

	time.Sleep(1500 * time.Millisecond)

	rows, err := store.TailHot(context.Background(), 10)
	if err != nil {
		t.Fatalf("TailHot: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected exactly 2 rows (only new lines), got %d", len(rows))
	}
	if rows[0].Raw != "new line 1" {
		t.Errorf("unexpected first row: %q", rows[0].Raw)
	}
	if rows[1].Raw != "new line 2" {
		t.Errorf("unexpected second row: %q", rows[1].Raw)
	}
}
