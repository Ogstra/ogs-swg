package core

import (
	"context"
	"time"
)

// GetUserQuotaAnchor returns the unix time since which a user's usage counts
// after a manual reset, or 0 if it was never reset.
func (s *Store) GetUserQuotaAnchor(email string) (int64, error) {
	var anchor int64
	err := s.db.QueryRowContext(context.Background(), `SELECT COALESCE(quota_anchor, 0) FROM users WHERE email = ?`, email).Scan(&anchor)
	if err != nil {
		return 0, nil // no row yet: never reset
	}
	return anchor, nil
}

// ListUserQuotaAnchors returns the manual reset time of every user that has one.
func (s *Store) ListUserQuotaAnchors() (map[string]int64, error) {
	rows, err := s.db.QueryContext(context.Background(), `SELECT email, quota_anchor FROM users WHERE COALESCE(quota_anchor, 0) > 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var email string
		var anchor int64
		if err := rows.Scan(&email, &anchor); err != nil {
			return nil, err
		}
		out[email] = anchor
	}
	return out, rows.Err()
}

// ResetUserUsage starts counting a user's usage from now. Recorded traffic is
// kept; only what counts against the quota changes.
func (s *Store) ResetUserUsage(email string, now time.Time) error {
	_, err := s.db.ExecContext(context.Background(), `UPDATE users SET quota_anchor = ? WHERE email = ?`, now.Unix(), email)
	return err
}
