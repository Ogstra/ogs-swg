package core

import (
	"context"
	"time"
)

// A user can expire on a fixed date (ExpiresAt) or a number of days after the
// first traffic they produce (ExpireAfterDays). An expired user is treated like
// one over quota: the sampler removes them from the live config, and extending
// the date brings them back through the same path.

// UserExpiry is a user's expiration settings. Zero values mean "never expires".
type UserExpiry struct {
	ExpiresAt       int64 `json:"expires_at"`
	ExpireAfterDays int64 `json:"expire_after_days"`
}

func (e UserExpiry) Expired(now time.Time) bool {
	return e.ExpiresAt > 0 && now.Unix() >= e.ExpiresAt
}

func (s *Store) GetUserExpiry(email string) (UserExpiry, error) {
	var e UserExpiry
	err := s.db.QueryRowContext(context.Background(),
		`SELECT COALESCE(expires_at, 0), COALESCE(expire_after_days, 0) FROM users WHERE email = ?`, email).
		Scan(&e.ExpiresAt, &e.ExpireAfterDays)
	if err != nil {
		return UserExpiry{}, nil // no row yet: nothing set
	}
	return e, nil
}

// ListUserExpiries returns the expiration settings of every user that has any.
func (s *Store) ListUserExpiries() (map[string]UserExpiry, error) {
	rows, err := s.db.QueryContext(context.Background(),
		`SELECT email, COALESCE(expires_at, 0), COALESCE(expire_after_days, 0) FROM users WHERE COALESCE(expires_at, 0) > 0 OR COALESCE(expire_after_days, 0) > 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]UserExpiry{}
	for rows.Next() {
		var email string
		var e UserExpiry
		if err := rows.Scan(&email, &e.ExpiresAt, &e.ExpireAfterDays); err != nil {
			return nil, err
		}
		out[email] = e
	}
	return out, rows.Err()
}

func (s *Store) SetUserExpiry(email string, e UserExpiry) error {
	if e.ExpiresAt < 0 {
		e.ExpiresAt = 0
	}
	if e.ExpireAfterDays < 0 {
		e.ExpireAfterDays = 0
	}
	_, err := s.db.ExecContext(context.Background(),
		`UPDATE users SET expires_at = ?, expire_after_days = ? WHERE email = ?`, e.ExpiresAt, e.ExpireAfterDays, email)
	return err
}

// activateExpiryOnFirstUse starts the countdown for users configured to expire
// some days after their first traffic. It reports whether it set a date.
func (s *Store) activateExpiryOnFirstUse(email string, now time.Time) (bool, error) {
	e, err := s.GetUserExpiry(email)
	if err != nil || e.ExpireAfterDays <= 0 || e.ExpiresAt > 0 {
		return false, err
	}
	samples, err := s.GetCombinedReport(email, 0, now.Unix())
	if err != nil {
		return false, err
	}
	for _, sample := range samples {
		if sample.Uplink+sample.Downlink > 0 {
			e.ExpiresAt = now.Add(time.Duration(e.ExpireAfterDays) * 24 * time.Hour).Unix()
			return true, s.SetUserExpiry(email, e)
		}
	}
	return false, nil
}
