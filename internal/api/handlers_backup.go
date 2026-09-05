package api

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Ogstra/ogs-swg/internal/core"
)

// handleGetLogStoreStats returns hot-tier and cold-segment statistics.
// GET /api/settings/logs/stats
func (s *Server) handleGetLogStoreStats(w http.ResponseWriter, r *http.Request) {
	if s.logStore == nil {
		writeErr(w, http.StatusServiceUnavailable, "log store not available")
		return
	}
	ctx := r.Context()

	rowCount, err := s.logStore.RowCount(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to query row count: "+err.Error())
		return
	}

	sizeBytes := s.logStore.SizeBytes()

	firstMs, lastMs, err := s.logStore.HotDateRange(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to query date range: "+err.Error())
		return
	}

	segCount, segBytes, err := s.logStore.SegmentStats(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to query segment stats: "+err.Error())
		return
	}

	resp := map[string]interface{}{
		"size_bytes":          sizeBytes,
		"row_count":           rowCount,
		"oldest_ts":           firstMs,
		"newest_ts":           lastMs,
		"segment_count":       segCount,
		"segment_total_bytes": segBytes,
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleDownloadDBBackup streams a tar.gz backup of the requested database.
// GET /api/settings/backup/download?target=main|audit|logs[&include_cold=all|N]
// include_cold only applies when target=logs.
func (s *Server) handleDownloadDBBackup(w http.ResponseWriter, r *http.Request) {
	target := r.URL.Query().Get("target")

	db, archiveName, filename, err := s.resolveBackupTarget(r.Context(), target)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	tmp, err := os.CreateTemp("", "ogs-db-backup-*.tar.gz")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create temp file: "+err.Error())
		return
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	var coldFiles []string
	if target == "logs" && s.logStore != nil {
		coldFiles = s.resolveColdFiles(r.Context(), r.URL.Query().Get("include_cold"))
	}

	if len(coldFiles) > 0 {
		err = core.BackupDBWithColdToTarGz(r.Context(), db, archiveName, tmpPath, coldFiles)
	} else {
		err = core.BackupDBToTarGz(r.Context(), db, archiveName, tmpPath)
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "backup failed: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	http.ServeFile(w, r, tmpPath)
}

// resolveColdFiles returns absolute paths for cold segment files based on the
// include_cold param: "all" → all segments, "N" (integer) → last N newest, else none.
func (s *Server) resolveColdFiles(ctx context.Context, param string) []string {
	if param == "" || param == "none" || s.logStore == nil {
		return nil
	}
	limit := -1
	if param != "all" {
		n, err := strconv.Atoi(param)
		if err != nil || n <= 0 {
			return nil
		}
		limit = n
	}
	segs, err := s.logStore.ListSegments(ctx, limit)
	if err != nil || len(segs) == 0 {
		return nil
	}
	coldDir := s.logColdDir()
	paths := make([]string, 0, len(segs))
	for _, seg := range segs {
		paths = append(paths, filepath.Join(coldDir, seg.Filename))
	}
	return paths
}

// handleTriggerDBBackup writes backup archives for all three databases to DBBackupPath.
// POST /api/settings/backup/trigger
func (s *Server) handleTriggerDBBackup(w http.ResponseWriter, r *http.Request) {
	if s.config.DemoMode {
		writeErr(w, http.StatusForbidden, "not available in demo mode")
		return
	}

	backupDir := s.config.DBBackupPath
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		writeErr(w, http.StatusInternalServerError, "failed to create backup dir: "+err.Error())
		return
	}

	ctx := r.Context()
	ts := time.Now().Format("2006-01-02_150405")
	var created []string

	// Main DB
	mainName := fmt.Sprintf("ogs_%s.tar.gz", ts)
	if err := core.BackupDBToTarGz(ctx, s.store.DB(), "stats.db", filepath.Join(backupDir, mainName)); err != nil {
		log.Printf("DB backup (main): %v", err)
	} else {
		created = append(created, mainName)
	}

	// Audit DB
	if s.auditStore != nil {
		auditName := fmt.Sprintf("audit_%s.tar.gz", ts)
		if err := core.BackupDBToTarGz(ctx, s.auditStore.DB(), "audit.db", filepath.Join(backupDir, auditName)); err != nil {
			log.Printf("DB backup (audit): %v", err)
		} else {
			created = append(created, auditName)
		}
	}

	// Log DB
	if s.logStore != nil {
		logName := logBackupFilename(ctx, s.logStore)
		if err := core.BackupDBToTarGz(ctx, s.logStore.DB(), "singbox_logs.db", filepath.Join(backupDir, logName)); err != nil {
			log.Printf("DB backup (logs): %v", err)
		} else {
			created = append(created, logName)
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"created": created})
}

// resolveBackupTarget returns the *sql.DB, inner archive name, and download filename
// for the given target string ("main", "audit", "logs").
func (s *Server) resolveBackupTarget(ctx context.Context, target string) (*sql.DB, string, string, error) {
	ts := time.Now().Format("2006-01-02_150405")
	switch target {
	case "main":
		return s.store.DB(), "stats.db", fmt.Sprintf("ogs_%s.tar.gz", ts), nil
	case "audit":
		if s.auditStore == nil {
			return nil, "", "", fmt.Errorf("audit store not available")
		}
		return s.auditStore.DB(), "audit.db", fmt.Sprintf("audit_%s.tar.gz", ts), nil
	case "logs":
		if s.logStore == nil {
			return nil, "", "", fmt.Errorf("log store not available")
		}
		return s.logStore.DB(), "singbox_logs.db", logBackupFilename(ctx, s.logStore), nil
	default:
		return nil, "", "", fmt.Errorf("target must be main, audit, or logs")
	}
}

// logBackupFilename builds the filename for a log DB backup using the hot date range.
func logBackupFilename(ctx context.Context, ls *core.LogStore) string {
	firstMs, lastMs, err := ls.HotDateRange(ctx)
	if err != nil || (firstMs == 0 && lastMs == 0) {
		return fmt.Sprintf("singbox_logs_empty_%s.tar.gz", time.Now().Format("2006-01-02"))
	}
	return fmt.Sprintf("singbox_logs_%s_%s.tar.gz",
		time.UnixMilli(firstMs).Format("2006-01-02"),
		time.UnixMilli(lastMs).Format("2006-01-02"))
}
