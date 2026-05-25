package core

import (
	"bufio"
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// LogRow is one row in the singbox_logs table.
type LogRow struct {
	ID    int64  `json:"id"`
	Ts    int64  `json:"ts"`   // unix ms
	Raw   string `json:"raw"`
	Level string `json:"level"`
	User  string `json:"user"`
}

// LogSegment is one row in the log_segments table.
type LogSegment struct {
	ID        int64  `json:"id"`
	Filename  string `json:"filename"`
	StartTs   int64  `json:"start_ts"`
	EndTs     int64  `json:"end_ts"`
	RowCount  int64  `json:"row_count"`
	SizeBytes int64  `json:"size_bytes"`
}

// ErrStopLogWalk is returned by a WalkHot visitor to stop iteration early.
var ErrStopLogWalk = errors.New("stop log walk")

// LogStore owns the singbox_logs.db SQLite database.
type LogStore struct {
	db   *sql.DB
	path string
}

// LogDBPathFor derives the log DB path from the main DB path.
// e.g. "data/stats.db" → "data/singbox_logs.db"
func LogDBPathFor(mainDBPath string) string {
	dir := filepath.Dir(mainDBPath)
	return filepath.Join(dir, "singbox_logs.db")
}

// NewLogStore opens (or creates) the log database at dbPath, applies pragmas, and
// initialises the schema.
func NewLogStore(dbPath string) (*LogStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("log store mkdir: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("log store open: %w", err)
	}
	// Single connection — WAL handles concurrent reads safely.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA synchronous=NORMAL;",
		"PRAGMA busy_timeout=5000;",
		"PRAGMA temp_store=MEMORY;",
		"PRAGMA auto_vacuum=INCREMENTAL;",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("log store pragma %q: %w", p, err)
		}
	}
	if err := initLogSchema(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("log store schema: %w", err)
	}
	return &LogStore{db: db, path: dbPath}, nil
}

func initLogSchema(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS singbox_logs (
    id    INTEGER PRIMARY KEY AUTOINCREMENT,
    ts    INTEGER NOT NULL,
    raw   TEXT    NOT NULL,
    level TEXT    NOT NULL DEFAULT '',
    user  TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_singbox_logs_ts ON singbox_logs(ts DESC);

CREATE VIRTUAL TABLE IF NOT EXISTS singbox_logs_fts
USING fts5(raw, content='singbox_logs', content_rowid='id', tokenize='unicode61');

CREATE TABLE IF NOT EXISTS log_segments (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    filename   TEXT    NOT NULL,
    start_ts   INTEGER NOT NULL,
    end_ts     INTEGER NOT NULL,
    row_count  INTEGER NOT NULL DEFAULT 0,
    size_bytes INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_log_segments_ts ON log_segments(start_ts, end_ts);
`)
	return err
}

// Close closes the database connection.
func (l *LogStore) Close() {
	if l.db != nil {
		_ = l.db.Close()
	}
}

// DB returns the underlying *sql.DB (used by the backup system in 49-06).
func (l *LogStore) DB() *sql.DB { return l.db }

// RowCount returns the total number of rows in singbox_logs.
func (l *LogStore) RowCount(ctx context.Context) (int64, error) {
	var n int64
	err := l.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM singbox_logs`).Scan(&n)
	return n, err
}

// SegmentStats returns the count and total size_bytes of all rows in log_segments.
func (l *LogStore) SegmentStats(ctx context.Context) (count int64, totalBytes int64, err error) {
	err = l.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(size_bytes), 0) FROM log_segments`).
		Scan(&count, &totalBytes)
	return count, totalBytes, err
}

// SizeBytes returns the current on-disk size of the log DB file.
func (l *LogStore) SizeBytes() int64 {
	info, err := os.Stat(l.path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// --------------------------------------------------------------------------
// Parsing helpers
// --------------------------------------------------------------------------

var (
	logLevelRe = regexp.MustCompile(`(?i)\b(INFO|WARN|WARNING|ERROR|DEBUG|FATAL|TRACE)\b`)
	// Matches ": [username]  inbound connection" or ": [username]  inbound packet connection"
	logUserRe = regexp.MustCompile(`:\s*(\[[^\[\]]+\])\s+inbound(?: packet)? connection\b`)
)

// parseLogMeta extracts level and user from a raw sing-box log line.
func parseLogMeta(line string) (level, user string) {
	if m := logLevelRe.FindString(line); m != "" {
		level = strings.ToUpper(m)
		if level == "WARNING" {
			level = "WARN"
		}
	}
	if m := logUserRe.FindStringSubmatch(line); len(m) >= 2 {
		user = m[1]
	}
	return level, user
}

// --------------------------------------------------------------------------
// Write methods
// --------------------------------------------------------------------------

// InsertLogs inserts a batch of raw log lines in one transaction. ts is unix ms
// (caller supplies time.Now().UnixMilli()). Both singbox_logs and the FTS5
// content table are kept in sync.
func (l *LogStore) InsertLogs(ctx context.Context, ts int64, lines []string) error {
	if len(lines) == 0 {
		return nil
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("log insert begin: %w", err)
	}
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO singbox_logs(ts, raw, level, user) VALUES(?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("log insert prepare main: %w", err)
	}
	defer stmt.Close()

	ftsStmt, err := tx.PrepareContext(ctx,
		`INSERT INTO singbox_logs_fts(rowid, raw) VALUES(?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("log insert prepare fts: %w", err)
	}
	defer ftsStmt.Close()

	for _, line := range lines {
		lvl, usr := parseLogMeta(line)
		res, err := stmt.ExecContext(ctx, ts, line, lvl, usr)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("log insert exec: %w", err)
		}
		id, _ := res.LastInsertId()
		if _, err := ftsStmt.ExecContext(ctx, id, line); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("log fts insert exec: %w", err)
		}
	}
	return tx.Commit()
}

