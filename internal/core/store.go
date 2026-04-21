package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	sqlcStore "github.com/Ogstra/ogs-swg/internal/core/store"
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
	*sqlcStore.Queries
}

func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA synchronous=NORMAL;",
		"PRAGMA busy_timeout=5000;",
		"PRAGMA auto_vacuum=INCREMENTAL;",
		"PRAGMA mmap_size=30000000000;",
		"PRAGMA temp_store=MEMORY;",
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			// Log but don't fail - some pragmas might not be critical
			log.Printf("Warning: Failed to set %s: %v", pragma, err)
		}
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := db.Ping(); err != nil {
		return nil, err
	}

	s := &Store{
		db:      db,
		Queries: sqlcStore.New(db),
	}
	if err := s.initSchema(); err != nil {
		return nil, err
	}

	return s, nil
}

type UserMetadata struct {
	Email         string   `json:"email"`
	QuotaLimit    int64    `json:"quota_limit"`
	QuotaPeriod   string   `json:"quota_period"`
	ResetDay      int      `json:"reset_day"`
	Enabled       bool     `json:"enabled"`
	Credential    string   `json:"credential,omitempty"`
	Flow          string   `json:"flow,omitempty"`
	VmessSecurity string   `json:"vmess_security,omitempty"`
	VmessAlterID  int      `json:"vmess_alter_id,omitempty"`
	InboundTags   []string `json:"inbound_tags,omitempty"`
}

