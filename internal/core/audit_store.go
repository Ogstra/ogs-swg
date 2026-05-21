package core

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// AuditEntry is one row in the audit_log table.
type AuditEntry struct {
	ID       int64  `json:"id"`
	Ts       int64  `json:"ts"`
	Actor    string `json:"actor"`
	IP       string `json:"ip"`
	Action   string `json:"action"`
	Domain   string `json:"domain"`
	EntityID string `json:"entity_id"`
	Detail   string `json:"detail"`
}

// AuditLogPage is the paginated response for GET /api/audit-log.
type AuditLogPage struct {
	Items      []AuditEntry `json:"items"`
	NextOffset int          `json:"next_offset"`
	HasMore    bool         `json:"has_more"`
}

// AuditStore is a separate SQLite DB holding only the audit_log table.
type AuditStore struct {
	db   *sql.DB
	path string
}

// AuditDBPathFor derives the audit DB path from the main DB path.
// e.g. "data/stats.db" → "data/audit.db"
func AuditDBPathFor(mainDBPath string) string {
	dir := filepath.Dir(mainDBPath)
	return filepath.Join(dir, "audit.db")
}

func NewAuditStore(dbPath string) (*AuditStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("audit store open: %w", err)
	}
	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA synchronous=NORMAL;",
		"PRAGMA busy_timeout=5000;",
		"PRAGMA temp_store=MEMORY;",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return nil, fmt.Errorf("audit store pragma %q: %w", p, err)
		}
	}
	if err := initAuditSchema(db); err != nil {
		return nil, fmt.Errorf("audit store schema: %w", err)
	}
	return &AuditStore{db: db, path: dbPath}, nil
}

func initAuditSchema(db *sql.DB) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS audit_log (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    ts        INTEGER NOT NULL,
    actor     TEXT    NOT NULL,
    ip        TEXT    NOT NULL DEFAULT '',
    action    TEXT    NOT NULL,
    domain    TEXT    NOT NULL,
    entity_id TEXT    NOT NULL DEFAULT '',
    detail    TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_audit_log_ts     ON audit_log(ts DESC);
CREATE INDEX IF NOT EXISTS idx_audit_log_domain ON audit_log(domain, ts DESC);
`)
	return err
}

func (a *AuditStore) Close() {
	if a.db != nil {
		_ = a.db.Close()
	}
}

// SizeBytes returns the current on-disk size of the audit DB file.
func (a *AuditStore) SizeBytes() int64 {
	info, err := os.Stat(a.path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// PruneToSize deletes the oldest rows in batches until the file is at or
// below maxBytes, then runs PRAGMA incremental_vacuum to reclaim space.
// Batch size is 500 rows; stops after 50 iterations to bound runtime.
func (a *AuditStore) PruneToSize(maxBytes int64) {
	const batchSize = 500
	const maxIter = 50
	for i := 0; i < maxIter; i++ {
		if a.SizeBytes() <= maxBytes {
			break
		}
		res, err := a.db.Exec(
			`DELETE FROM audit_log WHERE id IN (SELECT id FROM audit_log ORDER BY ts ASC LIMIT ?)`,
			batchSize,
		)
		if err != nil {
			log.Printf("audit prune: delete batch error: %v", err)
			return
		}
		affected, _ := res.RowsAffected()
		if affected == 0 {
			break
		}
	}
	if _, err := a.db.Exec(`PRAGMA incremental_vacuum`); err != nil {
		log.Printf("audit prune: incremental_vacuum error: %v", err)
	}
}

// InsertAuditLog writes a single audit entry. Errors are swallowed by the
// caller — audit failures must never break the primary operation.
func (a *AuditStore) InsertAuditLog(ctx context.Context, e AuditEntry) error {
	_, err := a.db.ExecContext(ctx,
		`INSERT INTO audit_log (ts, actor, ip, action, domain, entity_id, detail)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.Ts, e.Actor, e.IP, e.Action, e.Domain, e.EntityID, e.Detail,
	)
	return err
}

// QueryAuditLog returns paginated audit entries ordered by ts DESC.
// domain and action are optional filters ("" = no filter).
func (a *AuditStore) QueryAuditLog(ctx context.Context, limit, offset int, domain, action string) (AuditLogPage, error) {
	var conds []string
	var args []interface{}
	if domain != "" {
		conds = append(conds, "domain = ?")
		args = append(args, domain)
	}
	if action != "" {
		conds = append(conds, "action = ?")
		args = append(args, action)
	}
	where := "1=1"
	if len(conds) > 0 {
		where = strings.Join(conds, " AND ")
	}

	fetchLimit := limit + 1
	args = append(args, fetchLimit, offset)
	rows, err := a.db.QueryContext(ctx,
		`SELECT id, ts, actor, ip, action, domain, entity_id, detail
		 FROM audit_log
		 WHERE `+where+`
		 ORDER BY ts DESC
		 LIMIT ? OFFSET ?`,
		args...,
	)
	if err != nil {
		return AuditLogPage{}, err
	}
	defer rows.Close()

	items := make([]AuditEntry, 0, limit)
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.Ts, &e.Actor, &e.IP, &e.Action, &e.Domain, &e.EntityID, &e.Detail); err != nil {
			return AuditLogPage{}, err
		}
		items = append(items, e)
	}
	if err := rows.Err(); err != nil {
		return AuditLogPage{}, err
	}

	hasMore := false
	if len(items) > limit {
		hasMore = true
		items = items[:limit]
	}
	return AuditLogPage{
		Items:      items,
		NextOffset: offset + len(items),
		HasMore:    hasMore,
	}, nil
}
