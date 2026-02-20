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