// --------------------------------------------------------------------------
// Read methods
// --------------------------------------------------------------------------

// TailHot returns the most recent limit rows in chronological order (oldest first).
func (l *LogStore) TailHot(ctx context.Context, limit int) ([]LogRow, error) {
	rows, err := l.db.QueryContext(ctx,
		`SELECT id, ts, raw, level, user FROM singbox_logs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []LogRow
	for rows.Next() {
		var r LogRow
		if err := rows.Scan(&r.ID, &r.Ts, &r.Raw, &r.Level, &r.User); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Reverse to chronological order.
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result, nil
}

// PollAfterID returns up to limit rows with id > afterID, ordered id ASC.
// Used for live-tail incremental polling.
func (l *LogStore) PollAfterID(ctx context.Context, afterID int64, limit int) ([]LogRow, error) {
	rows, err := l.db.QueryContext(ctx,
		`SELECT id, ts, raw, level, user FROM singbox_logs WHERE id > ? ORDER BY id ASC LIMIT ?`,
		afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []LogRow
	for rows.Next() {
		var r LogRow
		if err := rows.Scan(&r.ID, &r.Ts, &r.Raw, &r.Level, &r.User); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// OldestHotTs returns the smallest ts currently in singbox_logs (0 if empty).
func (l *LogStore) OldestHotTs(ctx context.Context) (int64, error) {
	var ts sql.NullInt64
	err := l.db.QueryRowContext(ctx, `SELECT MIN(ts) FROM singbox_logs`).Scan(&ts)
	if err != nil {
		return 0, err
	}
	if !ts.Valid {
		return 0, nil
	}
	return ts.Int64, nil
}

// HotDateRange returns MIN(ts) and MAX(ts) from singbox_logs.
// Both are 0 if the table is empty. Used by backup filename generation.
func (l *LogStore) HotDateRange(ctx context.Context) (firstMs, lastMs int64, err error) {
	var minTs, maxTs sql.NullInt64
	err = l.db.QueryRowContext(ctx, `SELECT MIN(ts), MAX(ts) FROM singbox_logs`).Scan(&minTs, &maxTs)
	if err != nil {
		return 0, 0, err
	}
	if minTs.Valid {
		firstMs = minTs.Int64
	}
	if maxTs.Valid {
		lastMs = maxTs.Int64
	}
	return firstMs, lastMs, nil
}

// --------------------------------------------------------------------------
// Search / walk
// --------------------------------------------------------------------------

// WalkHot streams rows matching the time range and optional simple text in
// newest-first order. If simpleText != "", uses FTS5 MATCH with the term
// double-quoted (double-quote chars inside the term are escaped as "").
// If simpleText == "", scans singbox_logs with the ts filter only.
// visit may return ErrStopLogWalk to stop early; any other error is propagated.
// fromMs and toMs are unix ms; pass 0 to disable the respective bound.
func (l *LogStore) WalkHot(ctx context.Context, simpleText string, fromMs, toMs int64, visit func(LogRow) error) error {
	var (
		sqlRows *sql.Rows
		err     error
	)

	if simpleText != "" {
		term := `"` + strings.ReplaceAll(simpleText, `"`, `""`) + `"`
		switch {
		case fromMs > 0 && toMs > 0:
			sqlRows, err = l.db.QueryContext(ctx,
				`SELECT s.id, s.ts, s.raw, s.level, s.user
				 FROM singbox_logs_fts f
				 JOIN singbox_logs s ON s.id = f.rowid
				 WHERE singbox_logs_fts MATCH ?
				   AND s.ts >= ? AND s.ts <= ?
				 ORDER BY s.id DESC`,
				term, fromMs, toMs)
		case fromMs > 0:
			sqlRows, err = l.db.QueryContext(ctx,
				`SELECT s.id, s.ts, s.raw, s.level, s.user
				 FROM singbox_logs_fts f
				 JOIN singbox_logs s ON s.id = f.rowid
				 WHERE singbox_logs_fts MATCH ?
				   AND s.ts >= ?
				 ORDER BY s.id DESC`,
				term, fromMs)
		case toMs > 0:
			sqlRows, err = l.db.QueryContext(ctx,
				`SELECT s.id, s.ts, s.raw, s.level, s.user
				 FROM singbox_logs_fts f
				 JOIN singbox_logs s ON s.id = f.rowid
				 WHERE singbox_logs_fts MATCH ?
				   AND s.ts <= ?
				 ORDER BY s.id DESC`,
				term, toMs)
		default:
			sqlRows, err = l.db.QueryContext(ctx,
				`SELECT s.id, s.ts, s.raw, s.level, s.user
				 FROM singbox_logs_fts f
				 JOIN singbox_logs s ON s.id = f.rowid
				 WHERE singbox_logs_fts MATCH ?
				 ORDER BY s.id DESC`,
				term)
		}
	} else {
		switch {
		case fromMs > 0 && toMs > 0:
			sqlRows, err = l.db.QueryContext(ctx,
				`SELECT id, ts, raw, level, user FROM singbox_logs
				 WHERE ts >= ? AND ts <= ?
				 ORDER BY id DESC`,
				fromMs, toMs)
		case fromMs > 0:
			sqlRows, err = l.db.QueryContext(ctx,
				`SELECT id, ts, raw, level, user FROM singbox_logs
				 WHERE ts >= ?
				 ORDER BY id DESC`,
				fromMs)
		case toMs > 0:
			sqlRows, err = l.db.QueryContext(ctx,
				`SELECT id, ts, raw, level, user FROM singbox_logs
				 WHERE ts <= ?
				 ORDER BY id DESC`,
				toMs)
		default:
			sqlRows, err = l.db.QueryContext(ctx,
				`SELECT id, ts, raw, level, user FROM singbox_logs ORDER BY id DESC`)
		}
	}
	if err != nil {
		return err
	}
	defer sqlRows.Close()

	for sqlRows.Next() {
		var r LogRow
		if err := sqlRows.Scan(&r.ID, &r.Ts, &r.Raw, &r.Level, &r.User); err != nil {
			return err
		}
		if err := visit(r); err != nil {
			if errors.Is(err, ErrStopLogWalk) {
				return nil
			}
			return err
		}
	}
	return sqlRows.Err()
}

