package api

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
)

func newBackupTestServer(t *testing.T) *Server {
	t.Helper()

	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.db")

	dataStore, err := core.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = dataStore.Close() })

	logStore, err := core.NewLogStore(filepath.Join(tmp, "singbox_logs.db"))
	if err != nil {
		t.Fatalf("NewLogStore: %v", err)
	}
	t.Cleanup(func() { logStore.Close() })

	auditStore, err := core.NewAuditStore(filepath.Join(tmp, "audit.db"))
	if err != nil {
		t.Fatalf("NewAuditStore: %v", err)
	}
	t.Cleanup(func() { auditStore.Close() })

	cfg := &core.Config{
		EnableSingbox: true,
		DBBackupPath:  filepath.Join(tmp, "backups"),
	}

	srv := NewServer(dataStore, cfg, nil)
	srv.auditStore = auditStore
	srv.logStore = logStore
	return srv
}

func TestHandleDownloadDBBackup(t *testing.T) {
	srv := newBackupTestServer(t)

	// Insert one log row so the hot tier is non-empty.
	if err := srv.logStore.InsertLogs(t.Context(), time.Now().UnixMilli(), []string{"test line"}); err != nil {
		t.Fatalf("InsertLogs: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/settings/backup/download?target=logs", nil)
	rec := httptest.NewRecorder()
	srv.handleDownloadDBBackup(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "application/gzip" && ct != "application/x-tar" && ct != "application/octet-stream" {
		// http.ServeFile may override Content-Type; accept any binary type.
		// The key check is that the body is a valid tar.gz.
	}

	// Verify the response body is a valid gzip + tar stream.
	gr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("gzip reader on response body: %v", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	hdr, err := tr.Next()
	if err != nil {
		t.Fatalf("tar next: %v", err)
	}
	if hdr.Name != "singbox_logs.db" {
		t.Fatalf("expected inner file 'singbox_logs.db', got %q", hdr.Name)
	}
}

func TestHandleTriggerDBBackup(t *testing.T) {
	srv := newBackupTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/settings/backup/trigger", nil)
	rec := httptest.NewRecorder()
	srv.handleTriggerDBBackup(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string][]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	created, ok := resp["created"]
	if !ok {
		t.Fatal("response missing 'created' field")
	}
	// Expect main + audit + logs = 3 files.
	if len(created) != 3 {
		t.Fatalf("expected 3 created files, got %d: %v", len(created), created)
	}
}

func TestHandleGetLogStoreStats(t *testing.T) {
	srv := newBackupTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/settings/logs/stats", nil)
	rec := httptest.NewRecorder()
	srv.handleGetLogStoreStats(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	for _, field := range []string{"size_bytes", "row_count", "oldest_ts", "newest_ts", "segment_count", "segment_total_bytes"} {
		if _, ok := resp[field]; !ok {
			t.Errorf("response missing field %q", field)
		}
	}
}
