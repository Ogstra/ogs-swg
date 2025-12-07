package core

import (
	"database/sql"
	"time"

	_ "modernc.org/sqlite"
)

type Sample struct {
	User      string
	Timestamp int64
	Uplink    int64
	Downlink  int64
}

type Store struct {
	db *sql.DB
}

func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	db.Exec("PRAGMA journal_mode=WAL;")
	db.Exec("PRAGMA synchronous=NORMAL;")
	db.Exec("PRAGMA busy_timeout=5000;")
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := db.Ping(); err != nil {
		return nil, err
	}

	s := &Store{db: db}
	if err := s.initSchema(); err != nil {
		return nil, err
	}

	return s, nil
}

type UserMetadata struct {
	Email       string `json:"email"`
	QuotaLimit  int64  `json:"quota_limit"`
	QuotaPeriod string `json:"quota_period"`
	ResetDay    int    `json:"reset_day"`
	Enabled     bool   `json:"enabled"`
}

func (s *Store) initSchema() error {
	query := `
	CREATE TABLE IF NOT EXISTS samples (
		user TEXT NOT NULL,
		ts   INTEGER NOT NULL,
		uplink   INTEGER NOT NULL,
		downlink INTEGER NOT NULL
	);
	CREATE UNIQUE INDEX IF NOT EXISTS uq_samples_user_ts ON samples(user, ts);
	CREATE INDEX IF NOT EXISTS idx_samples_user_ts ON samples(user, ts);

	CREATE TABLE IF NOT EXISTS users (
		email TEXT PRIMARY KEY,
		quota_limit INTEGER DEFAULT 0,
		quota_period TEXT DEFAULT 'monthly',
		reset_day INTEGER DEFAULT 1,
		enabled INTEGER DEFAULT 1
	);
	CREATE TABLE IF NOT EXISTS import_state (
		key TEXT PRIMARY KEY,
		value TEXT
	);
	CREATE TABLE IF NOT EXISTS sampler_runs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		ts INTEGER NOT NULL,
		duration_ms INTEGER NOT NULL,
		inserted INTEGER NOT NULL,
		error TEXT
	);
	`
	if _, err := s.db.Exec(query); err != nil {
		return err
	}
	s.db.Exec("ALTER TABLE users ADD COLUMN enabled INTEGER DEFAULT 1;")
	return nil
}

func (s *Store) HasSamples() (bool, error) {
	var count int64
	if err := s.db.QueryRow("SELECT COUNT(1) FROM samples").Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *Store) TruncateSamples() error {
	_, err := s.db.Exec("DELETE FROM samples")
	return err
}

func (s *Store) GetMaxTimestamp() (int64, error) {
	var ts sql.NullInt64
	if err := s.db.QueryRow("SELECT MAX(ts) FROM samples").Scan(&ts); err != nil {
		return 0, err
	}
	if ts.Valid {
		return ts.Int64, nil
	}
	return 0, nil
}

func (s *Store) GetMaxTimestampForUser(user string) (int64, error) {
	var ts sql.NullInt64
	if err := s.db.QueryRow("SELECT MAX(ts) FROM samples WHERE user = ?", user).Scan(&ts); err != nil {
		return 0, err
	}
	if ts.Valid {
		return ts.Int64, nil
	}
	return 0, nil
}

func (s *Store) PruneOlderThan(ts int64) (int64, error) {
	res, err := s.db.Exec("DELETE FROM samples WHERE ts < ?", ts)
	if err != nil {
		return 0, err
	}
	affected, _ := res.RowsAffected()
	return affected, nil
}

func (s *Store) CountSamples() (int64, error) {
	var count int64
	if err := s.db.QueryRow("SELECT COUNT(*) FROM samples").Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

type SamplerRun struct {
	Timestamp  int64  `json:"timestamp"`
	DurationMs int64  `json:"duration_ms"`
	Inserted   int64  `json:"inserted"`
	Error      string `json:"error"`
}

func (s *Store) LogSamplerRun(ts int64, durationMs int64, inserted int64, errStr string) {
	_, _ = s.db.Exec("INSERT INTO sampler_runs (ts, duration_ms, inserted, error) VALUES (?, ?, ?, ?)", ts, durationMs, inserted, errStr)
}

func (s *Store) GetSamplerRuns(limit int) ([]SamplerRun, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query("SELECT ts, duration_ms, inserted, COALESCE(error,'') FROM sampler_runs ORDER BY ts DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []SamplerRun
	for rows.Next() {
		var r SamplerRun
		if err := rows.Scan(&r.Timestamp, &r.DurationMs, &r.Inserted, &r.Error); err != nil {
			return nil, err
		}
		res = append(res, r)
	}
	return res, nil
}

func (s *Store) SaveUserMetadata(meta UserMetadata) error {
	query := `
	INSERT INTO users (email, quota_limit, quota_period, reset_day, enabled) 
	VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(email) DO UPDATE SET
		quota_limit = excluded.quota_limit,
		quota_period = excluded.quota_period,
		reset_day = excluded.reset_day,
		enabled = excluded.enabled;
	`
	enabled := 0
	if meta.Enabled {
		enabled = 1
	}
	_, err := s.db.Exec(query, meta.Email, meta.QuotaLimit, meta.QuotaPeriod, meta.ResetDay, enabled)
	return err
}

func (s *Store) GetUserMetadata(email string) (*UserMetadata, error) {
	query := "SELECT email, quota_limit, quota_period, reset_day, enabled FROM users WHERE email = ?"
	row := s.db.QueryRow(query, email)

	var meta UserMetadata
	var enabled int
	if err := row.Scan(&meta.Email, &meta.QuotaLimit, &meta.QuotaPeriod, &meta.ResetDay, &enabled); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	meta.Enabled = enabled != 0
	return &meta, nil
}

func (s *Store) DeleteUserMetadata(email string) error {
	_, err := s.db.Exec("DELETE FROM users WHERE email = ?", email)
	return err
}

func (s *Store) GetLastSeenMap() (map[string]int64, error) {
	rows, err := s.db.Query("SELECT user, MAX(ts) FROM samples GROUP BY user")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]int64)
	for rows.Next() {
		var u string
		var ts sql.NullInt64
		if err := rows.Scan(&u, &ts); err != nil {
			return nil, err
		}
		if ts.Valid {
			result[u] = ts.Int64
		}
	}
	return result, nil
}

