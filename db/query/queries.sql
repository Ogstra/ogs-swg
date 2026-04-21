-- Admin Queries --
-- name: CreateAdmin :exec
INSERT INTO admins (username, password_hash) VALUES (?, ?);

-- name: GetAdmin :one
SELECT password_hash FROM admins WHERE username = ?;

-- name: UpdateAdminPassword :exec
UPDATE admins SET password_hash = ? WHERE username = ?;

-- name: CheckAdminExists :one
SELECT COUNT(*) FROM admins WHERE username = ?;

-- name: UpdateAdminUsername :exec
UPDATE admins SET username = ? WHERE username = ?;

-- name: CountAdmins :one
SELECT COUNT(*) FROM admins;

-- Panel Users Queries --
-- name: CreatePanelUser :exec
INSERT INTO panel_users (
	username,
	password_hash,
	can_read_users,
	can_write_users,
	can_read_wireguard,
	can_write_wireguard,
	can_read_config,
	can_write_config,
	can_read_settings,
	can_write_settings,
	can_read_panel_users,
	can_write_panel_users,
	can_read_logs
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetPanelUser :one
SELECT
	username,
	password_hash,
	can_read_users,
	can_write_users,
	can_read_wireguard,
	can_write_wireguard,
	can_read_config,
	can_write_config,
	can_read_settings,
	can_write_settings,
	can_read_panel_users,
	can_write_panel_users,
	can_read_logs,
	created_at,
	updated_at
FROM panel_users
WHERE username = ?;

-- name: GetAllPanelUsers :many
SELECT
	username,
	can_read_users,
	can_write_users,
	can_read_wireguard,
	can_write_wireguard,
	can_read_config,
	can_write_config,
	can_read_settings,
	can_write_settings,
	can_read_panel_users,
	can_write_panel_users,
	can_read_logs,
	created_at
FROM panel_users
ORDER BY username ASC;

-- name: UpdatePanelUserPassword :exec
UPDATE panel_users SET password_hash = ?, updated_at = strftime('%s','now') WHERE username = ?;

-- name: UpdatePanelUserPermissions :exec
UPDATE panel_users
SET
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
	updated_at = strftime('%s','now')
WHERE username = ?;

-- name: UpdatePanelUsername :exec
UPDATE panel_users SET username = ?, updated_at = strftime('%s','now') WHERE username = ?;

-- name: DeletePanelUser :exec
DELETE FROM panel_users WHERE username = ?;

-- name: CountPanelUsers :one
SELECT COUNT(*) FROM panel_users;

-- name: CheckPanelUserExists :one
SELECT COUNT(*) FROM panel_users WHERE username = ?;

-- name: GetPanelUserSubscriptionDefaults :one
SELECT
	subscription_default_profile_update_interval_hours,
	subscription_default_update_always,
	subscription_default_destinations_json
FROM panel_users
WHERE username = ?;

-- name: UpdatePanelUserSubscriptionDefaults :exec
UPDATE panel_users
SET
	subscription_default_profile_update_interval_hours = ?,
	subscription_default_update_always = ?,
	subscription_default_destinations_json = ?,
	updated_at = strftime('%s','now')
WHERE username = ?;

-- InboundMeta Queries --
-- name: UpsertInboundMeta :exec
INSERT INTO inbound_meta (tag, external_port) VALUES (?, ?) ON CONFLICT(tag) DO UPDATE SET external_port = excluded.external_port;

-- name: GetInboundMeta :one
SELECT tag, external_port FROM inbound_meta WHERE tag = ?;

-- name: GetAllInboundMeta :many
SELECT tag, external_port FROM inbound_meta;

-- name: DeleteInboundMeta :exec
DELETE FROM inbound_meta WHERE tag = ?;

-- name: RenameInboundMeta :exec
UPDATE inbound_meta SET tag = ? WHERE tag = ?;

-- Samples Queries --
-- name: CountSamples :one
SELECT COUNT(*) FROM samples;

-- name: TruncateSamples :exec
DELETE FROM samples;

-- name: GetMaxTimestamp :one
SELECT MAX(ts) FROM samples;

-- name: GetMaxTimestampForUser :one
SELECT MAX(ts) FROM samples WHERE user = ?;

-- name: PruneSamplesOlderThan :exec
DELETE FROM samples WHERE ts < ?;

-- name: InsertSample :exec
INSERT OR IGNORE INTO samples (user, ts, uplink, downlink) VALUES (?, ?, ?, ?);

-- name: GetSamplesForUser :many
SELECT user, ts, uplink, downlink 
FROM samples 
WHERE user = ? AND ts >= ? AND ts <= ? 
ORDER BY ts ASC;

-- name: GetGlobalTraffic :many
SELECT ts, SUM(uplink) as up, SUM(downlink) as down
FROM samples
WHERE ts >= ? AND ts <= ?
GROUP BY ts
ORDER BY ts ASC;

-- name: GetActiveUserCount :one
SELECT COUNT(DISTINCT user)
FROM samples
WHERE ts >= ? AND (uplink > 0 OR downlink > 0);

-- name: GetActiveUsersWithTraffic :many
SELECT DISTINCT user 
FROM samples 
WHERE ts >= ? AND (uplink > 0 OR downlink > 0);

-- name: GetLastSeenUserWithTraffic :one
SELECT MAX(ts) FROM samples WHERE user = ? AND (uplink > 0 OR downlink > 0);

-- name: GetLastSeenUserAndThreshold :one
SELECT MAX(ts) FROM samples WHERE user = ? AND (uplink + downlink) >= ?;

-- name: GetLastSeenMap :many
SELECT user, MAX(ts) as max_ts FROM samples GROUP BY user;

-- name: GetTrafficPerUser :many
SELECT user, SUM(uplink) as up, SUM(downlink) as down
FROM samples
WHERE ts >= ? AND ts <= ?
GROUP BY user;

-- name: GetActiveUsersWithThreshold :many
SELECT user, SUM(uplink + downlink) as total 
FROM samples 
WHERE ts >= ? 
GROUP BY user 
HAVING SUM(uplink + downlink) >= ?;

-- name: GetActiveUserCountWithThreshold :one
SELECT COUNT(*) FROM (
	SELECT user, SUM(uplink + downlink) as total
	FROM samples
	WHERE ts >= ?
	GROUP BY user
	HAVING SUM(uplink + downlink) >= ?
);

-- Users / Metadata --
-- name: UpsertUser :exec
INSERT INTO users (email, quota_limit, quota_period, reset_day, enabled, credential, flow, vmess_security, vmess_alter_id) 
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(email) DO UPDATE SET
	quota_limit = excluded.quota_limit,
	quota_period = excluded.quota_period,
	reset_day = excluded.reset_day,
	enabled = excluded.enabled,
	credential = excluded.credential,
	flow = excluded.flow,
	vmess_security = excluded.vmess_security,
	vmess_alter_id = excluded.vmess_alter_id;

-- name: GetUser :one
SELECT email, quota_limit, quota_period, reset_day, enabled, credential, flow, vmess_security, vmess_alter_id FROM users WHERE email = ?;

-- name: GetAllUsers :many
SELECT email, quota_limit, quota_period, reset_day, enabled, credential, flow, vmess_security, vmess_alter_id FROM users;

-- name: DeleteUser :exec
DELETE FROM users WHERE email = ?;

-- Sampler Runs --
-- name: InsertSamplerRun :exec
INSERT INTO sampler_runs (ts, duration_ms, inserted, error, source) VALUES (?, ?, ?, ?, ?);

-- name: GetSamplerRuns :many
SELECT ts, duration_ms, inserted, COALESCE(error,'') as error, COALESCE(source, 'sing-box') as source 
FROM sampler_runs 
ORDER BY ts DESC 
LIMIT ?;

-- WireGuard Samples --
-- name: CountWGSamples :one
SELECT COUNT(*) FROM wg_samples;

-- name: InsertWGSample :exec
INSERT INTO wg_samples (public_key, ts, rx, tx, endpoint) VALUES (?, ?, ?, ?, ?);

-- name: GetWGBoundarySamples :many
SELECT rx, tx FROM wg_samples WHERE public_key = ? AND ts >= ? AND ts <= ? ORDER BY ts ASC LIMIT 1;
-- Note: the descending query will need its own query name
-- name: GetWGLastBoundarySample :many
SELECT rx, tx FROM wg_samples WHERE public_key = ? AND ts >= ? AND ts <= ? ORDER BY ts DESC LIMIT 1;

-- name: GetWGTrafficSeries :many
SELECT public_key, ts, rx, tx, COALESCE(endpoint, '') as endpoint
FROM wg_samples 
WHERE public_key = ? AND ts >= ? AND ts <= ? 
ORDER BY ts ASC 
LIMIT ?;

-- name: PruneWGSamplesOlderThan :exec
DELETE FROM wg_samples WHERE ts < ?;

-- WG Peers --
-- name: UpsertWGPeer :exec
INSERT INTO wg_peers (public_key, alias, deleted, updated_at)
VALUES (?, ?, ?, strftime('%s','now'))
ON CONFLICT(public_key) DO UPDATE SET
	alias = excluded.alias,
	deleted = excluded.deleted,
	updated_at = strftime('%s','now');

-- name: UpdateWGPeerHandshake :exec
UPDATE wg_peers
SET last_handshake = ?, updated_at = strftime('%s','now')
WHERE public_key = ?;

-- name: GetAllWGPeers :many
SELECT public_key, alias, last_handshake, deleted FROM wg_peers;

-- Daily Usage --
-- name: InsertDailyUsage :exec
INSERT INTO daily_usage (user, ts, uplink, downlink)
VALUES (?, ?, ?, ?)
ON CONFLICT(user, ts) DO UPDATE SET
uplink = uplink + excluded.uplink,
downlink = downlink + excluded.downlink;

-- name: InsertWGDailyUsage :exec
INSERT INTO daily_wg_usage (public_key, ts, rx, tx)
VALUES (?, ?, ?, ?)
ON CONFLICT(public_key, ts) DO UPDATE SET
rx = rx + excluded.rx,
tx = tx + excluded.tx;

-- name: CountDailyUsage :one
SELECT COUNT(*) FROM daily_usage;

-- name: CountWGDailyUsage :one
SELECT COUNT(*) FROM daily_wg_usage;

-- name: GetDailyUsageInDateRange :many
SELECT user, ts, uplink, downlink
FROM daily_usage
WHERE user = ? AND ts >= ? AND ts <= ?;

-- Subscriptions Queries --
-- name: CreateSubscription :one
INSERT INTO subscriptions (
	token,
	name,
	quota_limit,
	quota_period,
	reset_day,
	profile_update_interval_hours,
	update_always
) VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id;

-- name: GetSubscriptionByToken :one
SELECT
	s.id,
	s.token,
	s.name,
	s.quota_limit,
	s.quota_period,
	s.reset_day,
	s.profile_update_interval_hours,
	s.update_always,
	(SELECT MAX(sr.requested_at) FROM subscription_requests sr WHERE sr.sub_id = s.id) AS last_request_at,
	s.created_at,
	s.updated_at
FROM subscriptions s
WHERE s.token = ?;

-- name: GetSubscriptionByID :one
SELECT
	s.id,
	s.token,
	s.name,
	s.quota_limit,
	s.quota_period,
	s.reset_day,
	s.profile_update_interval_hours,
	s.update_always,
	(SELECT MAX(sr.requested_at) FROM subscription_requests sr WHERE sr.sub_id = s.id) AS last_request_at,
	s.created_at,
	s.updated_at
FROM subscriptions s
WHERE s.id = ?;

-- name: GetAllSubscriptions :many
SELECT
	s.id,
	s.token,
	s.name,
	s.quota_limit,
	s.quota_period,
	s.reset_day,
	s.profile_update_interval_hours,
	s.update_always,
	(SELECT MAX(sr.requested_at) FROM subscription_requests sr WHERE sr.sub_id = s.id) AS last_request_at,
	s.created_at,
	s.updated_at
FROM subscriptions s
ORDER BY s.created_at DESC;

-- name: UpdateSubscription :exec
UPDATE subscriptions
SET
	name = ?,
	quota_limit = ?,
	quota_period = ?,
	reset_day = ?,
	profile_update_interval_hours = ?,
	update_always = ?,
	updated_at = strftime('%s','now')
WHERE id = ?;

-- name: RegenerateSubscriptionToken :exec
UPDATE subscriptions SET token = ?, updated_at = strftime('%s','now') WHERE id = ?;

-- name: DeleteSubscription :exec
DELETE FROM subscriptions WHERE id = ?;

-- name: GetSubscriptionUsageInRange :one
SELECT CAST(COALESCE(SUM(s.uplink + s.downlink), 0) AS INTEGER) as total
FROM samples s
INNER JOIN subscription_users su ON su.user_name = s.user
WHERE su.sub_id = ? AND s.ts >= ? AND s.ts < ?;

-- name: GetSubscriptionsForUser :many
SELECT
	s.id,
	s.token,
	s.name,
	s.quota_limit,
	s.quota_period,
	s.reset_day,
	s.profile_update_interval_hours,
	s.update_always,
	(SELECT MAX(sr.requested_at) FROM subscription_requests sr WHERE sr.sub_id = s.id) AS last_request_at,
	s.created_at,
	s.updated_at
FROM subscriptions s
INNER JOIN subscription_users su ON su.sub_id = s.id
WHERE su.user_name = ?;

-- Subscription Users Queries --
-- name: AddUserToSubscription :exec
INSERT INTO subscription_users (sub_id, user_name, alias, position) VALUES (?, ?, ?, ?)
ON CONFLICT(sub_id, user_name) DO UPDATE SET
	alias = excluded.alias,
	position = excluded.position;

-- name: RemoveUserFromSubscription :exec
DELETE FROM subscription_users WHERE sub_id = ? AND user_name = ?;

-- name: RemoveUserFromAllSubscriptions :exec
DELETE FROM subscription_users WHERE user_name = ?;

-- name: ClearSubscriptionUsers :exec
DELETE FROM subscription_users WHERE sub_id = ?;

-- name: GetUsersForSubscription :many
SELECT user_name FROM subscription_users WHERE sub_id = ? ORDER BY position ASC, user_name ASC;

-- name: GetSubscriptionMembers :many
SELECT sub_id, user_name, alias, position
FROM subscription_users
WHERE sub_id = ?
ORDER BY position ASC, user_name ASC;

-- Subscription Requests Queries --
-- name: InsertSubscriptionRequest :exec
INSERT INTO subscription_requests (
	sub_id,
	user_name,
	request_ip,
	request_host,
	request_path,
	user_agent,
	device_model,
	device_os,
	device_os_version,
	app_version,
	country,
	hwid_hash,
	hwid_prefix,
	requested_at,
	served_from_cache,
	blocked,
	block_reason
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetSubscriptionRequestHistory :many
SELECT
	sr.id,
	sr.sub_id,
	COALESCE(s.name, '') AS name,
	sr.user_name,
	sr.request_ip,
	sr.request_host,
	sr.request_path,
	sr.user_agent,
	sr.device_model,
	sr.device_os,
	sr.device_os_version,
	sr.app_version,
	sr.country,
	sr.hwid_hash,
	sr.hwid_prefix,
	sr.requested_at,
	sr.served_from_cache,
	sr.blocked,
	sr.block_reason
FROM subscription_requests sr
LEFT JOIN subscriptions s ON s.id = sr.sub_id
WHERE (? = 0 OR sr.sub_id = ?)
ORDER BY sr.requested_at DESC, sr.id DESC
LIMIT ? OFFSET ?;

-- name: GetBlockedSubscriptionRequests :many
SELECT
	sr.id,
	sr.sub_id,
	COALESCE(s.name, '') AS sub_name,
	sr.request_ip,
	sr.requested_at,
	sr.block_reason,
	sr.user_agent
FROM subscription_requests sr
LEFT JOIN subscriptions s ON s.id = sr.sub_id
WHERE sr.blocked = 1
ORDER BY sr.requested_at DESC, sr.id DESC
LIMIT ? OFFSET ?;

-- name: CountSubscriptionRequests :one
SELECT COUNT(*) FROM subscription_requests;

-- name: PruneSubscriptionRequestsOlderThan :exec
DELETE FROM subscription_requests WHERE requested_at < ?;

-- Subscription Protection Rules Queries --
-- name: InsertProtectionRule :exec
INSERT INTO subscription_protection_rules (
	rule_type,
	value,
	note,
	created_at
) VALUES (?, ?, ?, ?);

-- name: GetAllProtectionRules :many
SELECT id, rule_type, value, note, created_at
FROM subscription_protection_rules
ORDER BY created_at DESC, id DESC;

-- name: DeleteProtectionRule :exec
DELETE FROM subscription_protection_rules WHERE id = ?;
