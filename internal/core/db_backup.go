package core

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
)

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