type InboundMeta struct {
	Tag               string `json:"tag"`
	ExternalPort      int    `json:"external_port"`
	ClientSNI         string `json:"client_sni,omitempty"`
	LinkAllowInsecure *bool  `json:"link_allow_insecure,omitempty"`
	OverrideAddress   string `json:"override_address,omitempty"`
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
		credential TEXT DEFAULT '',
		flow TEXT DEFAULT '',
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
	CREATE TABLE IF NOT EXISTS wg_peers (
		public_key TEXT PRIMARY KEY,
		alias TEXT NOT NULL,
		last_handshake INTEGER DEFAULT 0,
		deleted INTEGER DEFAULT 0,
		created_at INTEGER DEFAULT (strftime('%s','now')),
		updated_at INTEGER DEFAULT (strftime('%s','now'))
	);

	CREATE TABLE IF NOT EXISTS admins (
		username TEXT PRIMARY KEY,
		password_hash TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS panel_users (
		username TEXT PRIMARY KEY,
		password_hash TEXT NOT NULL,
		can_read_users INTEGER NOT NULL DEFAULT 0,
		can_write_users INTEGER NOT NULL DEFAULT 0,
		can_read_wireguard INTEGER NOT NULL DEFAULT 0,
		can_write_wireguard INTEGER NOT NULL DEFAULT 0,
		can_read_config INTEGER NOT NULL DEFAULT 0,
		can_write_config INTEGER NOT NULL DEFAULT 0,
		can_read_settings INTEGER NOT NULL DEFAULT 0,
		can_write_settings INTEGER NOT NULL DEFAULT 0,
		can_read_panel_users INTEGER NOT NULL DEFAULT 0,
		can_write_panel_users INTEGER NOT NULL DEFAULT 0,
		can_read_logs INTEGER NOT NULL DEFAULT 0,
		subscription_default_profile_update_interval_hours INTEGER DEFAULT NULL,
		subscription_default_update_always INTEGER NOT NULL DEFAULT 0,
		subscription_default_destinations_json TEXT NOT NULL DEFAULT '[]',
		created_at INTEGER DEFAULT (strftime('%s','now')),
		updated_at INTEGER DEFAULT (strftime('%s','now'))
	);

	CREATE TABLE IF NOT EXISTS dashboard_preferences (
		principal TEXT PRIMARY KEY,
		default_service TEXT NOT NULL DEFAULT 'singbox',
		refresh_ms INTEGER NOT NULL DEFAULT 10000,
		default_range TEXT NOT NULL DEFAULT '24h',
		active_user_window_minutes INTEGER NOT NULL DEFAULT 5,
		detail_chart_target_points INTEGER NOT NULL DEFAULT 200,
		created_at INTEGER DEFAULT (strftime('%s','now')),
		updated_at INTEGER DEFAULT (strftime('%s','now'))
	);

	CREATE TABLE IF NOT EXISTS inbound_meta (
		tag TEXT PRIMARY KEY,
		external_port INTEGER DEFAULT 0,
		client_sni TEXT DEFAULT NULL,
		link_allow_insecure INTEGER DEFAULT NULL,
		override_address TEXT DEFAULT NULL
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

	CREATE TABLE IF NOT EXISTS subscriptions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		token TEXT UNIQUE NOT NULL,
		name TEXT NOT NULL,
		quota_limit INTEGER DEFAULT 0,
		quota_period TEXT DEFAULT 'monthly',
		reset_day INTEGER DEFAULT 1,
		profile_update_interval_hours INTEGER DEFAULT NULL,
		update_always INTEGER NOT NULL DEFAULT 0,
		created_at INTEGER DEFAULT (strftime('%s','now')),
		updated_at INTEGER DEFAULT (strftime('%s','now'))
	);

	CREATE TABLE IF NOT EXISTS subscription_users (
		sub_id INTEGER NOT NULL,
		user_name TEXT NOT NULL,
		alias TEXT NOT NULL DEFAULT '',
		position INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (sub_id, user_name),
		FOREIGN KEY (sub_id) REFERENCES subscriptions(id) ON DELETE CASCADE,
		FOREIGN KEY (user_name) REFERENCES users(email) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS subscription_requests (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		sub_id INTEGER NOT NULL,
		user_name TEXT NOT NULL DEFAULT '',
		request_ip TEXT NOT NULL DEFAULT '',
		request_host TEXT NOT NULL DEFAULT '',
		request_path TEXT NOT NULL DEFAULT '',
		user_agent TEXT NOT NULL DEFAULT '',
		device_model TEXT NOT NULL DEFAULT '',
		device_os TEXT NOT NULL DEFAULT '',
		device_os_version TEXT NOT NULL DEFAULT '',
		app_version TEXT NOT NULL DEFAULT '',
		country TEXT NOT NULL DEFAULT '',
		hwid_hash TEXT NOT NULL DEFAULT '',
		hwid_prefix TEXT NOT NULL DEFAULT '',
		requested_at INTEGER NOT NULL,
		served_from_cache INTEGER NOT NULL DEFAULT 0,
		blocked INTEGER NOT NULL DEFAULT 0,
		block_reason TEXT NOT NULL DEFAULT '',
		FOREIGN KEY (sub_id) REFERENCES subscriptions(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS subscription_protection_rules (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		rule_type TEXT NOT NULL,
		value TEXT NOT NULL,
		note TEXT NOT NULL DEFAULT '',
		created_at INTEGER DEFAULT (strftime('%s','now'))
	);

	CREATE INDEX IF NOT EXISTS idx_subscription_requests_sub_id_requested_at
		ON subscription_requests(sub_id, requested_at DESC);
	CREATE INDEX IF NOT EXISTS idx_protection_rules_type_value
		ON subscription_protection_rules(rule_type, value);
	`
	if _, err := s.db.Exec(query); err != nil {
		return err
	}
	// Upgrade path: add client_sni to existing inbound_meta tables that predate this column.
	// Silently ignored if column already exists (fresh installs have it from CREATE TABLE above).
	s.db.Exec("ALTER TABLE inbound_meta ADD COLUMN client_sni TEXT DEFAULT NULL;")
	// Upgrade path: add link_allow_insecure to existing inbound_meta tables that predate this column.
	// NULL means "auto" so legacy heuristic behavior remains unchanged until an explicit choice is stored.
	s.db.Exec("ALTER TABLE inbound_meta ADD COLUMN link_allow_insecure INTEGER DEFAULT NULL;")
	s.db.Exec("ALTER TABLE inbound_meta ADD COLUMN override_address TEXT DEFAULT NULL;")
	// Upgrade path: add inbound_tags to existing users tables that predate this column.
	// Silently ignored if column already exists (SQLite returns "duplicate column name" error).
	s.db.Exec("ALTER TABLE users ADD COLUMN inbound_tags TEXT DEFAULT '';")
	s.db.Exec("ALTER TABLE users ADD COLUMN credential TEXT DEFAULT '';")
	s.db.Exec("ALTER TABLE users ADD COLUMN flow TEXT DEFAULT '';")
	// Reset day is now fixed to 1 for all users.
	s.db.Exec("UPDATE users SET reset_day = 1 WHERE COALESCE(reset_day, 1) != 1;")
	// Upgrade path: add can_read_logs_censored to existing panel_users tables.
	// Silently ignored if column already exists (SQLite returns "duplicate column name" error).
	s.db.Exec("ALTER TABLE panel_users ADD COLUMN can_read_logs_censored INTEGER NOT NULL DEFAULT 0;")
	s.db.Exec("ALTER TABLE panel_users ADD COLUMN subscription_default_profile_update_interval_hours INTEGER DEFAULT NULL;")
	s.db.Exec("ALTER TABLE panel_users ADD COLUMN subscription_default_update_always INTEGER NOT NULL DEFAULT 0;")
	s.db.Exec("ALTER TABLE panel_users ADD COLUMN subscription_default_destinations_json TEXT NOT NULL DEFAULT '[]';")
	s.db.Exec("UPDATE panel_users SET subscription_default_destinations_json = '[]' WHERE COALESCE(subscription_default_destinations_json, '') = '';")
	s.db.Exec("ALTER TABLE dashboard_preferences ADD COLUMN default_service TEXT NOT NULL DEFAULT 'singbox';")
	s.db.Exec("ALTER TABLE dashboard_preferences ADD COLUMN refresh_ms INTEGER NOT NULL DEFAULT 10000;")
	s.db.Exec("ALTER TABLE dashboard_preferences ADD COLUMN default_range TEXT NOT NULL DEFAULT '24h';")
	s.db.Exec("ALTER TABLE dashboard_preferences ADD COLUMN active_user_window_minutes INTEGER NOT NULL DEFAULT 5;")
	s.db.Exec("ALTER TABLE dashboard_preferences ADD COLUMN detail_chart_target_points INTEGER NOT NULL DEFAULT 200;")
	s.db.Exec(`UPDATE dashboard_preferences SET created_at = strftime('%s','now') WHERE typeof(created_at) != 'integer'`)
	s.db.Exec(`UPDATE dashboard_preferences SET updated_at = strftime('%s','now') WHERE typeof(updated_at) != 'integer'`)
	s.db.Exec(`UPDATE panel_users SET created_at = strftime('%s','now') WHERE typeof(created_at) != 'integer'`)
	s.db.Exec(`UPDATE panel_users SET updated_at = strftime('%s','now') WHERE typeof(updated_at) != 'integer'`)
	s.db.Exec(`UPDATE wg_peers SET created_at = strftime('%s','now') WHERE typeof(created_at) != 'integer'`)
	s.db.Exec(`UPDATE wg_peers SET updated_at = strftime('%s','now') WHERE typeof(updated_at) != 'integer'`)
	// Upgrade path: add quota fields to subscriptions for existing rows that predate this feature.
	s.db.Exec("ALTER TABLE subscriptions ADD COLUMN quota_limit INTEGER DEFAULT 0;")
	s.db.Exec("ALTER TABLE subscriptions ADD COLUMN quota_period TEXT DEFAULT 'monthly';")
	s.db.Exec("ALTER TABLE subscriptions ADD COLUMN reset_day INTEGER DEFAULT 1;")
	s.db.Exec("ALTER TABLE subscriptions ADD COLUMN profile_update_interval_hours INTEGER DEFAULT NULL;")
	s.db.Exec("ALTER TABLE subscriptions ADD COLUMN update_always INTEGER NOT NULL DEFAULT 0;")
	s.db.Exec("UPDATE subscriptions SET update_always = 0 WHERE update_always IS NULL;")
	s.db.Exec("ALTER TABLE subscription_users ADD COLUMN alias TEXT NOT NULL DEFAULT '';")
	s.db.Exec("ALTER TABLE subscription_users ADD COLUMN position INTEGER NOT NULL DEFAULT 0;")
	s.db.Exec("ALTER TABLE subscription_requests ADD COLUMN user_name TEXT NOT NULL DEFAULT '';")
	s.db.Exec("ALTER TABLE subscription_requests ADD COLUMN request_ip TEXT NOT NULL DEFAULT '';")
	s.db.Exec("ALTER TABLE subscription_requests ADD COLUMN request_host TEXT NOT NULL DEFAULT '';")
	s.db.Exec("ALTER TABLE subscription_requests ADD COLUMN request_path TEXT NOT NULL DEFAULT '';")
	s.db.Exec("ALTER TABLE subscription_requests ADD COLUMN user_agent TEXT NOT NULL DEFAULT '';")
	s.db.Exec("ALTER TABLE subscription_requests ADD COLUMN device_model TEXT NOT NULL DEFAULT '';")
	s.db.Exec("ALTER TABLE subscription_requests ADD COLUMN device_os TEXT NOT NULL DEFAULT '';")
	s.db.Exec("ALTER TABLE subscription_requests ADD COLUMN device_os_version TEXT NOT NULL DEFAULT '';")
	s.db.Exec("ALTER TABLE subscription_requests ADD COLUMN app_version TEXT NOT NULL DEFAULT '';")
	s.db.Exec("ALTER TABLE subscription_requests ADD COLUMN country TEXT NOT NULL DEFAULT '';")
	s.db.Exec("ALTER TABLE subscription_requests ADD COLUMN hwid_hash TEXT NOT NULL DEFAULT '';")
	s.db.Exec("ALTER TABLE subscription_requests ADD COLUMN hwid_prefix TEXT NOT NULL DEFAULT '';")
	s.db.Exec("ALTER TABLE subscription_requests ADD COLUMN blocked INTEGER NOT NULL DEFAULT 0;")
	s.db.Exec("ALTER TABLE subscription_requests ADD COLUMN block_reason TEXT NOT NULL DEFAULT '';")
	s.db.Exec("CREATE INDEX IF NOT EXISTS idx_subscription_requests_blocked ON subscription_requests(blocked, requested_at DESC);")
	return nil
}

// PanelUserPermissions holds the set of permissions for a panel user.
type PanelUserPermissions struct {
	CanReadUsers        bool `json:"can_read_users"`
	CanWriteUsers       bool `json:"can_write_users"`
	CanReadWireguard    bool `json:"can_read_wireguard"`
	CanWriteWireguard   bool `json:"can_write_wireguard"`
	CanReadConfig       bool `json:"can_read_config"`
	CanWriteConfig      bool `json:"can_write_config"`
	CanReadSettings     bool `json:"can_read_settings"`
	CanWriteSettings    bool `json:"can_write_settings"`
	CanReadPanelUsers   bool `json:"can_read_panel_users"`
	CanWritePanelUsers  bool `json:"can_write_panel_users"`
	CanReadLogs         bool `json:"can_read_logs"`
	CanReadLogsCensored bool `json:"can_read_logs_censored"`
}

// PanelUserInfo is a safe (no password hash) representation of a panel user.
type PanelUserInfo struct {
	Username    string               `json:"username"`
	Permissions PanelUserPermissions `json:"permissions"`
	CreatedAt   int64                `json:"created_at"`
}

type SubscriptionDefaults struct {
	ProfileUpdateIntervalHours *int64   `json:"profile_update_interval_hours"`
	UpdateAlways               bool     `json:"update_always"`
	Destinations               []string `json:"destinations"`
}

type DashboardPreferences struct {
	DefaultService          string `json:"default_service"`
	RefreshMs               int    `json:"refresh_ms"`
	DefaultRange            string `json:"default_range"`
	ActiveUserWindowMinutes int    `json:"active_user_window_minutes"`
	DetailChartTargetPoints int    `json:"detail_chart_target_points"`
}

func DefaultDashboardPreferences() DashboardPreferences {
	return DashboardPreferences{
		DefaultService:          "singbox",
		RefreshMs:               10000,
		DefaultRange:            "24h",
		ActiveUserWindowMinutes: 5,
		DetailChartTargetPoints: 200,
	}
}

func normalizeDashboardPreferences(p DashboardPreferences) DashboardPreferences {
	out := DefaultDashboardPreferences()
	if p.DefaultService == "wireguard" {
		out.DefaultService = "wireguard"
	}
	switch p.DefaultRange {
	case "30m", "1h", "6h", "24h", "1w", "1m":
		out.DefaultRange = p.DefaultRange
	}
	if p.RefreshMs >= 1000 {
		out.RefreshMs = p.RefreshMs
	}
	if p.ActiveUserWindowMinutes >= 1 && p.ActiveUserWindowMinutes <= 1440 {
		out.ActiveUserWindowMinutes = p.ActiveUserWindowMinutes
	}
	switch p.DetailChartTargetPoints {
	case 50, 100, 150, 200:
		out.DetailChartTargetPoints = p.DetailChartTargetPoints
	}
	return out
}

func boolToInt64(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func nullableInt64(value *int64) sql.NullInt64 {
	if value == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *value, Valid: true}
}

func nullableInt64Ptr(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	v := value.Int64
	return &v
}

// Normalize keeps granular permissions coherent.
func (p *PanelUserPermissions) Normalize() {
	if p == nil {
		return
	}

	// write implies read
	p.CanReadUsers = p.CanReadUsers || p.CanWriteUsers
	p.CanReadWireguard = p.CanReadWireguard || p.CanWriteWireguard
	p.CanReadConfig = p.CanReadConfig || p.CanWriteConfig
	p.CanReadSettings = p.CanReadSettings || p.CanWriteSettings
	p.CanReadPanelUsers = p.CanReadPanelUsers || p.CanWritePanelUsers
	p.CanReadLogs = p.CanReadLogs || p.CanReadLogsCensored
}

func fullPanelUserPermissions() PanelUserPermissions {
	p := PanelUserPermissions{
		CanReadUsers:        true,
		CanWriteUsers:       true,
		CanReadWireguard:    true,
		CanWriteWireguard:   true,
		CanReadConfig:       true,
		CanWriteConfig:      true,
		CanReadSettings:     true,
		CanWriteSettings:    true,
		CanReadPanelUsers:   true,
		CanWritePanelUsers:  true,
		CanReadLogs:         true,
		CanReadLogsCensored: true,
	}
	p.Normalize()
	return p
}

func (s *Store) CreatePanelUser(username, password string, perms PanelUserPermissions) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	perms.Normalize()
	_, err = s.db.ExecContext(context.Background(), `
		INSERT INTO panel_users
			(username, password_hash,
			 can_read_users, can_write_users,
			 can_read_wireguard, can_write_wireguard,
			 can_read_config, can_write_config,
			 can_read_settings, can_write_settings,
			 can_read_panel_users, can_write_panel_users,
			 can_read_logs, can_read_logs_censored,
			 created_at, updated_at)
		VALUES (?, ?,
		        ?, ?,
		        ?, ?,
		        ?, ?,
		        ?, ?,
		        ?, ?,
		        ?, ?,
		        strftime('%s','now'), strftime('%s','now'))`,
		username, string(hash),
		boolToInt64(perms.CanReadUsers),
		boolToInt64(perms.CanWriteUsers),
		boolToInt64(perms.CanReadWireguard),
		boolToInt64(perms.CanWriteWireguard),
		boolToInt64(perms.CanReadConfig),
		boolToInt64(perms.CanWriteConfig),
		boolToInt64(perms.CanReadSettings),
		boolToInt64(perms.CanWriteSettings),
		boolToInt64(perms.CanReadPanelUsers),
		boolToInt64(perms.CanWritePanelUsers),
		boolToInt64(perms.CanReadLogs),
		boolToInt64(perms.CanReadLogsCensored),
	)
	return err
}

// VerifyPanelUser checks credentials and returns the user's permissions if valid.
func (s *Store) VerifyPanelUser(username, password string) (*PanelUserPermissions, error) {
	row := s.db.QueryRowContext(context.Background(), `
		SELECT
			password_hash,
			COALESCE(can_read_users, 0),
			COALESCE(can_write_users, 0),
			COALESCE(can_read_wireguard, 0),
			COALESCE(can_write_wireguard, 0),
			COALESCE(can_read_config, 0),
			COALESCE(can_write_config, 0),
			COALESCE(can_read_settings, 0),
			COALESCE(can_write_settings, 0),
			COALESCE(can_read_panel_users, 0),
			COALESCE(can_write_panel_users, 0),
			COALESCE(can_read_logs, 0),
			COALESCE(can_read_logs_censored, 0)
		FROM panel_users
		WHERE username = ?
	`, username)

	var (
		passwordHash        string
		canReadUsers        int64
		canWriteUsers       int64
		canReadWireguard    int64
		canWriteWireguard   int64
		canReadConfig       int64
		canWriteConfig      int64
		canReadSettings     int64
		canWriteSettings    int64
		canReadPanelUsers   int64
		canWritePanelUsers  int64
		canReadLogs         int64
		canReadLogsCensored int64
	)

	err := row.Scan(
		&passwordHash,
		&canReadUsers,
		&canWriteUsers,
		&canReadWireguard,
		&canWriteWireguard,
		&canReadConfig,
		&canWriteConfig,
		&canReadSettings,
		&canWriteSettings,
		&canReadPanelUsers,
		&canWritePanelUsers,
		&canReadLogs,
		&canReadLogsCensored,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)); err != nil {
		return nil, nil
	}
	perms := &PanelUserPermissions{
		CanReadUsers:        canReadUsers != 0,
		CanWriteUsers:       canWriteUsers != 0,
		CanReadWireguard:    canReadWireguard != 0,
		CanWriteWireguard:   canWriteWireguard != 0,
		CanReadConfig:       canReadConfig != 0,
		CanWriteConfig:      canWriteConfig != 0,
		CanReadSettings:     canReadSettings != 0,
		CanWriteSettings:    canWriteSettings != 0,
		CanReadPanelUsers:   canReadPanelUsers != 0,
		CanWritePanelUsers:  canWritePanelUsers != 0,
		CanReadLogs:         canReadLogs != 0,
		CanReadLogsCensored: canReadLogsCensored != 0,
	}
	perms.Normalize()
	return perms, nil
}

func (s *Store) GetAllPanelUsers() ([]PanelUserInfo, error) {
	rows, err := s.db.QueryContext(context.Background(), `
		SELECT
			username,
			COALESCE(can_read_users, 0),
			COALESCE(can_write_users, 0),
			COALESCE(can_read_wireguard, 0),
			COALESCE(can_write_wireguard, 0),
			COALESCE(can_read_config, 0),
			COALESCE(can_write_config, 0),
			COALESCE(can_read_settings, 0),
			COALESCE(can_write_settings, 0),
			COALESCE(can_read_panel_users, 0),
			COALESCE(can_write_panel_users, 0),
			COALESCE(can_read_logs, 0),
			COALESCE(can_read_logs_censored, 0),
			COALESCE(created_at, 0)
		FROM panel_users
		ORDER BY username ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]PanelUserInfo, 0)
	for rows.Next() {
		var (
			username            string
			canReadUsers        int64
			canWriteUsers       int64
			canReadWireguard    int64
			canWriteWireguard   int64
			canReadConfig       int64
			canWriteConfig      int64
			canReadSettings     int64
			canWriteSettings    int64
			canReadPanelUsers   int64
			canWritePanelUsers  int64
			canReadLogs         int64
			canReadLogsCensored int64
			createdAt           int64
		)
		if err := rows.Scan(
			&username,
			&canReadUsers,
			&canWriteUsers,
			&canReadWireguard,
			&canWriteWireguard,
			&canReadConfig,
			&canWriteConfig,
			&canReadSettings,
			&canWriteSettings,
			&canReadPanelUsers,
			&canWritePanelUsers,
			&canReadLogs,
			&canReadLogsCensored,
			&createdAt,
		); err != nil {
			return nil, err
		}
		perms := PanelUserPermissions{
			CanReadUsers:        canReadUsers != 0,
			CanWriteUsers:       canWriteUsers != 0,
			CanReadWireguard:    canReadWireguard != 0,
			CanWriteWireguard:   canWriteWireguard != 0,
			CanReadConfig:       canReadConfig != 0,
			CanWriteConfig:      canWriteConfig != 0,
			CanReadSettings:     canReadSettings != 0,
			CanWriteSettings:    canWriteSettings != 0,
			CanReadPanelUsers:   canReadPanelUsers != 0,
			CanWritePanelUsers:  canWritePanelUsers != 0,
			CanReadLogs:         canReadLogs != 0,
			CanReadLogsCensored: canReadLogsCensored != 0,
		}
		perms.Normalize()

		result = append(result, PanelUserInfo{
			Username:    username,
			Permissions: perms,
			CreatedAt:   createdAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Store) UpdatePanelUserPassword(username, newPassword string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.Queries.UpdatePanelUserPassword(context.Background(), sqlcStore.UpdatePanelUserPasswordParams{
		PasswordHash: string(hash),
		Username:     username,
	})
}

func (s *Store) UpdatePanelUserPermissions(username string, perms PanelUserPermissions) error {
	perms.Normalize()
	_, err := s.db.ExecContext(context.Background(), `
		UPDATE panel_users SET
			can_read_users = ?,
			can_write_users = ?,
			can_read_wireguard = ?,
			can_write_wireguard = ?,
			can_read_config = ?,
			can_write_config = ?,
			can_read_settings = ?,
			can_write_settings = ?,
			can_read_panel_users = ?,
			can_write_panel_users = ?,
			can_read_logs = ?,
			can_read_logs_censored = ?,
			updated_at = strftime('%s','now')
		WHERE username = ?`,
		boolToInt64(perms.CanReadUsers),
		boolToInt64(perms.CanWriteUsers),
		boolToInt64(perms.CanReadWireguard),
		boolToInt64(perms.CanWriteWireguard),
		boolToInt64(perms.CanReadConfig),
		boolToInt64(perms.CanWriteConfig),
		boolToInt64(perms.CanReadSettings),
		boolToInt64(perms.CanWriteSettings),
		boolToInt64(perms.CanReadPanelUsers),
		boolToInt64(perms.CanWritePanelUsers),
		boolToInt64(perms.CanReadLogs),
		boolToInt64(perms.CanReadLogsCensored),
		username,
	)
	return err
}

func (s *Store) GetPanelUserSubscriptionDefaults(ctx context.Context, username string) (SubscriptionDefaults, error) {
	row, err := s.Queries.GetPanelUserSubscriptionDefaults(ctx, username)
	if err != nil {
		return SubscriptionDefaults{}, err
	}

	defaults := SubscriptionDefaults{
		ProfileUpdateIntervalHours: nullableInt64Ptr(row.SubscriptionDefaultProfileUpdateIntervalHours),
		UpdateAlways:               row.SubscriptionDefaultUpdateAlways != 0,
		Destinations:               []string{},
	}

	if raw := strings.TrimSpace(row.SubscriptionDefaultDestinationsJson); raw != "" {
		if err := json.Unmarshal([]byte(raw), &defaults.Destinations); err != nil {
			return SubscriptionDefaults{}, err
		}
	}

	return defaults, nil
}

func (s *Store) UpdatePanelUserSubscriptionDefaults(ctx context.Context, username string, defaults SubscriptionDefaults) error {
	destinationsJSON, err := json.Marshal(defaults.Destinations)
	if err != nil {
		return err
	}

	return s.Queries.UpdatePanelUserSubscriptionDefaults(ctx, sqlcStore.UpdatePanelUserSubscriptionDefaultsParams{
		SubscriptionDefaultProfileUpdateIntervalHours: nullableInt64(defaults.ProfileUpdateIntervalHours),
		SubscriptionDefaultUpdateAlways:               boolToInt64(defaults.UpdateAlways),
		SubscriptionDefaultDestinationsJson:           string(destinationsJSON),
		Username:                                      username,
	})
}

func (s *Store) GetDashboardPreferences(ctx context.Context, principal string) (DashboardPreferences, error) {
	defaults := DefaultDashboardPreferences()
	row := s.db.QueryRowContext(ctx, `
		SELECT default_service, refresh_ms, default_range, active_user_window_minutes, detail_chart_target_points
		FROM dashboard_preferences
		WHERE principal = ?
	`, principal)

	var prefs DashboardPreferences
	if err := row.Scan(&prefs.DefaultService, &prefs.RefreshMs, &prefs.DefaultRange, &prefs.ActiveUserWindowMinutes, &prefs.DetailChartTargetPoints); err != nil {
		if err == sql.ErrNoRows {
			return defaults, nil
		}
		return defaults, err
	}
	return normalizeDashboardPreferences(prefs), nil
}

func (s *Store) UpdateDashboardPreferences(ctx context.Context, principal string, prefs DashboardPreferences) error {
	prefs = normalizeDashboardPreferences(prefs)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO dashboard_preferences (
			principal, default_service, refresh_ms, default_range, active_user_window_minutes, detail_chart_target_points, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, strftime('%s','now'), strftime('%s','now'))
		ON CONFLICT(principal) DO UPDATE SET
			default_service = excluded.default_service,
			refresh_ms = excluded.refresh_ms,
			default_range = excluded.default_range,
			active_user_window_minutes = excluded.active_user_window_minutes,
			detail_chart_target_points = excluded.detail_chart_target_points,
			updated_at = strftime('%s','now')
	`, principal, prefs.DefaultService, prefs.RefreshMs, prefs.DefaultRange, prefs.ActiveUserWindowMinutes, prefs.DetailChartTargetPoints)
	return err
}

func (s *Store) UpdatePanelUsername(oldUsername, newUsername string) error {
	count, err := s.Queries.CheckPanelUserExists(context.Background(), newUsername)
	if err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("username %s already exists", newUsername)
	}
	if err := s.Queries.UpdatePanelUsername(context.Background(), sqlcStore.UpdatePanelUsernameParams{
		Username:   newUsername,
		Username_2: oldUsername,
	}); err != nil {
		return err
	}
	if _, err := s.db.Exec("UPDATE dashboard_preferences SET principal = ?, updated_at = strftime('%s','now') WHERE principal = ?", newUsername, oldUsername); err != nil {
		return err
	}
	return nil
}

func (s *Store) DeletePanelUser(username string) error {
	if _, err := s.db.Exec("DELETE FROM dashboard_preferences WHERE principal = ?", username); err != nil {
		return err
	}
	return s.Queries.DeletePanelUser(context.Background(), username)
}

// EnsureDefaultPanelUser migrates existing admins to panel_users and/or bootstraps the initial superuser.
func (s *Store) EnsureDefaultPanelUser() error {
	// Collect existing admins first, then close the cursor BEFORE any INSERT.
	// With SetMaxOpenConns(1), keeping rows open while calling db.Exec deadlocks SQLite.
	type adminEntry struct{ username, passwordHash string }
	var toMigrate []adminEntry

	rows, err := s.db.Query("SELECT username, password_hash FROM admins")
	if err == nil {
		for rows.Next() {
			var uname, phash string
			if err := rows.Scan(&uname, &phash); err != nil {
				continue
			}
			toMigrate = append(toMigrate, adminEntry{uname, phash})
		}
		rows.Close()
	}

	for _, a := range toMigrate {
		s.db.Exec(`
			INSERT OR IGNORE INTO panel_users
				(username, password_hash,
				 can_read_users, can_write_users,
				 can_read_wireguard, can_write_wireguard,
				 can_read_config, can_write_config,
				 can_read_settings, can_write_settings,
				 can_read_panel_users, can_write_panel_users,
				 can_read_logs,
				 created_at, updated_at)
			VALUES (?, ?,
			        1, 1,
			        1, 1,
			        1, 1,
			        1, 1,
			        1, 1,
			        1,
			        strftime('%s','now'), strftime('%s','now'))
		`, a.username, a.passwordHash)
	}

	// Bootstrap from env if still empty
	count, err := s.Queries.CountPanelUsers(context.Background())
	if err != nil {
		return err
	}
	if count == 0 {
		username := strings.TrimSpace(os.Getenv("OGS_ADMIN_USER"))
		if username == "" {
			username = "admin"
		}
		password := os.Getenv("OGS_ADMIN_PASSWORD")
		if strings.TrimSpace(password) == "" {
			return fmt.Errorf("no panel user found and OGS_ADMIN_PASSWORD is empty; set it to bootstrap initial credentials")
		}
		allPerms := fullPanelUserPermissions()
		return s.CreatePanelUser(username, password, allPerms)
	}
	return nil
}

// Admin Management

func (s *Store) CreateAdmin(username, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.Queries.CreateAdmin(context.Background(), sqlcStore.CreateAdminParams{
		Username:     username,
		PasswordHash: string(hash),
	})
}

func (s *Store) VerifyAdmin(username, password string) (bool, error) {
	hash, err := s.Queries.GetAdmin(context.Background(), username)
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
	return s.Queries.UpdateAdminPassword(context.Background(), sqlcStore.UpdateAdminPasswordParams{
		PasswordHash: string(hash),
		Username:     username,
	})
}

func (s *Store) UpdateAdminUsername(oldUsername, newUsername string) error {
	count, err := s.Queries.CheckAdminExists(context.Background(), newUsername)
	if err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("username %s already exists", newUsername)
	}

	return s.Queries.UpdateAdminUsername(context.Background(), sqlcStore.UpdateAdminUsernameParams{
		Username:   newUsername,
		Username_2: oldUsername,
	})
}

func (s *Store) SaveInboundMeta(meta InboundMeta) error {
	if meta.Tag == "" {
		return fmt.Errorf("inbound tag required")
	}
	if meta.ExternalPort <= 0 &&
		strings.TrimSpace(meta.ClientSNI) == "" &&
		meta.LinkAllowInsecure == nil &&
		strings.TrimSpace(meta.OverrideAddress) == "" {
		return s.DeleteInboundMeta(meta.Tag)
	}

	var externalPort sql.NullInt64
	if meta.ExternalPort > 0 {
		externalPort = sql.NullInt64{Int64: int64(meta.ExternalPort), Valid: true}
	}
	var clientSNI sql.NullString
	if strings.TrimSpace(meta.ClientSNI) != "" {
		clientSNI = sql.NullString{String: strings.TrimSpace(meta.ClientSNI), Valid: true}
	}
	var linkAllowInsecure sql.NullInt64
	if meta.LinkAllowInsecure != nil {
		if *meta.LinkAllowInsecure {
			linkAllowInsecure = sql.NullInt64{Int64: 1, Valid: true}
		} else {
			linkAllowInsecure = sql.NullInt64{Int64: 0, Valid: true}
		}
	}
	var overrideAddress sql.NullString
	if strings.TrimSpace(meta.OverrideAddress) != "" {
		overrideAddress = sql.NullString{String: strings.TrimSpace(meta.OverrideAddress), Valid: true}
	}

	_, err := s.db.ExecContext(
		context.Background(),
		`INSERT INTO inbound_meta (tag, external_port, client_sni, link_allow_insecure, override_address)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(tag) DO UPDATE SET
		   external_port = excluded.external_port,
		   client_sni = excluded.client_sni,
		   link_allow_insecure = excluded.link_allow_insecure,
		   override_address = excluded.override_address`,
		meta.Tag,
		externalPort,
		clientSNI,
		linkAllowInsecure,
		overrideAddress,
	)
	return err
}

func (s *Store) GetInboundMeta(tag string) (*InboundMeta, error) {
	if tag == "" {
		return nil, nil
	}
	var meta InboundMeta
	var externalPort sql.NullInt64
	var clientSNI sql.NullString
	var linkAllowInsecure sql.NullInt64
	var overrideAddress sql.NullString
	err := s.db.QueryRowContext(
		context.Background(),
		`SELECT tag, external_port, client_sni, link_allow_insecure, override_address FROM inbound_meta WHERE tag = ?`,
		tag,
	).Scan(&meta.Tag, &externalPort, &clientSNI, &linkAllowInsecure, &overrideAddress)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	meta.ExternalPort = int(externalPort.Int64)
	meta.ClientSNI = strings.TrimSpace(clientSNI.String)
	if linkAllowInsecure.Valid {
		value := linkAllowInsecure.Int64 != 0
		meta.LinkAllowInsecure = &value
	}
	meta.OverrideAddress = strings.TrimSpace(overrideAddress.String)
	return &meta, nil
}

func (s *Store) GetAllInboundMeta() (map[string]InboundMeta, error) {
	rows, err := s.db.QueryContext(
		context.Background(),
		`SELECT tag, external_port, client_sni, link_allow_insecure, override_address FROM inbound_meta`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	meta := make(map[string]InboundMeta)
	for rows.Next() {
		var entry InboundMeta
		var externalPort sql.NullInt64
		var clientSNI sql.NullString
		var linkAllowInsecure sql.NullInt64
		var overrideAddress sql.NullString
		if err := rows.Scan(&entry.Tag, &externalPort, &clientSNI, &linkAllowInsecure, &overrideAddress); err != nil {
			return nil, err
		}
		entry.ExternalPort = int(externalPort.Int64)
		entry.ClientSNI = strings.TrimSpace(clientSNI.String)
		if linkAllowInsecure.Valid {
			value := linkAllowInsecure.Int64 != 0
			entry.LinkAllowInsecure = &value
		}
		entry.OverrideAddress = strings.TrimSpace(overrideAddress.String)
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
	return s.Queries.DeleteInboundMeta(context.Background(), tag)
}

func (s *Store) RenameInboundMeta(oldTag, newTag string) error {
	if oldTag == "" || newTag == "" || oldTag == newTag {
		return nil
	}
	return s.Queries.RenameInboundMeta(context.Background(), sqlcStore.RenameInboundMetaParams{
		Tag:   newTag,
		Tag_2: oldTag,
	})
}

func renameInboundTagReferences(tags []string, oldTag, newTag string) ([]string, bool) {
	if oldTag == "" || newTag == "" || oldTag == newTag {
		return tags, false
	}
	changed := false
	out := make([]string, len(tags))
	copy(out, tags)
	for i, tag := range out {
		if tag == oldTag {
			out[i] = newTag
			changed = true
		}
	}
	return out, changed
}

func (s *Store) RenameInboundReferences(oldTag, newTag string) error {
	if oldTag == "" || newTag == "" || oldTag == newTag {
		return nil
	}

	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	qtx := s.Queries.WithTx(tx)
	if err := qtx.RenameInboundMeta(ctx, sqlcStore.RenameInboundMetaParams{
		Tag:   newTag,
		Tag_2: oldTag,
	}); err != nil {
		return err
	}

	rows, err := tx.QueryContext(ctx, "SELECT email, COALESCE(inbound_tags, '') FROM users")
	if err != nil {
		return err
	}

	type inboundTagUpdate struct {
		email   string
		payload string
	}
	updates := make([]inboundTagUpdate, 0)

	for rows.Next() {
		var email, tagsJSON string
		if err := rows.Scan(&email, &tagsJSON); err != nil {
			rows.Close()
			return err
		}

		if tagsJSON == "" || tagsJSON == "[]" {
			continue
		}

		var tags []string
		if err := json.Unmarshal([]byte(tagsJSON), &tags); err != nil {
			return fmt.Errorf("decode inbound_tags for %s: %w", email, err)
		}

		renamedTags, changed := renameInboundTagReferences(tags, oldTag, newTag)
		if !changed {
			continue
		}

		payload, err := json.Marshal(renamedTags)
		if err != nil {
			rows.Close()
			return fmt.Errorf("encode inbound_tags for %s: %w", email, err)
		}
		updates = append(updates, inboundTagUpdate{email: email, payload: string(payload)})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}

	for _, update := range updates {
		if _, err := tx.ExecContext(ctx, "UPDATE users SET inbound_tags = ? WHERE email = ?", update.payload, update.email); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *Store) EnsureDefaultAdmin() error {
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM admins").Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		username := strings.TrimSpace(os.Getenv("OGS_ADMIN_USER"))
		if username == "" {
			username = "admin"
		}
		password := os.Getenv("OGS_ADMIN_PASSWORD")
		if strings.TrimSpace(password) == "" {
			return fmt.Errorf("no admin found and OGS_ADMIN_PASSWORD is empty; set OGS_ADMIN_PASSWORD to bootstrap initial admin credentials")
		}
		return s.CreateAdmin(username, password)
	}
	return nil
}

func (s *Store) HasSamples() (bool, error) {
	count, err := s.Queries.CountSamples(context.Background())
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *Store) TruncateSamples() error {
	return s.Queries.TruncateSamples(context.Background())
}

func (s *Store) GetMaxTimestamp() (int64, error) {
	max, err := s.Queries.GetMaxTimestamp(context.Background())
	if err != nil {
		return 0, err
	}
	if max == nil {
		return 0, nil
	}
	switch v := max.(type) {
	case int64:
		return v, nil
	case sql.NullInt64:
		if v.Valid {
			return v.Int64, nil
		}
	}
	return 0, nil
}

func (s *Store) GetMaxTimestampForUser(user string) (int64, error) {
	max, err := s.Queries.GetMaxTimestampForUser(context.Background(), user)
	if err != nil {
		return 0, err
	}
	if max == nil {
		return 0, nil
	}
	switch v := max.(type) {
	case int64:
		return v, nil
	case sql.NullInt64:
		if v.Valid {
			return v.Int64, nil
		}
	}
	return 0, nil
}

func (s *Store) PruneOlderThan(ts int64) error {
	return s.Queries.PruneSamplesOlderThan(context.Background(), ts)
}

func (s *Store) PruneSubscriptionRequestsOlderThan(ts int64) error {
	return s.Queries.PruneSubscriptionRequestsOlderThan(context.Background(), ts)
}

func (s *Store) CountSamples() (int64, error) {
	c1, _ := s.Queries.CountSamples(context.Background())
	c2, _ := s.Queries.CountWGSamples(context.Background())
	c3, _ := s.Queries.CountDailyUsage(context.Background())
	c4, _ := s.Queries.CountWGDailyUsage(context.Background())
	c5, _ := s.Queries.CountSubscriptionRequests(context.Background())
	return c1 + c2 + c3 + c4 + c5, nil
}

type SamplerRun struct {
	Timestamp  int64  `json:"timestamp"`
	DurationMs int64  `json:"duration_ms"`
	Inserted   int64  `json:"inserted"`
	Error      string `json:"error"`
	Source     string `json:"source"`
}

func (s *Store) LogSamplerRun(ts int64, durationMs int64, inserted int64, errStr string, source string) {
	err := s.Queries.InsertSamplerRun(context.Background(), sqlcStore.InsertSamplerRunParams{
		Ts:         ts,
		DurationMs: durationMs,
		Inserted:   inserted,
		Error: sql.NullString{
			String: errStr,
			Valid:  errStr != "",
		},
		Source: sql.NullString{
			String: source,
			Valid:  source != "",
		},
	})
	if err != nil {
		log.Printf("Warning: Failed to log sampler run: %v", err)
	}
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
	enabled := int64(0)
	if meta.Enabled {
		enabled = 1
	}
	// Reset day is fixed to day 1 of each month.
	resetDay := int64(1)
	if err := s.Queries.UpsertUser(context.Background(), sqlcStore.UpsertUserParams{
		Email: meta.Email,
		QuotaLimit: sql.NullInt64{
			Int64: meta.QuotaLimit,
			Valid: true,
		},
		QuotaPeriod: sql.NullString{
			String: meta.QuotaPeriod,
			Valid:  true,
		},
		ResetDay: sql.NullInt64{
			Int64: resetDay,
			Valid: true,
		},
		Enabled: sql.NullInt64{
			Int64: enabled,
			Valid: true,
		},
		Credential: sql.NullString{
			String: meta.Credential,
			Valid:  true,
		},
		Flow: sql.NullString{
			String: meta.Flow,
			Valid:  true,
		},
		VmessSecurity: sql.NullString{
			String: meta.VmessSecurity,
			Valid:  true,
		},
		VmessAlterID: sql.NullInt64{
			Int64: int64(meta.VmessAlterID),
			Valid: true,
		},
	}); err != nil {
		return err
	}

	// Persist InboundTags as JSON in the inbound_tags column (not covered by sqlc-generated query).
	tagsJSON := "[]"
	if len(meta.InboundTags) > 0 {
		b, err := json.Marshal(meta.InboundTags)
		if err != nil {
			return fmt.Errorf("failed to marshal inbound_tags: %w", err)
		}
		tagsJSON = string(b)
	}
	_, err := s.db.Exec("UPDATE users SET inbound_tags = ? WHERE email = ?", tagsJSON, meta.Email)
	return err
}

func (s *Store) GetUserMetadata(email string) (*UserMetadata, error) {
	meta, err := s.Queries.GetUser(context.Background(), email)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	// Read inbound_tags from the column not covered by sqlc-generated query.
	var tagsJSON string
	_ = s.db.QueryRow("SELECT COALESCE(inbound_tags, '') FROM users WHERE email = ?", email).Scan(&tagsJSON)
	var inboundTags []string
	if tagsJSON != "" && tagsJSON != "[]" {
		if err := json.Unmarshal([]byte(tagsJSON), &inboundTags); err != nil {
			inboundTags = []string{}
		}
	}
	if inboundTags == nil {
		inboundTags = []string{}
	}

	return &UserMetadata{
		Email:         meta.Email,
		QuotaLimit:    meta.QuotaLimit.Int64,
		QuotaPeriod:   meta.QuotaPeriod.String,
		ResetDay:      int(meta.ResetDay.Int64),
		Enabled:       meta.Enabled.Int64 != 0,
		Credential:    meta.Credential.String,
		Flow:          meta.Flow.String,
		VmessSecurity: meta.VmessSecurity.String,
		VmessAlterID:  int(meta.VmessAlterID.Int64),
		InboundTags:   inboundTags,
	}, nil
}

func (s *Store) DeleteUserMetadata(email string) error {
	return s.Queries.DeleteUser(context.Background(), email)
}

func (s *Store) RemoveUserFromSubscriptions(email string) error {
	return s.Queries.RemoveUserFromAllSubscriptions(context.Background(), strings.TrimSpace(email))
}

func (s *Store) RenameUserTrafficIdentity(oldName, newName string) error {
	oldName = strings.TrimSpace(oldName)
	newName = strings.TrimSpace(newName)
	if oldName == "" || newName == "" {
		return fmt.Errorf("old and new user names are required")
	}
	if oldName == newName {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("PRAGMA defer_foreign_keys = ON;"); err != nil {
		return err
	}

	for _, conflict := range []struct {
		table  string
		column string
	}{
		{table: "users", column: "email"},
		{table: "samples", column: "user"},
		{table: "daily_usage", column: "user"},
	} {
		var exists int
		query := fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s WHERE %s = ?)", conflict.table, conflict.column)
		if err := tx.QueryRow(query, newName).Scan(&exists); err != nil {
			return err
		}
		if exists != 0 {
			return fmt.Errorf("destination identity %q already exists in %s", newName, conflict.table)
		}
	}

	if _, err := tx.Exec("UPDATE samples SET user = ? WHERE user = ?", newName, oldName); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE daily_usage SET user = ? WHERE user = ?", newName, oldName); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE users SET email = ? WHERE email = ?", newName, oldName); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE subscription_users SET user_name = ? WHERE user_name = ?", newName, oldName); err != nil {
		return err
	}

	return tx.Commit()
}

func (s *Store) GetAllUserMetadata() ([]UserMetadata, error) {
	rows, err := s.Queries.GetAllUsers(context.Background())
	if err != nil {
		return nil, err
	}

	// Build a map of inbound_tags per email using a single raw query.
	tagsMap := make(map[string][]string)
	tagRows, err := s.db.Query("SELECT email, COALESCE(inbound_tags, '') FROM users")
	if err == nil {
		defer tagRows.Close()
		for tagRows.Next() {
			var email, tagsJSON string
			if err := tagRows.Scan(&email, &tagsJSON); err != nil {
				continue
			}
			var tags []string
			if tagsJSON != "" && tagsJSON != "[]" {
				if err := json.Unmarshal([]byte(tagsJSON), &tags); err != nil {
					tags = []string{}
				}
			}
			if tags == nil {
				tags = []string{}
			}
			tagsMap[email] = tags
		}
	}

	var result []UserMetadata
	for _, meta := range rows {
		tags := tagsMap[meta.Email]
		if tags == nil {
			tags = []string{}
		}
		result = append(result, UserMetadata{
			Email:         meta.Email,
			QuotaLimit:    meta.QuotaLimit.Int64,
			QuotaPeriod:   meta.QuotaPeriod.String,
			ResetDay:      int(meta.ResetDay.Int64),
			Enabled:       meta.Enabled.Int64 != 0,
			Credential:    meta.Credential.String,
			Flow:          meta.Flow.String,
			VmessSecurity: meta.VmessSecurity.String,
			VmessAlterID:  int(meta.VmessAlterID.Int64),
			InboundTags:   tags,
		})
	}
	return result, nil
}

func (s *Store) GetLastSeenMap() (map[string]int64, error) {
	rows, err := s.Queries.GetLastSeenMap(context.Background())
	if err != nil {
		return nil, err
	}
	result := make(map[string]int64)
	for _, r := range rows {
		if r.MaxTs != nil {
			switch v := r.MaxTs.(type) {
			case int64:
				result[r.User] = v
			case float64:
				result[r.User] = int64(v)
			}
		}
	}
	return result, nil
}

func (s *Store) GetLastSeenUser(user string) (int64, error) {
	max, err := s.Queries.GetMaxTimestampForUser(context.Background(), user)
	if err != nil {
		return 0, err
	}
	if max == nil {
		return 0, nil
	}
	switch v := max.(type) {
	case int64:
		return v, nil
	case sql.NullInt64:
		if v.Valid {
			return v.Int64, nil
		}
	}
	return 0, nil
}

func (s *Store) GetLastSeenUserWithTraffic(user string) (int64, error) {
	max, err := s.Queries.GetLastSeenUserWithTraffic(context.Background(), user)
	if err != nil {
		return 0, err
	}
	if max == nil {
		return 0, nil
	}
	switch v := max.(type) {
	case int64:
		return v, nil
	case sql.NullInt64:
		if v.Valid {
			return v.Int64, nil
		}
	}
	return 0, nil
}

func (s *Store) GetLastSeenWithThreshold(user string, threshold int64) (int64, error) {
	if threshold <= 0 {
		return s.GetLastSeenUserWithTraffic(user)
	}
	max, err := s.Queries.GetLastSeenUserAndThreshold(context.Background(), sqlcStore.GetLastSeenUserAndThresholdParams{
		User:   user,
		Uplink: threshold,
	})
	if err != nil {
		return 0, err
	}
	if max == nil {
		return 0, nil
	}
	switch v := max.(type) {
	case int64:
		return v, nil
	case float64:
		return int64(v), nil
	case sql.NullFloat64:
		if v.Valid {
			return int64(v.Float64), nil
		}
	}
	return 0, nil
}

func (s *Store) GetActiveUsers(duration time.Duration) ([]string, error) {
	cutoff := time.Now().Add(-duration).Unix()
	return s.Queries.GetActiveUsersWithTraffic(context.Background(), cutoff)
}

func (s *Store) GetActiveUsersWithThreshold(duration time.Duration, threshold int64) ([]string, error) {
	if threshold <= 0 {
		return s.GetActiveUsers(duration)
	}
	cutoff := time.Now().Add(-duration).Unix()
	rows, err := s.Queries.GetActiveUsersWithThreshold(context.Background(), sqlcStore.GetActiveUsersWithThresholdParams{
		Ts:     cutoff,
		Uplink: threshold,
	})
	if err != nil {
		return nil, err
	}
	var users []string
	for _, r := range rows {
		users = append(users, r.User)
	}
	return users, nil
}

func (s *Store) AddSample(sample Sample) error {
	return s.Queries.InsertSample(context.Background(), sqlcStore.InsertSampleParams{
		User:     sample.User,
		Ts:       sample.Timestamp,
		Uplink:   sample.Uplink,
		Downlink: sample.Downlink,
	})
}

func (s *Store) BulkInsert(samples []Sample) error {
	if len(samples) == 0 {
		return nil
	}
	// Limit batch size to prevent memory issues and DoS
	const maxBatchSize = 10000
	if len(samples) > maxBatchSize {
		samples = samples[:maxBatchSize]
		log.Printf("Warning: BulkInsert limited to %d samples", maxBatchSize)
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
	rows, err := s.Queries.GetSamplesForUser(context.Background(), sqlcStore.GetSamplesForUserParams{
		User: user,
		Ts:   start,
		Ts_2: end,
	})
	if err != nil {
		return nil, err
	}
	var samples []Sample
	for _, smp := range rows {
		samples = append(samples, Sample{
			User:      smp.User,
			Timestamp: smp.Ts,
			Uplink:    smp.Uplink,
			Downlink:  smp.Downlink,
		})
	}
	return samples, nil
}

func (s *Store) GetGlobalTraffic(start, end int64) ([]TrafficPoint, error) {
	rows, err := s.Queries.GetGlobalTraffic(context.Background(), sqlcStore.GetGlobalTrafficParams{
		Ts:   start,
		Ts_2: end,
	})
	if err != nil {
		return nil, err
	}
	var points []TrafficPoint
	for _, p := range rows {
		points = append(points, TrafficPoint{
			Timestamp: p.Ts,
			Uplink:    int64(p.Up.Float64),
			Downlink:  int64(p.Down.Float64),
		})
	}
	return points, nil
}

func (s *Store) GetActiveUserCount(duration time.Duration) (int64, error) {
	cutoff := time.Now().Add(-duration).Unix()
	return s.Queries.GetActiveUserCount(context.Background(), cutoff)
}

func (s *Store) GetActiveUserCountWithThreshold(duration time.Duration, threshold int64) (int64, error) {
	if threshold <= 0 {
		return s.GetActiveUserCount(duration)
	}
	cutoff := time.Now().Add(-duration).Unix()
	return s.Queries.GetActiveUserCountWithThreshold(context.Background(), sqlcStore.GetActiveUserCountWithThresholdParams{
		Ts:     cutoff,
		Uplink: threshold,
	})
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
	qtx := s.Queries.WithTx(tx)
	for _, smp := range samples {
		err := qtx.InsertWGSample(context.Background(), sqlcStore.InsertWGSampleParams{
			PublicKey: smp.PublicKey,
			Ts:        smp.Timestamp,
			Rx:        smp.Rx,
			Tx:        smp.Tx,
			Endpoint: sql.NullString{
				String: smp.Endpoint,
				Valid:  smp.Endpoint != "",
			},
		})
		if err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// RunWGSampleTx persists a WireGuard sampling batch atomically: updates peer
// handshake timestamps and inserts new traffic samples in a single transaction.
func (s *Store) RunWGSampleTx(handshakes map[string]int64, samples []WGSample) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	qtx := s.Queries.WithTx(tx)

	for key, ts := range handshakes {
		if key == "" || ts <= 0 {
			continue
		}
		if err := qtx.UpdateWGPeerHandshake(context.Background(), sqlcStore.UpdateWGPeerHandshakeParams{
			LastHandshake: sql.NullInt64{Int64: ts, Valid: true},
			PublicKey:     key,
		}); err != nil {
			return err
		}
	}

	if len(samples) > 5000 {
		samples = samples[:5000]
	}
	for _, smp := range samples {
		if err := qtx.InsertWGSample(context.Background(), sqlcStore.InsertWGSampleParams{
			PublicKey: smp.PublicKey,
			Ts:        smp.Timestamp,
			Rx:        smp.Rx,
			Tx:        smp.Tx,
			Endpoint: sql.NullString{
				String: smp.Endpoint,
				Valid:  smp.Endpoint != "",
			},
		}); err != nil {
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
type TrafficTotal struct {
	Key   string
	Total int64
	Rx    int64
	Tx    int64
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
	rows, err := s.Queries.GetAllUsers(context.Background())
	if err != nil {
		return nil, err
	}
	var users []User
	for _, u := range rows {
		users = append(users, User{
			Uuid:        "", // Optional since we dropped UUID requirement, keeping for compatibility
			Username:    u.Email,
			DataLimit:   u.QuotaLimit.Int64,
			QuotaPeriod: u.QuotaPeriod.String,
			ResetDay:    int(u.ResetDay.Int64),
			Enabled:     u.Enabled.Int64 == 1,
		})
	}
	return users, nil
}

// GetTrafficPerUser returns aggregated usage per user for the given time range.
func (s *Store) GetTrafficPerUser(start, end int64) (map[string]TrafficStats, error) {
	rows, err := s.Queries.GetTrafficPerUser(context.Background(), sqlcStore.GetTrafficPerUserParams{
		Ts:   start,
		Ts_2: end,
	})
	if err != nil {
		return nil, err
	}
	result := make(map[string]TrafficStats)
	for _, r := range rows {
		result[r.User] = TrafficStats{
			Uplink:   int64(r.Up.Float64),
			Downlink: int64(r.Down.Float64),
		}
	}
	return result, nil
}

// GetWGTrafficDelta returns rx/tx delta between first and last sample in the range.
func (s *Store) GetWGTrafficDelta(publicKey string, start, end int64) (int64, int64, error) {
	if publicKey == "" {
		return 0, 0, nil
	}
	firstSmp, err1 := s.Queries.GetWGBoundarySamples(context.Background(), sqlcStore.GetWGBoundarySamplesParams{
		PublicKey: publicKey,
		Ts:        start,
		Ts_2:      end,
	})
	if err1 != nil {
		return 0, 0, err1
	}

	lastSmp, err2 := s.Queries.GetWGLastBoundarySample(context.Background(), sqlcStore.GetWGLastBoundarySampleParams{
		PublicKey: publicKey,
		Ts:        start,
		Ts_2:      end,
	})
	if err2 != nil {
		return 0, 0, err2
	}

	if len(firstSmp) == 0 || len(lastSmp) == 0 {
		return 0, 0, nil
	}

	deltaRx := lastSmp[0].Rx - firstSmp[0].Rx
	deltaTx := lastSmp[0].Tx - firstSmp[0].Tx
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

	rows, err := s.Queries.GetWGTrafficSeries(context.Background(), sqlcStore.GetWGTrafficSeriesParams{
		PublicKey: publicKey,
		Ts:        start,
		Ts_2:      end,
		Limit:     int64(limit),
	})
	if err != nil {
		return nil, err
	}

	var series []WGSample
	for _, r := range rows {
		series = append(series, WGSample{
			PublicKey: r.PublicKey,
			Timestamp: r.Ts,
			Rx:        r.Rx,
			Tx:        r.Tx,
			Endpoint:  r.Endpoint,
		})
	}
	return series, nil
}

type WGPeerMeta struct {
	PublicKey     string
	Alias         string
	LastHandshake int64
	Deleted       bool
}

func (s *Store) UpsertWGPeer(publicKey, alias string, deleted bool) error {
	if publicKey == "" || alias == "" {
		return fmt.Errorf("public_key and alias are required")
	}
	deletedVal := int64(0)
	if deleted {
		deletedVal = 1
	}
	return s.Queries.UpsertWGPeer(context.Background(), sqlcStore.UpsertWGPeerParams{
		PublicKey: publicKey,
		Alias:     alias,
		Deleted: sql.NullInt64{
			Int64: deletedVal,
			Valid: true,
		},
	})
}

func (s *Store) UpdateWGPeerHandshakes(handshakes map[string]int64) error {
	if len(handshakes) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	qtx := s.Queries.WithTx(tx)
	for key, ts := range handshakes {
		if key == "" || ts <= 0 {
			continue
		}
		if err := qtx.UpdateWGPeerHandshake(context.Background(), sqlcStore.UpdateWGPeerHandshakeParams{
			LastHandshake: sql.NullInt64{
				Int64: ts,
				Valid: true,
			},
			PublicKey: key,
		}); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *Store) GetWGPeerMeta() (map[string]WGPeerMeta, error) {
	rows, err := s.Queries.GetAllWGPeers(context.Background())
	if err != nil {
		return nil, err
	}

	result := make(map[string]WGPeerMeta)
	for _, m := range rows {
		result[m.PublicKey] = WGPeerMeta{
			PublicKey:     m.PublicKey,
			Alias:         m.Alias,
			LastHandshake: m.LastHandshake.Int64,
			Deleted:       m.Deleted.Int64 == 1,
		}
	}
	return result, nil
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
func (s *Store) GetWGTopTotals(start, end int64, limit int) ([]TrafficTotal, error) {
	rows, err := s.db.Query(`
WITH ordered AS (
  SELECT
    public_key,
    ts,
    rx,
    tx,
    LAG(rx) OVER (PARTITION BY public_key ORDER BY ts) AS prev_rx,
    LAG(tx) OVER (PARTITION BY public_key ORDER BY ts) AS prev_tx
  FROM wg_samples
  WHERE ts >= ? AND ts <= ?
),
diffs AS (
  SELECT
    public_key,
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
  public_key,
  SUM(dr) AS rx_delta,
  SUM(dx) AS tx_delta
FROM diffs
GROUP BY public_key
ORDER BY (rx_delta + tx_delta) DESC
LIMIT ?`, start, end, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := []TrafficTotal{}
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
		results = append(results, TrafficTotal{
			Key:   pub,
			Rx:    r,
			Tx:    t,
			Total: r + t,
		})
	}
	return results, nil
}

// GetSBTrafficBuckets aggregates Sing-box traffic per time bucket.
func (s *Store) GetSBTrafficBuckets(start, end, interval int64) (map[int64]TrafficStats, error) {
	out := make(map[int64]TrafficStats)
	if interval <= 0 {
		interval = 60
	}
	rows, err := s.db.Query(`
		SELECT (ts / ?) * ? AS bucket_ts, SUM(uplink), SUM(downlink)
		FROM samples
		WHERE ts >= ? AND ts <= ?
		GROUP BY bucket_ts
		ORDER BY bucket_ts ASC
	`, interval, interval, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ts int64
		var up, down sql.NullInt64
		if err := rows.Scan(&ts, &up, &down); err != nil {
			return nil, err
		}
		out[ts] = TrafficStats{Uplink: up.Int64, Downlink: down.Int64}
	}
	return out, nil
}

func (s *Store) GetSBUserTrafficBuckets(user string, start, end, interval int64) (map[int64]TrafficStats, error) {
	out := make(map[int64]TrafficStats)
	if interval <= 0 {
		interval = 60
	}
	rows, err := s.db.Query(`
		SELECT (ts / ?) * ? AS bucket_ts, SUM(uplink), SUM(downlink)
		FROM (
			SELECT ts, uplink, downlink
			FROM samples
			WHERE user = ? AND ts >= ? AND ts <= ?

			UNION ALL

			SELECT ts, uplink, downlink
			FROM daily_usage
			WHERE user = ? AND ts >= ? AND ts <= ?
		)
		GROUP BY bucket_ts
		ORDER BY bucket_ts ASC
	`, interval, interval, user, start, end, user, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ts int64
		var up, down sql.NullInt64
		if err := rows.Scan(&ts, &up, &down); err != nil {
			return nil, err
		}
		out[ts] = TrafficStats{Uplink: up.Int64, Downlink: down.Int64}
	}
	return out, nil
}

// GetSBTopTotals aggregates Sing-box usage per user in the range.
func (s *Store) GetSBTopTotals(start, end int64, limit int) ([]TrafficTotal, error) {
	rows, err := s.db.Query(`
		SELECT user, SUM(uplink) AS up, SUM(downlink) AS down
		FROM (
			SELECT user, uplink, downlink
			FROM samples
			WHERE ts >= ? AND ts <= ?

			UNION ALL

			SELECT user, uplink, downlink
			FROM daily_usage
			WHERE ts >= ? AND ts <= ?
		)
		GROUP BY user
		ORDER BY (up + down) DESC
		LIMIT ?
	`, start, end, start, end, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	res := []TrafficTotal{}
	for rows.Next() {
		var key string
		var up, down sql.NullInt64
		if err := rows.Scan(&key, &up, &down); err != nil {
			return nil, err
		}
		u := up.Int64
		d := down.Int64
		if u < 0 {
			u = 0
		}
		if d < 0 {
			d = 0
		}
		res = append(res, TrafficTotal{
			Key:   key,
			Rx:    d,
			Tx:    u,
			Total: u + d,
		})
	}
	return res, nil
}

func (s *Store) PruneWGSamplesOlderThan(ts int64) error {
	return s.Queries.PruneWGSamplesOlderThan(context.Background(), ts)
}

func (s *Store) CompressOldSamples(olderThanTs int64) error {
	// 1. Transaction start
	tx, err := s.db.Begin()
	if err != nil {
		return err
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
		return fmt.Errorf("compress query failed: %v", err)
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
			return err
		}
		agg = append(agg, r)
	}
	rows.Close()

	if len(agg) == 0 {
		return nil // Nothing to compress
	}

	qtx := s.Queries.WithTx(tx)

	// 3. Upsert into daily_usage
	for _, a := range agg {
		err := qtx.InsertDailyUsage(context.Background(), sqlcStore.InsertDailyUsageParams{
			User:     a.u,
			Ts:       a.ts,
			Uplink:   a.up,
			Downlink: a.down,
		})
		if err != nil {
			return fmt.Errorf("compress insert failed: %v", err)
		}
	}

	// 4. Delete old samples
	err = qtx.PruneSamplesOlderThan(context.Background(), olderThanTs)
	if err != nil {
		return fmt.Errorf("compress delete failed: %v", err)
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}

func (s *Store) CompressOldWGSamples(olderThanTs int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
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
		return fmt.Errorf("compress wg query failed: %v", err)
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
			return err
		}
		agg = append(agg, r)
	}
	rows.Close()

	if len(agg) == 0 {
		return nil
	}

	qtx := s.Queries.WithTx(tx)

	for _, a := range agg {
		err := qtx.InsertWGDailyUsage(context.Background(), sqlcStore.InsertWGDailyUsageParams{
			PublicKey: a.pk,
			Ts:        a.ts,
			Rx:        a.rx,
			Tx:        a.tx,
		})
		if err != nil {
			return fmt.Errorf("compress wg insert failed: %v", err)
		}
	}

	err = qtx.PruneWGSamplesOlderThan(context.Background(), olderThanTs)
	if err != nil {
		return fmt.Errorf("compress wg delete failed: %v", err)
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
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
	rawRows, err := s.Queries.GetSamplesForUser(context.Background(), sqlcStore.GetSamplesForUserParams{
		User: user,
		Ts:   start,
		Ts_2: end,
	})
	if err == nil {
		for _, r := range rawRows {
			samples = append(samples, Sample{
				User:      r.User,
				Timestamp: r.Ts,
				Uplink:    r.Uplink,
				Downlink:  r.Downlink,
			})
		}
	}

	return samples, nil
}

func (s *Store) Vacuum() error {
	_, err := s.db.Exec("VACUUM;")
	return err
}
