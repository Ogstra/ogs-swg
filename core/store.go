package core

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

type Sample struct {
	User      string
	Timestamp int64
	Uplink    int64
	Downlink  int64
}

type WGSample struct {
	PublicKey string `json:"public_key"`
	Timestamp int64  `json:"timestamp"`
	Rx        int64  `json:"rx"`
	Tx        int64  `json:"tx"`
	Endpoint  string `json:"endpoint"`
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
	db.Exec("PRAGMA auto_vacuum = INCREMENTAL;")
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
	Email         string `json:"email"`
	QuotaLimit    int64  `json:"quota_limit"`
	QuotaPeriod   string `json:"quota_period"`
	ResetDay      int    `json:"reset_day"`
	Enabled       bool   `json:"enabled"`
	VmessSecurity string `json:"vmess_security,omitempty"`
	VmessAlterID  int    `json:"vmess_alter_id,omitempty"`
}

type InboundMeta struct {
	Tag          string `json:"tag"`
	ExternalPort int    `json:"external_port"`
}

// DailyUsage represents aggregated traffic data for a user on a specific bucket (8h).
type DailyUsage struct {
	User      string
	Timestamp int64 // Bucket start timestamp
	Uplink    int64
	Downlink  int64
}

// WGDailyUsage represents aggregated traffic data for a WG peer on a specific bucket (8h).
type WGDailyUsage struct {
	PublicKey string
	Timestamp int64
	Rx        int64
	Tx        int64
}

