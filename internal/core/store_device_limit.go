package core

import "context"

// Device limits cap how many distinct HWIDs a subscription serves. 0 on a
// subscription means "use the panel default"; 0 as the default means unlimited.

func (s *Store) GetSubscriptionDeviceLimit(ctx context.Context, subID int64) (int64, error) {
	var limit int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(device_limit, 0) FROM subscriptions WHERE id = ?`, subID).Scan(&limit)
	return limit, err
}

func (s *Store) SetSubscriptionDeviceLimit(ctx context.Context, subID, limit int64) error {
	if limit < 0 {
		limit = 0
	}
	_, err := s.db.ExecContext(ctx, `UPDATE subscriptions SET device_limit = ? WHERE id = ?`, limit, subID)
	return err
}

// ListSubscriptionDeviceLimits returns the per-subscription limits that are set.
func (s *Store) ListSubscriptionDeviceLimits(ctx context.Context) (map[int64]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, COALESCE(device_limit, 0) FROM subscriptions WHERE COALESCE(device_limit, 0) > 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int64{}
	for rows.Next() {
		var id, limit int64
		if err := rows.Scan(&id, &limit); err != nil {
			return nil, err
		}
		out[id] = limit
	}
	return out, rows.Err()
}

// CountKnownHWIDsBySub returns the known device count of every subscription that has any.
func (s *Store) CountKnownHWIDsBySub(ctx context.Context) (map[int64]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT sub_id, COUNT(*) FROM subscription_known_hwids GROUP BY sub_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int64{}
	for rows.Next() {
		var id, n int64
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// HasIncomingHWIDMigration reports whether hwidHash is being moved to targetSubID,
// so the device is let in even when the target is at its device limit.
func (s *Store) HasIncomingHWIDMigration(ctx context.Context, targetSubID int64, hwidHash string) (bool, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM subscription_hwid_migrations
		WHERE target_sub_id = ? AND hwid_hash = ? AND status IN ('pending','delivered')
	`, targetSubID, hwidHash).Scan(&n)
	return n > 0, err
}