func (s *Store) GetLastSeenUser(user string) (int64, error) {
	var ts sql.NullInt64
	if err := s.db.QueryRow("SELECT MAX(ts) FROM samples WHERE user = ?", user).Scan(&ts); err != nil {
		return 0, err
	}
	if ts.Valid {
		return ts.Int64, nil
	}
	return 0, nil
}

func (s *Store) GetLastSeenUserWithTraffic(user string) (int64, error) {
	var ts sql.NullInt64
	if err := s.db.QueryRow("SELECT MAX(ts) FROM samples WHERE user = ? AND (uplink > 0 OR downlink > 0)", user).Scan(&ts); err != nil {
		return 0, err
	}
	if ts.Valid {
		return ts.Int64, nil
	}
	return 0, nil
}

func (s *Store) GetLastSeenWithThreshold(user string, threshold int64) (int64, error) {
	if threshold <= 0 {
		return s.GetLastSeenUserWithTraffic(user)
	}
	var ts sql.NullInt64
	if err := s.db.QueryRow("SELECT MAX(ts) FROM samples WHERE user = ? AND (uplink + downlink) >= ?", user, threshold).Scan(&ts); err != nil {
		return 0, err
	}
	if ts.Valid {
		return ts.Int64, nil
	}
	return 0, nil
}

func (s *Store) GetActiveUsers(duration time.Duration) ([]string, error) {
	cutoff := time.Now().Add(-duration).Unix()
	rows, err := s.db.Query(`SELECT DISTINCT user FROM samples WHERE ts >= ? AND (uplink > 0 OR downlink > 0)`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, nil
}

func (s *Store) GetActiveUsersWithThreshold(duration time.Duration, threshold int64) ([]string, error) {
	if threshold <= 0 {
		return s.GetActiveUsers(duration)
	}
	cutoff := time.Now().Add(-duration).Unix()
	rows, err := s.db.Query(`SELECT user, SUM(uplink + downlink) as total FROM samples WHERE ts >= ? GROUP BY user HAVING total >= ?`, cutoff, threshold)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []string
	for rows.Next() {
		var u string
		var total int64
		if err := rows.Scan(&u, &total); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, nil
}

func (s *Store) AddSample(sample Sample) error {
	query := "INSERT OR IGNORE INTO samples (user, ts, uplink, downlink) VALUES (?, ?, ?, ?)"
	_, err := s.db.Exec(query, sample.User, sample.Timestamp, sample.Uplink, sample.Downlink)
	return err
}

func (s *Store) BulkInsert(samples []Sample) error {
	if len(samples) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare("INSERT OR IGNORE INTO samples (user, ts, uplink, downlink) VALUES (?, ?, ?, ?)")
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()

	for _, smp := range samples {
		if _, err := stmt.Exec(smp.User, smp.Timestamp, smp.Uplink, smp.Downlink); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) GetSamples(user string, start, end int64) ([]Sample, error) {
	query := `
	SELECT user, ts, uplink, downlink 
	FROM samples 
	WHERE user = ? AND ts >= ? AND ts <= ? 
	ORDER BY ts ASC`

	rows, err := s.db.Query(query, user, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var samples []Sample
	for rows.Next() {
		var smp Sample
		if err := rows.Scan(&smp.User, &smp.Timestamp, &smp.Uplink, &smp.Downlink); err != nil {
			return nil, err
		}
		samples = append(samples, smp)
	}
	return samples, nil
}

func (s *Store) GetGlobalTraffic(start, end int64) ([]TrafficPoint, error) {
	query := `
	SELECT ts, SUM(uplink), SUM(downlink)
	FROM samples
	WHERE ts >= ? AND ts <= ?
	GROUP BY ts
	ORDER BY ts ASC`

	rows, err := s.db.Query(query, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []TrafficPoint
	for rows.Next() {
		var p TrafficPoint
		if err := rows.Scan(&p.Timestamp, &p.Uplink, &p.Downlink); err != nil {
			return nil, err
		}
		points = append(points, p)
	}
	return points, nil
}

func (s *Store) GetActiveUserCount(duration time.Duration) (int64, error) {
	cutoff := time.Now().Add(-duration).Unix()
	query := `
	SELECT COUNT(DISTINCT user)
	FROM samples
	WHERE ts >= ? AND (uplink > 0 OR downlink > 0)`

	var count int64
	err := s.db.QueryRow(query, cutoff).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (s *Store) GetActiveUserCountWithThreshold(duration time.Duration, threshold int64) (int64, error) {
	if threshold <= 0 {
		return s.GetActiveUserCount(duration)
	}
	cutoff := time.Now().Add(-duration).Unix()
	query := `
	SELECT COUNT(*) FROM (
		SELECT user, SUM(uplink + downlink) as total
		FROM samples
		WHERE ts >= ?
		GROUP BY user
		HAVING total >= ?
	)`
	var count int64
	err := s.db.QueryRow(query, cutoff, threshold).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}