func (s *Store) initSchema() error {
	// Check for old daily_usage schema (migration)
	var colName string
	_ = s.db.QueryRow("SELECT name FROM pragma_table_info('daily_usage') WHERE name='date'").Scan(&colName)
	if colName == "date" {
		s.db.Exec("DROP TABLE daily_usage")
	}

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
		enabled INTEGER DEFAULT 1,
		vmess_security TEXT DEFAULT '',
		vmess_alter_id INTEGER DEFAULT 0
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
		error TEXT,
		source TEXT DEFAULT 'sing-box'
	);
	CREATE TABLE IF NOT EXISTS wg_samples (
		public_key TEXT NOT NULL,
		ts INTEGER NOT NULL,
		rx INTEGER NOT NULL,
		tx INTEGER NOT NULL,
		endpoint TEXT DEFAULT ''
	);
	CREATE INDEX IF NOT EXISTS idx_wg_samples_pub_ts ON wg_samples(public_key, ts);

	CREATE TABLE IF NOT EXISTS admins (
		username TEXT PRIMARY KEY,
		password_hash TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS inbound_meta (
		tag TEXT PRIMARY KEY,
		external_port INTEGER DEFAULT 0
	);
	
	CREATE TABLE IF NOT EXISTS daily_usage (
		user TEXT NOT NULL,
		ts INTEGER NOT NULL,
		uplink INTEGER NOT NULL,
		downlink INTEGER NOT NULL,
		PRIMARY KEY (user, ts)
	);
	CREATE TABLE IF NOT EXISTS daily_wg_usage (
		public_key TEXT NOT NULL,
		ts INTEGER NOT NULL,
		rx INTEGER NOT NULL,
		tx INTEGER NOT NULL,
		PRIMARY KEY (public_key, ts)
	);
	`
	if _, err := s.db.Exec(query); err != nil {
		return err
	}
	s.db.Exec("ALTER TABLE users ADD COLUMN enabled INTEGER DEFAULT 1;")
	s.db.Exec("ALTER TABLE users ADD COLUMN vmess_security TEXT DEFAULT '';")
	s.db.Exec("ALTER TABLE users ADD COLUMN vmess_alter_id INTEGER DEFAULT 0;")
	s.db.Exec("ALTER TABLE wg_samples ADD COLUMN endpoint TEXT DEFAULT ''")
	// Migration for sampler_runs source column
	var colCheck string
	_ = s.db.QueryRow("SELECT name FROM pragma_table_info('sampler_runs') WHERE name='source'").Scan(&colCheck)
	if colCheck == "" {
		s.db.Exec("ALTER TABLE sampler_runs ADD COLUMN source TEXT DEFAULT 'sing-box'")
	}
	return nil
}

// Admin Management

func (s *Store) CreateAdmin(username, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.db.Exec("INSERT INTO admins (username, password_hash) VALUES (?, ?)", username, string(hash))
	return err
}

func (s *Store) VerifyAdmin(username, password string) (bool, error) {
	var hash string
	err := s.db.QueryRow("SELECT password_hash FROM admins WHERE username = ?", username).Scan(&hash)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	err = bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	if err != nil {
		return false, nil // Invalid password
	}
	return true, nil
}

func (s *Store) UpdateAdminPassword(username, newPassword string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	res, err := s.db.Exec("UPDATE admins SET password_hash = ? WHERE username = ?", string(hash), username)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) UpdateAdminUsername(oldUsername, newUsername string) error {
	// Check if new username already exists
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM admins WHERE username = ?", newUsername).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("username %s already exists", newUsername)
	}

	res, err := s.db.Exec("UPDATE admins SET username = ? WHERE username = ?", newUsername, oldUsername)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) SaveInboundMeta(tag string, externalPort int) error {
	if tag == "" {
		return fmt.Errorf("inbound tag required")
	}
	if externalPort <= 0 {
		return s.DeleteInboundMeta(tag)
	}
	_, err := s.db.Exec("INSERT INTO inbound_meta (tag, external_port) VALUES (?, ?) ON CONFLICT(tag) DO UPDATE SET external_port = excluded.external_port", tag, externalPort)
	return err
}

func (s *Store) GetInboundMeta(tag string) (*InboundMeta, error) {
	if tag == "" {
		return nil, nil
	}
	var meta InboundMeta
	err := s.db.QueryRow("SELECT tag, external_port FROM inbound_meta WHERE tag = ?", tag).Scan(&meta.Tag, &meta.ExternalPort)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &meta, nil
}

func (s *Store) GetAllInboundMeta() (map[string]InboundMeta, error) {
	rows, err := s.db.Query("SELECT tag, external_port FROM inbound_meta")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	meta := make(map[string]InboundMeta)
	for rows.Next() {
		var entry InboundMeta
		if err := rows.Scan(&entry.Tag, &entry.ExternalPort); err != nil {
			return nil, err
		}
		meta[entry.Tag] = entry
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return meta, nil
}

func (s *Store) DeleteInboundMeta(tag string) error {
	if tag == "" {
		return nil
	}
	_, err := s.db.Exec("DELETE FROM inbound_meta WHERE tag = ?", tag)
	return err
}

func (s *Store) RenameInboundMeta(oldTag, newTag string) error {
	if oldTag == "" || newTag == "" || oldTag == newTag {
		return nil
	}
	_, err := s.db.Exec("UPDATE inbound_meta SET tag = ? WHERE tag = ?", newTag, oldTag)
	return err
}

func (s *Store) EnsureDefaultAdmin() error {
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM admins").Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return s.CreateAdmin("admin", "admin")
	}
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
	var c1, c2, c3, c4 int64
	s.db.QueryRow("SELECT COUNT(*) FROM samples").Scan(&c1)
	s.db.QueryRow("SELECT COUNT(*) FROM wg_samples").Scan(&c2)
	s.db.QueryRow("SELECT COUNT(*) FROM daily_usage").Scan(&c3)
	s.db.QueryRow("SELECT COUNT(*) FROM daily_wg_usage").Scan(&c4)
	return c1 + c2 + c3 + c4, nil
}

type SamplerRun struct {
	Timestamp  int64  `json:"timestamp"`
	DurationMs int64  `json:"duration_ms"`
	Inserted   int64  `json:"inserted"`
	Error      string `json:"error"`
	Source     string `json:"source"`
}

func (s *Store) LogSamplerRun(ts int64, durationMs int64, inserted int64, errStr string, source string) {
	_, _ = s.db.Exec("INSERT INTO sampler_runs (ts, duration_ms, inserted, error, source) VALUES (?, ?, ?, ?, ?)", ts, durationMs, inserted, errStr, source)
}

func (s *Store) GetSamplerRuns(limit int) ([]SamplerRun, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query("SELECT ts, duration_ms, inserted, COALESCE(error,''), COALESCE(source, 'sing-box') FROM sampler_runs ORDER BY ts DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []SamplerRun
	for rows.Next() {
		var r SamplerRun
		if err := rows.Scan(&r.Timestamp, &r.DurationMs, &r.Inserted, &r.Error, &r.Source); err != nil {
			return nil, err
		}
		res = append(res, r)
	}
	return res, nil
}

func (s *Store) SaveUserMetadata(meta UserMetadata) error {
	query := `
	INSERT INTO users (email, quota_limit, quota_period, reset_day, enabled, vmess_security, vmess_alter_id) 
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(email) DO UPDATE SET
		quota_limit = excluded.quota_limit,
		quota_period = excluded.quota_period,
		reset_day = excluded.reset_day,
		enabled = excluded.enabled,
		vmess_security = excluded.vmess_security,
		vmess_alter_id = excluded.vmess_alter_id;
	`
	enabled := 0
	if meta.Enabled {
		enabled = 1
	}
	_, err := s.db.Exec(query, meta.Email, meta.QuotaLimit, meta.QuotaPeriod, meta.ResetDay, enabled, meta.VmessSecurity, meta.VmessAlterID)
	return err
}

func (s *Store) GetUserMetadata(email string) (*UserMetadata, error) {
	query := "SELECT email, quota_limit, quota_period, reset_day, enabled, vmess_security, vmess_alter_id FROM users WHERE email = ?"
	row := s.db.QueryRow(query, email)

	var meta UserMetadata
	var enabled int
	if err := row.Scan(&meta.Email, &meta.QuotaLimit, &meta.QuotaPeriod, &meta.ResetDay, &enabled, &meta.VmessSecurity, &meta.VmessAlterID); err != nil {
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

func (s *Store) GetAllUserMetadata() ([]UserMetadata, error) {
	rows, err := s.db.Query("SELECT email, quota_limit, quota_period, reset_day, enabled, vmess_security, vmess_alter_id FROM users")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []UserMetadata
	for rows.Next() {
		var meta UserMetadata
		var enabled int
		if err := rows.Scan(&meta.Email, &meta.QuotaLimit, &meta.QuotaPeriod, &meta.ResetDay, &enabled, &meta.VmessSecurity, &meta.VmessAlterID); err != nil {
			return nil, err
		}
		meta.Enabled = enabled != 0
		result = append(result, meta)
	}
	return result, nil
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

// WireGuard traffic samples

func (s *Store) InsertWGSamples(samples []WGSample) error {
	if len(samples) == 0 {
		return nil
	}
	if len(samples) > 5000 {
		samples = samples[:5000]
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare("INSERT INTO wg_samples (public_key, ts, rx, tx, endpoint) VALUES (?, ?, ?, ?, ?)")
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()

	for _, smp := range samples {
		if _, err := stmt.Exec(smp.PublicKey, smp.Timestamp, smp.Rx, smp.Tx, smp.Endpoint); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

type TrafficStats struct {
	Uplink   int64
	Downlink int64
}

// WGPubTotal represents aggregated wireguard usage for a peer.
type WGPubTotal struct {
	PublicKey string
	Total     int64
	Rx        int64
	Tx        int64
}

type User struct {
	Uuid        string
	Username    string
	DataLimit   int64
	QuotaPeriod string
	ResetDay    int
	Enabled     bool
}

// GetUsers returns all users.
func (s *Store) GetUsers() ([]User, error) {
	query := `SELECT uuid, username, data_limit, quota_period, reset_day, enabled FROM users`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		var enabled int
		if err := rows.Scan(&u.Uuid, &u.Username, &u.DataLimit, &u.QuotaPeriod, &u.ResetDay, &enabled); err != nil {
			return nil, err
		}
		u.Enabled = enabled == 1
		users = append(users, u)
	}
	return users, nil
}

// GetTrafficPerUser returns aggregated usage per user for the given time range.
func (s *Store) GetTrafficPerUser(start, end int64) (map[string]TrafficStats, error) {
	query := `
	SELECT user, SUM(uplink), SUM(downlink)
	FROM samples
	WHERE ts >= ? AND ts <= ?
	GROUP BY user`

	rows, err := s.db.Query(query, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]TrafficStats)
	for rows.Next() {
		var user string
		var up, down sql.NullInt64
		if err := rows.Scan(&user, &up, &down); err != nil {
			return nil, err
		}
		result[user] = TrafficStats{
			Uplink:   up.Int64,
			Downlink: down.Int64,
		}
	}
	return result, nil
}

// GetWGTrafficDelta returns rx/tx delta between first and last sample in the range.
func (s *Store) GetWGTrafficDelta(publicKey string, start, end int64) (int64, int64, error) {
	if publicKey == "" {
		return 0, 0, nil
	}
	var firstRx, firstTx, lastRx, lastTx sql.NullInt64

	err := s.db.QueryRow(`SELECT rx, tx FROM wg_samples WHERE public_key = ? AND ts >= ? AND ts <= ? ORDER BY ts ASC LIMIT 1`,
		publicKey, start, end).Scan(&firstRx, &firstTx)
	if err != nil && err != sql.ErrNoRows {
		return 0, 0, err
	}
	err = s.db.QueryRow(`SELECT rx, tx FROM wg_samples WHERE public_key = ? AND ts >= ? AND ts <= ? ORDER BY ts DESC LIMIT 1`,
		publicKey, start, end).Scan(&lastRx, &lastTx)
	if err != nil && err != sql.ErrNoRows {
		return 0, 0, err
	}

	if !firstRx.Valid || !lastRx.Valid {
		return 0, 0, nil
	}

	deltaRx := lastRx.Int64 - firstRx.Int64
	deltaTx := lastTx.Int64 - firstTx.Int64
	if deltaRx < 0 {
		deltaRx = 0
	}
	if deltaTx < 0 {
		deltaTx = 0
	}
	return deltaRx, deltaTx, nil
}

func (s *Store) GetWGTrafficSeries(publicKey string, start, end int64, limit int) ([]WGSample, error) {
	if publicKey == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 5000 {
		limit = 5000
	}
	rows, err := s.db.Query(`SELECT public_key, ts, rx, tx, COALESCE(endpoint, '') 
		FROM wg_samples 
		WHERE public_key = ? AND ts >= ? AND ts <= ? 
		ORDER BY ts ASC 
		LIMIT ?`, publicKey, start, end, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var series []WGSample
	for rows.Next() {
		var smp WGSample
		if err := rows.Scan(&smp.PublicKey, &smp.Timestamp, &smp.Rx, &smp.Tx, &smp.Endpoint); err != nil {
			return nil, err
		}
		series = append(series, smp)
	}
	return series, nil
}

// GetWGTrafficBuckets returns aggregated WireGuard traffic deltas bucketed by interval.
// It computes per-sample deltas using window functions, then sums them per bucket.
func (s *Store) GetWGTrafficBuckets(publicKeys []string, start, end, interval int64) (map[int64]TrafficStats, error) {
	out := make(map[int64]TrafficStats)
	if len(publicKeys) == 0 {
		return out, nil
	}
	if interval <= 0 {
		interval = 60
	}

	placeholders := strings.Repeat("?,", len(publicKeys))
	placeholders = strings.TrimSuffix(placeholders, ",")

	args := make([]interface{}, 0, len(publicKeys)+4)
	for _, k := range publicKeys {
		args = append(args, k)
	}
	args = append(args, start, end, interval, interval)

	query := fmt.Sprintf(`
WITH ordered AS (
  SELECT
    public_key,
    ts,
    rx,
    tx,
    LAG(rx) OVER (PARTITION BY public_key ORDER BY ts) AS prev_rx,
    LAG(tx) OVER (PARTITION BY public_key ORDER BY ts) AS prev_tx
  FROM wg_samples
  WHERE public_key IN (%s) AND ts >= ? AND ts <= ?
),
diffs AS (
  SELECT
    ts,
    CASE
      WHEN prev_tx IS NULL THEN 0
      WHEN tx - prev_tx < 0 THEN 0
      ELSE tx - prev_tx
    END AS dx,
    CASE
      WHEN prev_rx IS NULL THEN 0
      WHEN rx - prev_rx < 0 THEN 0
      ELSE rx - prev_rx
    END AS dr
  FROM ordered
)
SELECT
  (ts / ?) * ? AS bucket_ts,
  SUM(dx) AS uplink,
  SUM(dr) AS downlink
FROM diffs
GROUP BY bucket_ts
ORDER BY bucket_ts ASC
`, placeholders)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var bucketTs int64
		var up, down sql.NullInt64
		if err := rows.Scan(&bucketTs, &up, &down); err != nil {
			return nil, err
		}
		out[bucketTs] = TrafficStats{Uplink: up.Int64, Downlink: down.Int64}
	}
	return out, nil
}

// GetWGTopTotals aggregates total usage per peer (rx/tx deltas) in the given range.
func (s *Store) GetWGTopTotals(start, end int64, limit int) ([]WGPubTotal, error) {
	rows, err := s.db.Query(`
		SELECT
			public_key,
			(MAX(rx) - MIN(rx)) AS rx_delta,
			(MAX(tx) - MIN(tx)) AS tx_delta
		FROM wg_samples
		WHERE ts >= ? AND ts <= ?
		GROUP BY public_key
		ORDER BY (rx_delta + tx_delta) DESC
		LIMIT ?`, start, end, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := []WGPubTotal{}
	for rows.Next() {
		var pub string
		var rx, tx sql.NullInt64
		if err := rows.Scan(&pub, &rx, &tx); err != nil {
			return nil, err
		}
		r := rx.Int64
		t := tx.Int64
		if r < 0 {
			r = 0
		}
		if t < 0 {
			t = 0
		}
		results = append(results, WGPubTotal{
			PublicKey: pub,
			Rx:        r,
			Tx:        t,
			Total:     r + t,
		})
	}
	return results, nil
}

func (s *Store) PruneWGSamplesOlderThan(ts int64) (int64, error) {
	res, err := s.db.Exec("DELETE FROM wg_samples WHERE ts < ?", ts)
	if err != nil {
		return 0, err
	}
	affected, _ := res.RowsAffected()
	return affected, nil
}

func (s *Store) CompressOldSamples(olderThanTs int64) (int64, error) {
	// 1. Transaction start
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	// 2. Aggregate data into 8-hour buckets (28800 seconds)
	// We use integer division to floor the timestamp to the nearest 8h bucket
	bucketSize := int64(8 * 3600)

	rows, err := tx.Query(`
		SELECT user, (ts / ?) * ? as bucket_ts, SUM(uplink), SUM(downlink)
		FROM samples
		WHERE ts < ?
		GROUP BY user, bucket_ts
	`, bucketSize, bucketSize, olderThanTs)
	if err != nil {
		return 0, fmt.Errorf("compress query failed: %v", err)
	}

	type aggRow struct {
		u    string
		ts   int64
		up   int64
		down int64
	}
	var agg []aggRow

	for rows.Next() {
		var r aggRow
		if err := rows.Scan(&r.u, &r.ts, &r.up, &r.down); err != nil {
			rows.Close()
			return 0, err
		}
		agg = append(agg, r)
	}
	rows.Close()

	if len(agg) == 0 {
		return 0, nil // Nothing to compress
	}

	// 3. Upsert into daily_usage
	for _, a := range agg {
		_, err := tx.Exec(`
			INSERT INTO daily_usage (user, ts, uplink, downlink)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(user, ts) DO UPDATE SET
			uplink = uplink + excluded.uplink,
			downlink = downlink + excluded.downlink
		`, a.u, a.ts, a.up, a.down)
		if err != nil {
			return 0, fmt.Errorf("compress insert failed: %v", err)
		}
	}

	// 4. Delete old samples
	res, err := tx.Exec("DELETE FROM samples WHERE ts < ?", olderThanTs)
	if err != nil {
		return 0, fmt.Errorf("compress delete failed: %v", err)
	}

	deleted, _ := res.RowsAffected()

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	return deleted, nil
}

func (s *Store) CompressOldWGSamples(olderThanTs int64) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	// Aggregate into 8-hour buckets
	bucketSize := int64(8 * 3600)

	rows, err := tx.Query(`
		SELECT public_key, (ts / ?) * ? as bucket_ts, SUM(rx), SUM(tx)
		FROM wg_samples
		WHERE ts < ?
		GROUP BY public_key, bucket_ts
	`, bucketSize, bucketSize, olderThanTs)
	if err != nil {
		return 0, fmt.Errorf("compress wg query failed: %v", err)
	}

	type aggRow struct {
		pk string
		ts int64
		rx int64
		tx int64
	}
	var agg []aggRow

	for rows.Next() {
		var r aggRow
		if err := rows.Scan(&r.pk, &r.ts, &r.rx, &r.tx); err != nil {
			rows.Close()
			return 0, err
		}
		agg = append(agg, r)
	}
	rows.Close()

	if len(agg) == 0 {
		return 0, nil
	}

	for _, a := range agg {
		_, err := tx.Exec(`
			INSERT INTO daily_wg_usage (public_key, ts, rx, tx)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(public_key, ts) DO UPDATE SET
			rx = rx + excluded.rx,
			tx = tx + excluded.tx
		`, a.pk, a.ts, a.rx, a.tx)
		if err != nil {
			return 0, fmt.Errorf("compress wg insert failed: %v", err)
		}
	}

	res, err := tx.Exec("DELETE FROM wg_samples WHERE ts < ?", olderThanTs)
	if err != nil {
		return 0, fmt.Errorf("compress wg delete failed: %v", err)
	}

	deleted, _ := res.RowsAffected()

	if err := tx.Commit(); err != nil {
		return 0, err
	}

	return deleted, nil
}

// GetCombinedReport queries both daily_usage and samples to build a comprehensive report.
func (s *Store) GetCombinedReport(user string, start, end int64) ([]Sample, error) {
	// 1. Get Aggregated Data in Range

	// Adjust start date to include the day of 'start' timestamp
	// Actually, if we want strict range, we should be careful.
	// But usually reports are "Last 30 days".

	rows, err := s.db.Query(`
		SELECT user, ts, uplink, downlink
		FROM daily_usage
		WHERE user = ? AND ts >= ? AND ts <= ?
	`, user, start, end)

	var samples []Sample
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var u string
			var ts int64
			var up, down int64
			if err := rows.Scan(&u, &ts, &up, &down); err == nil {
				samples = append(samples, Sample{
					User:      u,
					Timestamp: ts,
					Uplink:    up,
					Downlink:  down,
				})
			}
		}
	}

	// 2. Get Raw Samples in Range
	// We might have overlap if compression ran recently.
	// Ideally we only query raw samples > configured compression cut-off?
	// But simplest is just union all for now.
	rawRows, err := s.db.Query(`
		SELECT user, ts, uplink, downlink
		FROM samples
		WHERE user = ? AND ts >= ? AND ts <= ?
	`, user, start, end)
	if err == nil {
		defer rawRows.Close()
		for rawRows.Next() {
			var smp Sample
			if err := rawRows.Scan(&smp.User, &smp.Timestamp, &smp.Uplink, &smp.Downlink); err == nil {
				samples = append(samples, smp)
			}
		}
	}

	return samples, nil
}

func (s *Store) Vacuum() error {
	_, err := s.db.Exec("VACUUM;")
	return err
}