// SegmentsInRange returns segments whose [start_ts, end_ts] overlaps [fromMs, toMs],
// ordered start_ts DESC. If both fromMs and toMs are <= 0, all segments are returned.
func (l *LogStore) SegmentsInRange(ctx context.Context, fromMs, toMs int64) ([]LogSegment, error) {
	var (
		sqlRows *sql.Rows
		err     error
	)
	switch {
	case fromMs <= 0 && toMs <= 0:
		// No range: return all segments.
		sqlRows, err = l.db.QueryContext(ctx,
			`SELECT id, filename, start_ts, end_ts, row_count, size_bytes
			 FROM log_segments ORDER BY start_ts DESC`)
	case fromMs > 0 && toMs > 0:
		// Overlap: seg.start_ts <= toMs AND seg.end_ts >= fromMs
		sqlRows, err = l.db.QueryContext(ctx,
			`SELECT id, filename, start_ts, end_ts, row_count, size_bytes
			 FROM log_segments
			 WHERE start_ts <= ? AND end_ts >= ?
			 ORDER BY start_ts DESC`,
			toMs, fromMs)
	case fromMs > 0:
		// No upper bound: any segment with end_ts >= fromMs overlaps.
		sqlRows, err = l.db.QueryContext(ctx,
			`SELECT id, filename, start_ts, end_ts, row_count, size_bytes
			 FROM log_segments
			 WHERE end_ts >= ?
			 ORDER BY start_ts DESC`,
			fromMs)
	default:
		// Only toMs set: any segment with start_ts <= toMs overlaps.
		sqlRows, err = l.db.QueryContext(ctx,
			`SELECT id, filename, start_ts, end_ts, row_count, size_bytes
			 FROM log_segments
			 WHERE start_ts <= ?
			 ORDER BY start_ts DESC`,
			toMs)
	}
	if err != nil {
		return nil, err
	}
	defer sqlRows.Close()

	var result []LogSegment
	for sqlRows.Next() {
		var s LogSegment
		if err := sqlRows.Scan(&s.ID, &s.Filename, &s.StartTs, &s.EndTs, &s.RowCount, &s.SizeBytes); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, sqlRows.Err()
}

// --------------------------------------------------------------------------
// Cold export and retention
// --------------------------------------------------------------------------

// ListSegments returns segments ordered by start_ts DESC (newest first).
// limit <= 0 returns all segments.
func (l *LogStore) ListSegments(ctx context.Context, limit int) ([]LogSegment, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if limit > 0 {
		rows, err = l.db.QueryContext(ctx,
			`SELECT id, filename, start_ts, end_ts, row_count, size_bytes
			 FROM log_segments ORDER BY start_ts DESC LIMIT ?`, limit)
	} else {
		rows, err = l.db.QueryContext(ctx,
			`SELECT id, filename, start_ts, end_ts, row_count, size_bytes
			 FROM log_segments ORDER BY start_ts DESC`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var segs []LogSegment
	for rows.Next() {
		var s LogSegment
		if err := rows.Scan(&s.ID, &s.Filename, &s.StartTs, &s.EndTs, &s.RowCount, &s.SizeBytes); err != nil {
			return nil, err
		}
		segs = append(segs, s)
	}
	return segs, rows.Err()
}

// ExportToCold streams rows with id <= maxID into coldDir/singbox_YYYYMMDD-YYYYMMDD.log.gz,
// inserts a log_segments row, deletes the exported rows from singbox_logs and
// singbox_logs_fts in one transaction, then runs PRAGMA incremental_vacuum.
// Returns the created segment. Returns nil, nil if there are no rows to export.
func (l *LogStore) ExportToCold(ctx context.Context, coldDir string, maxID int64) (*LogSegment, error) {
	if err := os.MkdirAll(coldDir, 0o755); err != nil {
		return nil, fmt.Errorf("export cold mkdir: %w", err)
	}

	// Stream rows to gzip file while collecting stats.
	qrows, err := l.db.QueryContext(ctx,
		`SELECT id, ts, raw FROM singbox_logs WHERE id <= ? ORDER BY ts ASC`, maxID)
	if err != nil {
		return nil, fmt.Errorf("export cold query: %w", err)
	}

	var (
		minTs    int64 = -1
		maxTs    int64
		rowCount int64
		ids      []int64
	)

	// Write to a temp file first; rename after success.
	tmpPath := filepath.Join(coldDir, fmt.Sprintf(".singbox_export_%d.log.gz.tmp", time.Now().UnixNano()))
	f, err := os.Create(tmpPath)
	if err != nil {
		qrows.Close()
		return nil, fmt.Errorf("export cold create tmp: %w", err)
	}
	gz := gzip.NewWriter(f)
	bw := bufio.NewWriter(gz)

	for qrows.Next() {
		var id, ts int64
		var raw string
		if err := qrows.Scan(&id, &ts, &raw); err != nil {
			qrows.Close()
			_ = gz.Close()
			_ = f.Close()
			_ = os.Remove(tmpPath)
			return nil, fmt.Errorf("export cold scan: %w", err)
		}
		if _, err := bw.WriteString(raw + "\n"); err != nil {
			qrows.Close()
			_ = gz.Close()
			_ = f.Close()
			_ = os.Remove(tmpPath)
			return nil, fmt.Errorf("export cold write: %w", err)
		}
		ids = append(ids, id)
		if minTs < 0 || ts < minTs {
			minTs = ts
		}
		if ts > maxTs {
			maxTs = ts
		}
		rowCount++
	}
	if err := qrows.Err(); err != nil {
		_ = gz.Close()
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("export cold rows err: %w", err)
	}
	qrows.Close()

	if rowCount == 0 {
		_ = gz.Close()
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return nil, nil
	}

	if err := bw.Flush(); err != nil {
		_ = gz.Close()
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("export cold flush: %w", err)
	}
	if err := gz.Close(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("export cold gz close: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("export cold file close: %w", err)
	}

	fi, err := os.Stat(tmpPath)
	if err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("export cold stat: %w", err)
	}

	// Build the final filename: singbox_YYYYMMDD-YYYYMMDD.log.gz
	startDay := time.UnixMilli(minTs).UTC().Format("20060102")
	endDay := time.UnixMilli(maxTs).UTC().Format("20060102")
	baseName := fmt.Sprintf("singbox_%s-%s.log.gz", startDay, endDay)
	destPath := filepath.Join(coldDir, baseName)
	// Avoid overwriting an existing file by appending a suffix.
	if _, err := os.Stat(destPath); err == nil {
		for n := 2; ; n++ {
			candidate := filepath.Join(coldDir, fmt.Sprintf("singbox_%s-%s-%d.log.gz", startDay, endDay, n))
			if _, err := os.Stat(candidate); err != nil {
				destPath = candidate
				baseName = filepath.Base(candidate)
				break
			}
		}
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("export cold rename: %w", err)
	}

	// Atomically record segment + delete rows.
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("export cold tx begin: %w", err)
	}

	var segID int64
	if err := tx.QueryRowContext(ctx,
		`INSERT INTO log_segments(filename, start_ts, end_ts, row_count, size_bytes)
		 VALUES(?, ?, ?, ?, ?) RETURNING id`,
		baseName, minTs, maxTs, rowCount, fi.Size(),
	).Scan(&segID); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("export cold insert segment: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM singbox_logs_fts WHERE rowid IN (SELECT id FROM singbox_logs WHERE id <= ?)`, maxID,
	); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("export cold delete fts: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM singbox_logs WHERE id <= ?`, maxID,
	); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("export cold delete rows: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("export cold commit: %w", err)
	}

	if _, err := l.db.ExecContext(ctx, `PRAGMA incremental_vacuum`); err != nil {
		log.Printf("log store: incremental_vacuum after export: %v", err)
	}

	return &LogSegment{
		ID:        segID,
		Filename:  baseName,
		StartTs:   minTs,
		EndTs:     maxTs,
		RowCount:  rowCount,
		SizeBytes: fi.Size(),
	}, nil
}

// CheckRetention inspects the current hot tier and exports old rows if the
// configured threshold is exceeded.
//
// mode "size": export oldest ~50% of rows when SizeBytes() > maxMB*1024*1024.
// mode "time": export rows with ts < now - duration(days, unit) where unit is
//
//	"days", "weeks", or "months".
//
// Returns the created segment if anything was exported (nil, nil otherwise).
func (l *LogStore) CheckRetention(ctx context.Context, mode string, maxMB, days int, unit, coldDir string) (*LogSegment, error) {
	switch mode {
	case "size":
		limit := int64(maxMB) * 1024 * 1024
		if l.SizeBytes() <= limit {
			return nil, nil
		}
		// Find the id at the 50th-percentile (export oldest half).
		var maxID sql.NullInt64
		err := l.db.QueryRowContext(ctx,
			`SELECT id FROM singbox_logs ORDER BY id ASC LIMIT 1 OFFSET (
				SELECT COUNT(*)/2 FROM singbox_logs
			)`).Scan(&maxID)
		if err != nil || !maxID.Valid {
			return nil, nil
		}
		return l.ExportToCold(ctx, coldDir, maxID.Int64)

	case "time":
		if days <= 0 {
			return nil, nil
		}
		var cutoff time.Time
		now := time.Now()
		switch unit {
		case "weeks":
			cutoff = now.AddDate(0, 0, -days*7)
		case "months":
			cutoff = now.AddDate(0, -days, 0)
		default: // "days"
			cutoff = now.AddDate(0, 0, -days)
		}
		cutoffMs := cutoff.UnixMilli()

		var maxID sql.NullInt64
		err := l.db.QueryRowContext(ctx,
			`SELECT MAX(id) FROM singbox_logs WHERE ts < ?`, cutoffMs).Scan(&maxID)
		if err != nil || !maxID.Valid {
			return nil, nil
		}
		return l.ExportToCold(ctx, coldDir, maxID.Int64)

	default:
		return nil, fmt.Errorf("unknown retention mode: %q", mode)
	}
}
