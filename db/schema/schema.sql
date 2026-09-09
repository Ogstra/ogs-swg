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
	detail_chart_target_points INTEGER NOT NULL DEFAULT 200,
	created_at INTEGER DEFAULT (strftime('%s','now')),
	updated_at INTEGER DEFAULT (strftime('%s','now'))
);

CREATE TABLE IF NOT EXISTS inbound_meta (
	tag TEXT PRIMARY KEY,
	external_port INTEGER DEFAULT 0,
	client_sni TEXT DEFAULT NULL
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
	alias TEXT NOT NULL DEFAULT '',
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

CREATE TABLE IF NOT EXISTS user_route_tags (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL UNIQUE,
	color TEXT NOT NULL DEFAULT '',
	description TEXT NOT NULL DEFAULT '',
	rule_match_json TEXT NOT NULL,
	created_at INTEGER DEFAULT (strftime('%s','now')),
	updated_at INTEGER DEFAULT (strftime('%s','now'))
);

CREATE TABLE IF NOT EXISTS external_profiles (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL,
	flag TEXT NOT NULL DEFAULT '',
	type TEXT NOT NULL DEFAULT 'vless',
	host_ipv4 TEXT NOT NULL DEFAULT '',
	host_ipv6_file TEXT NOT NULL DEFAULT '',
	port INTEGER NOT NULL DEFAULT 0,
	uuid TEXT NOT NULL DEFAULT '',
	password TEXT NOT NULL DEFAULT '',
	ss_method TEXT NOT NULL DEFAULT '',
	ss_server_key TEXT NOT NULL DEFAULT '',
	public_key TEXT NOT NULL DEFAULT '',
	short_id TEXT NOT NULL DEFAULT '',
	server_name TEXT NOT NULL DEFAULT '',
	alpn TEXT NOT NULL DEFAULT '',
	flow TEXT NOT NULL DEFAULT '',
	enabled INTEGER NOT NULL DEFAULT 1,
	position INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER DEFAULT (strftime('%s','now')),
	updated_at INTEGER DEFAULT (strftime('%s','now'))
);

CREATE TABLE IF NOT EXISTS user_external_profiles (
	user_name TEXT NOT NULL,
	external_profile_id INTEGER NOT NULL,
	PRIMARY KEY (user_name, external_profile_id),
	FOREIGN KEY (external_profile_id) REFERENCES external_profiles(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_user_external_profiles_user ON user_external_profiles(user_name);
