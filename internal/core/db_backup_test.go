package core

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestBackupDBToTarGz(t *testing.T) {
	dir := t.TempDir()

	// Create a minimal SQLite DB with one table and one row.
	srcPath := filepath.Join(dir, "test.db")
	db, err := sql.Open("sqlite", srcPath)
	if err != nil {
		t.Fatalf("open src db: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO t(v) VALUES('hello')`); err != nil {
		t.Fatalf("insert: %v", err)
	}

	destPath := filepath.Join(dir, "test.tar.gz")
	archiveName := "backup.db"

	if err := BackupDBToTarGz(context.Background(), db, archiveName, destPath); err != nil {
		t.Fatalf("BackupDBToTarGz: %v", err)
	}
	db.Close()

	// Untar and verify the inner file opens as a valid SQLite DB.
	f, err := os.Open(destPath)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	hdr, err := tr.Next()
	if err != nil {
		t.Fatalf("tar next: %v", err)
	}
	if hdr.Name != archiveName {
		t.Fatalf("expected archive name %q, got %q", archiveName, hdr.Name)
	}

	// Write the inner DB file to a temp path and open it.
	innerPath := filepath.Join(dir, "inner.db")
	innerF, err := os.Create(innerPath)
	if err != nil {
		t.Fatalf("create inner: %v", err)
	}
	if _, err := io.Copy(innerF, tr); err != nil {
		innerF.Close()
		t.Fatalf("copy inner db: %v", err)
	}
	innerF.Close()

	innerDB, err := sql.Open("sqlite", innerPath)
	if err != nil {
		t.Fatalf("open inner db: %v", err)
	}
	defer innerDB.Close()

	var v string
	if err := innerDB.QueryRow(`SELECT v FROM t WHERE id=1`).Scan(&v); err != nil {
		t.Fatalf("query inner db: %v", err)
	}
	if v != "hello" {
		t.Fatalf("expected 'hello', got %q", v)
	}
}
