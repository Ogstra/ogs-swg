package core

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// BackupDBWithColdToTarGz is like BackupDBToTarGz but also appends cold segment
// files (already-compressed .log.gz) into the same tar archive.
// coldFiles is a list of absolute paths; each is stored in the tar under "cold/<basename>".
func BackupDBWithColdToTarGz(ctx context.Context, db *sql.DB, archiveName, destPath string, coldFiles []string) error {
	tmp := destPath + ".vacuum.tmp"
	defer os.Remove(tmp)

	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", tmp); err != nil {
		return fmt.Errorf("db backup vacuum into: %w", err)
	}

	fi, err := os.Stat(tmp)
	if err != nil {
		return fmt.Errorf("db backup stat vacuum file: %w", err)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("db backup create dest: %w", err)
	}
	var writeErr error
	defer func() {
		if writeErr != nil {
			_ = out.Close()
			_ = os.Remove(destPath)
		}
	}()

	gw := gzip.NewWriter(out)
	tw := tar.NewWriter(gw)

	hdr := &tar.Header{Name: archiveName, Size: fi.Size(), Mode: 0600}
	if writeErr = tw.WriteHeader(hdr); writeErr != nil {
		return fmt.Errorf("db backup write tar header: %w", writeErr)
	}
	src, writeErr := os.Open(tmp)
	if writeErr != nil {
		return fmt.Errorf("db backup open vacuum file: %w", writeErr)
	}
	if _, writeErr = io.Copy(tw, src); writeErr != nil {
		src.Close()
		return fmt.Errorf("db backup copy data: %w", writeErr)
	}
	src.Close()

	for _, cf := range coldFiles {
		cfi, err := os.Stat(cf)
		if err != nil {
			continue
		}
		chdr := &tar.Header{Name: filepath.Join("cold", filepath.Base(cf)), Size: cfi.Size(), Mode: 0600}
		if writeErr = tw.WriteHeader(chdr); writeErr != nil {
			return fmt.Errorf("db backup cold header %s: %w", cf, writeErr)
		}
		csrc, err := os.Open(cf)
		if err != nil {
			return fmt.Errorf("db backup open cold %s: %w", cf, err)
		}
		_, writeErr = io.Copy(tw, csrc)
		csrc.Close()
		if writeErr != nil {
			return fmt.Errorf("db backup copy cold %s: %w", cf, writeErr)
		}
	}

	if writeErr = tw.Close(); writeErr != nil {
		return fmt.Errorf("db backup close tar: %w", writeErr)
	}
	if writeErr = gw.Close(); writeErr != nil {
		return fmt.Errorf("db backup close gzip: %w", writeErr)
	}
	if writeErr = out.Close(); writeErr != nil {
		return fmt.Errorf("db backup close file: %w", writeErr)
	}
	writeErr = nil
	return nil
}

// BackupDBToTarGz takes a snapshot of db via VACUUM INTO, then wraps the
// snapshot file in a tar.gz archive written to destPath.
// archiveName is the filename stored inside the tar (e.g. "stats.db").
func BackupDBToTarGz(ctx context.Context, db *sql.DB, archiveName, destPath string) error {
	tmp := destPath + ".vacuum.tmp"
	defer os.Remove(tmp)

	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", tmp); err != nil {
		return fmt.Errorf("db backup vacuum into: %w", err)
	}

	fi, err := os.Stat(tmp)
	if err != nil {
		return fmt.Errorf("db backup stat vacuum file: %w", err)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("db backup create dest: %w", err)
	}
	defer func() {
		// Clean up dest file on error — caller checks return value.
		if err != nil {
			_ = out.Close()
			_ = os.Remove(destPath)
		}
	}()

	gw := gzip.NewWriter(out)
	tw := tar.NewWriter(gw)

	hdr := &tar.Header{
		Name: archiveName,
		Size: fi.Size(),
		Mode: 0600,
	}
	if err = tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("db backup write tar header: %w", err)
	}

	src, err := os.Open(tmp)
	if err != nil {
		return fmt.Errorf("db backup open vacuum file: %w", err)
	}
	defer src.Close()

	if _, err = io.Copy(tw, src); err != nil {
		return fmt.Errorf("db backup copy data: %w", err)
	}

	if err = tw.Close(); err != nil {
		return fmt.Errorf("db backup close tar: %w", err)
	}
	if err = gw.Close(); err != nil {
		return fmt.Errorf("db backup close gzip: %w", err)
	}
	if err = out.Close(); err != nil {
		return fmt.Errorf("db backup close file: %w", err)
	}
	// Clear err so the deferred cleanup does not remove the successfully written file.
	err = nil
	return nil
}
