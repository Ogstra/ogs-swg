package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// MaxHWIDMigrationAttempts bounds how many times new-url is re-sent to a
// device that never shows up on the target sub before giving up.
const MaxHWIDMigrationAttempts = 10

// HWIDMigrationAttemptGapSeconds is the minimum spacing between COUNTED
// delivery attempts. new-url is still sent on every request; only the counter
// is rate-limited.
const HWIDMigrationAttemptGapSeconds = 600

type HWIDMigration struct {
	SourceSubID int64  `json:"source_sub_id"`
	HWIDHash    string `json:"hwid_hash"`
	TargetSubID int64  `json:"target_sub_id"`
	Status      string `json:"status"`
	Attempts    int64  `json:"attempts"`
	CreatedAt   int64  `json:"created_at"`
	DeliveredAt int64  `json:"delivered_at"`
	ConfirmedAt int64  `json:"confirmed_at"`
}

const hwidMigrationColumns = `source_sub_id, hwid_hash, target_sub_id, status, attempts, created_at, delivered_at, confirmed_at`

// CreateHWIDMigration (re)arms a migration for one device. Re-creating
// replaces any previous row for the same (source, hwid), resetting it to
// pending, so an operator can retry a failed/cancelled move.
func (s *Store) CreateHWIDMigration(ctx context.Context, sourceSubID int64, hwidHash string, targetSubID int64, now int64) error {
	hwidHash = strings.TrimSpace(hwidHash)
	if hwidHash == "" {
		return errors.New("hwid is required")
	}
	if sourceSubID == targetSubID {
		return errors.New("target subscription must differ from the source")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO subscription_hwid_migrations
			(source_sub_id, hwid_hash, target_sub_id, status, attempts, created_at, delivered_at, confirmed_at)
		VALUES (?, ?, ?, 'pending', 0, ?, 0, 0)
		ON CONFLICT(source_sub_id, hwid_hash) DO UPDATE SET
			target_sub_id = excluded.target_sub_id,
			status = 'pending', attempts = 0,
			created_at = excluded.created_at, delivered_at = 0, confirmed_at = 0
	`, sourceSubID, hwidHash, targetSubID, now)
	if err != nil {
		return fmt.Errorf("create hwid migration: %w", err)
	}
	return nil
}

// GetLiveHWIDMigration returns the migration only while it still needs
// delivering (pending or delivered). Confirmed/failed/cancelled -> nil.
// This is the single indexed lookup on the public request path.
func (s *Store) GetLiveHWIDMigration(ctx context.Context, sourceSubID int64, hwidHash string) (*HWIDMigration, error) {
	var m HWIDMigration
	err := s.db.QueryRowContext(ctx, `
		SELECT `+hwidMigrationColumns+`
		FROM subscription_hwid_migrations
		WHERE source_sub_id = ? AND hwid_hash = ? AND status IN ('pending', 'delivered')
	`, sourceSubID, hwidHash).Scan(&m.SourceSubID, &m.HWIDHash, &m.TargetSubID, &m.Status, &m.Attempts, &m.CreatedAt, &m.DeliveredAt, &m.ConfirmedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// MarkHWIDMigrationDelivered records a new-url emission. It only counts as an
// attempt when the previous counted one is at least
// HWIDMigrationAttemptGapSeconds old, and flips the row to failed once
// MaxHWIDMigrationAttempts is reached. It reports whether THIS call is the one
// that flipped it to failed, so the caller notifies exactly once.
func (s *Store) MarkHWIDMigrationDelivered(ctx context.Context, sourceSubID int64, hwidHash string, now int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE subscription_hwid_migrations
		SET attempts = attempts + 1,
		    delivered_at = ?,
		    status = CASE WHEN attempts + 1 >= ? THEN 'failed' ELSE 'delivered' END
		WHERE source_sub_id = ? AND hwid_hash = ? AND status IN ('pending', 'delivered')
		  AND (attempts = 0 OR ? - delivered_at >= ?)
	`, now, MaxHWIDMigrationAttempts, sourceSubID, hwidHash, now, HWIDMigrationAttemptGapSeconds)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		return false, err
	}
	var status string
	if err := s.db.QueryRowContext(ctx, `SELECT status FROM subscription_hwid_migrations WHERE source_sub_id = ? AND hwid_hash = ?`, sourceSubID, hwidHash).Scan(&status); err != nil {
		return false, err
	}
	return status == "failed", nil
}

// CancelAllHWIDMigrations stops every live migration (operator kill switch).
func (s *Store) CancelAllHWIDMigrations(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE subscription_hwid_migrations SET status = 'cancelled' WHERE status IN ('pending', 'delivered')`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ConfirmHWIDMigration marks every live migration targeting targetSubID for
// this HWID as confirmed. Returns true only for the call that actually flipped
// a row (RowsAffected), so the confirmation fires exactly once.
func (s *Store) ConfirmHWIDMigration(ctx context.Context, targetSubID int64, hwidHash string, now int64) (bool, error) {
	hwidHash = strings.TrimSpace(hwidHash)
	if hwidHash == "" {
		return false, nil
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE subscription_hwid_migrations
		SET status = 'confirmed', confirmed_at = ?
		WHERE target_sub_id = ? AND hwid_hash = ? AND status IN ('pending', 'delivered')
	`, now, targetSubID, hwidHash)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (s *Store) CancelHWIDMigration(ctx context.Context, sourceSubID int64, hwidHash string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE subscription_hwid_migrations SET status = 'cancelled'
		WHERE source_sub_id = ? AND hwid_hash = ? AND status IN ('pending', 'delivered')
	`, sourceSubID, hwidHash)
	return err
}

func (s *Store) ListHWIDMigrations(ctx context.Context, sourceSubID int64) ([]HWIDMigration, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT `+hwidMigrationColumns+`
		FROM subscription_hwid_migrations WHERE source_sub_id = ? ORDER BY created_at DESC
	`, sourceSubID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HWIDMigration{}
	for rows.Next() {
		var m HWIDMigration
		if err := rows.Scan(&m.SourceSubID, &m.HWIDHash, &m.TargetSubID, &m.Status, &m.Attempts, &m.CreatedAt, &m.DeliveredAt, &m.ConfirmedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
