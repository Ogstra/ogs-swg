package core

import (
	"database/sql"
	"fmt"
	"net/netip"
	"os"
	"strings"
)

// ExternalProfile represents a manually configured VLESS or Shadowsocks profile
// for a homelab server not managed by the VPS sing-box instance.
type ExternalProfile struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Flag         string `json:"flag"`
	Type         string `json:"type"` // "vless" | "shadowsocks"
	HostIPv4     string `json:"host_ipv4"`
	HostIPv6File string `json:"host_ipv6_file"` // IPv6 literal or path on VPS, kept empty if not used
	Port         int    `json:"port"`
	UUID         string `json:"uuid"`          // VLESS only
	Password     string `json:"password"`      // Shadowsocks user password
	SSMethod     string `json:"ss_method"`     // Shadowsocks only (e.g. "2022-blake3-aes-128-gcm")
	SSServerKey  string `json:"ss_server_key"` // Shadowsocks server password/key
	PublicKey    string `json:"public_key"`    // Reality public key (homelab server key)
	ShortID      string `json:"short_id"`
	ServerName   string `json:"server_name"` // SNI
	ALPN         string `json:"alpn"`        // comma-separated ALPN protocols for share links
	Flow         string `json:"flow"`
	Enabled      bool   `json:"enabled"`
	Position     int    `json:"position"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
}

// ReadExternalIPv6 resolves an IPv6 literal or reads the current IPv6 address
// from the file at value. Returns "" if value is empty or the file cannot be read.
func ReadExternalIPv6(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if addr, err := netip.ParseAddr(trimmed); err == nil && addr.Is6() {
		return addr.String()
	}
	data, err := os.ReadFile(trimmed)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// UpsertExternalProfile inserts or updates an external profile.
// If p.ID == 0, a new row is inserted and the new ID is returned.
// If p.ID != 0, the existing row is updated and p.ID is returned.
func (s *Store) UpsertExternalProfile(p ExternalProfile) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var id int64
	previousUserName := ""
	if p.ID == 0 {
		result, err := tx.Exec(`
			INSERT INTO external_profiles
				(name, flag, type, host_ipv4, host_ipv6_file, port, uuid, password, ss_method, ss_server_key,
				 public_key, short_id, server_name, alpn, flow, enabled, position)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			p.Name, p.Flag, p.Type, p.HostIPv4, p.HostIPv6File, p.Port, p.UUID, p.Password,
			p.SSMethod, p.SSServerKey, p.PublicKey, p.ShortID, p.ServerName, p.ALPN, p.Flow,
			boolToInt(p.Enabled), p.Position,
		)
		if err != nil {
			return 0, err
		}
		id, err = result.LastInsertId()
		if err != nil {
			return 0, err
		}
	} else {
		if err := tx.QueryRow(`SELECT name FROM external_profiles WHERE id = ?`, p.ID).Scan(&previousUserName); err != nil {
			if err == sql.ErrNoRows {
				return 0, fmt.Errorf("external profile %d not found", p.ID)
			}
			return 0, err
		}
		result, err := tx.Exec(`
			UPDATE external_profiles SET
				name = ?, flag = ?, type = ?, host_ipv4 = ?, host_ipv6_file = ?, port = ?, uuid = ?,
				password = ?, ss_method = ?, ss_server_key = ?, public_key = ?, short_id = ?,
				server_name = ?, alpn = ?, flow = ?, enabled = ?, position = ?,
				updated_at = strftime('%s','now')
			WHERE id = ?`,
			p.Name, p.Flag, p.Type, p.HostIPv4, p.HostIPv6File, p.Port, p.UUID, p.Password,
			p.SSMethod, p.SSServerKey, p.PublicKey, p.ShortID, p.ServerName, p.ALPN, p.Flow,
			boolToInt(p.Enabled), p.Position, p.ID,
		)
		if err != nil {
			return 0, err
		}
		if rows, err := result.RowsAffected(); err == nil && rows == 0 {
			return 0, fmt.Errorf("external profile %d not found", p.ID)
		}
		id = p.ID
	}

	p.ID = id
	if err := ensureExternalProfileUserTx(tx, p); err != nil {
		return 0, err
	}
	if previousUserName != "" && strings.TrimSpace(previousUserName) != strings.TrimSpace(p.Name) {
		if err := transferExternalProfileSubscriptionsTx(tx, previousUserName, p.Name); err != nil {
			return 0, err
		}
		if err := deleteExternalOnlyUserTx(tx, previousUserName); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit()
}

func ensureExternalProfileUserTx(tx *sql.Tx, p ExternalProfile) error {
	userName := strings.TrimSpace(p.Name)
	if userName == "" {
		return fmt.Errorf("external profile user name is required")
	}
	if p.ID <= 0 {
		return fmt.Errorf("external profile id is required")
	}

	_, err := tx.Exec(`
		INSERT INTO users
			(email, quota_limit, quota_period, reset_day, enabled, credential, flow, vmess_security, vmess_alter_id, inbound_tags)
		VALUES (?, 0, 'monthly', 1, ?, '', '', '', 0, '[]')
		ON CONFLICT(email) DO NOTHING`,
		userName, boolToInt(p.Enabled),
	)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE users SET inbound_tags = '[]' WHERE email = ? AND COALESCE(inbound_tags, '') = ''`, userName); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM user_external_profiles WHERE external_profile_id = ?`, p.ID); err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT OR IGNORE INTO user_external_profiles (user_name, external_profile_id) VALUES (?, ?)`, userName, p.ID)
	return err
}

func transferExternalProfileSubscriptionsTx(tx *sql.Tx, oldName, newName string) error {
	oldName = strings.TrimSpace(oldName)
	newName = strings.TrimSpace(newName)
	if oldName == "" || newName == "" || oldName == newName {
		return nil
	}

	if _, err := tx.Exec(`
		UPDATE subscription_users
		SET user_name = ?
		WHERE user_name = ?
		  AND NOT EXISTS (
			SELECT 1
			FROM subscription_users existing
			WHERE existing.sub_id = subscription_users.sub_id
			  AND existing.user_name = ?
		  )`,
		newName, oldName, newName,
	); err != nil {
		return err
	}
	_, err := tx.Exec(`DELETE FROM subscription_users WHERE user_name = ?`, oldName)
	return err
}

// GetExternalProfile retrieves a single external profile by ID.
// Returns nil, nil if not found.
func (s *Store) GetExternalProfile(id int64) (*ExternalProfile, error) {
	row := s.db.QueryRow(`
		SELECT id, name, COALESCE(flag, ''), type, host_ipv4, host_ipv6_file, port, uuid, password, ss_method,
		       ss_server_key, public_key, short_id, server_name, COALESCE(alpn, ''), flow, enabled, position,
		       COALESCE(created_at, 0), COALESCE(updated_at, 0)
		FROM external_profiles WHERE id = ?`, id)
	p, err := scanExternalProfile(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return p, err
}

// ListExternalProfiles returns all external profiles ordered by position then id.
func (s *Store) ListExternalProfiles() ([]ExternalProfile, error) {
	rows, err := s.db.Query(`
		SELECT id, name, COALESCE(flag, ''), type, host_ipv4, host_ipv6_file, port, uuid, password, ss_method,
		       ss_server_key, public_key, short_id, server_name, COALESCE(alpn, ''), flow, enabled, position,
		       COALESCE(created_at, 0), COALESCE(updated_at, 0)
		FROM external_profiles ORDER BY position ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	profiles := make([]ExternalProfile, 0)
	for rows.Next() {
		p, err := scanExternalProfile(rows)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, *p)
	}
	return profiles, rows.Err()
}

// DeleteExternalProfile removes a profile and the metadata-only user it created
// when that user has no local sing-box inbound identity.
func (s *Store) DeleteExternalProfile(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	userName := ""
	if err := tx.QueryRow(`SELECT name FROM external_profiles WHERE id = ?`, id).Scan(&userName); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	if _, err := tx.Exec(`DELETE FROM user_external_profiles WHERE external_profile_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM external_profiles WHERE id = ?`, id); err != nil {
		return err
	}
	if err := deleteExternalOnlyUserTx(tx, userName); err != nil {
		return err
	}
	return tx.Commit()
}

func deleteExternalOnlyUserTx(tx *sql.Tx, userName string) error {
	userName = strings.TrimSpace(userName)
	if userName == "" {
		return nil
	}

	var remainingAssignments int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM user_external_profiles WHERE user_name = ?`, userName).Scan(&remainingAssignments); err != nil {
		return err
	}
	if remainingAssignments > 0 {
		return nil
	}

	var quotaLimit sql.NullInt64
	var quotaPeriod sql.NullString
	var resetDay sql.NullInt64
	var credential, flow, vmessSecurity, inboundTags sql.NullString
	var vmessAlterID sql.NullInt64
	err := tx.QueryRow(`
		SELECT quota_limit, quota_period, reset_day, credential, flow, vmess_security, vmess_alter_id, COALESCE(inbound_tags, '')
		FROM users WHERE email = ?`, userName).Scan(
		&quotaLimit, &quotaPeriod, &resetDay, &credential, &flow, &vmessSecurity, &vmessAlterID, &inboundTags,
	)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}

	if quotaLimit.Int64 != 0 ||
		(quotaPeriod.Valid && quotaPeriod.String != "" && quotaPeriod.String != "monthly") ||
		(resetDay.Valid && resetDay.Int64 != 0 && resetDay.Int64 != 1) ||
		strings.TrimSpace(credential.String) != "" ||
		strings.TrimSpace(flow.String) != "" ||
		strings.TrimSpace(vmessSecurity.String) != "" ||
		vmessAlterID.Int64 != 0 ||
		!isEmptyInboundTags(inboundTags.String) {
		return nil
	}

	if _, err := tx.Exec(`DELETE FROM subscription_users WHERE user_name = ?`, userName); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM user_external_profiles WHERE user_name = ?`, userName); err != nil {
		return err
	}
	_, err = tx.Exec(`DELETE FROM users WHERE email = ?`, userName)
	return err
}

func isEmptyInboundTags(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	return trimmed == "" || trimmed == "[]"
}

// SetUserExternalProfiles replaces all external profile assignments for a user atomically.
func (s *Store) SetUserExternalProfiles(userName string, profileIDs []int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM user_external_profiles WHERE user_name = ?`, userName); err != nil {
		return err
	}
	for _, pid := range profileIDs {
		if _, err := tx.Exec(`INSERT INTO user_external_profiles (user_name, external_profile_id) VALUES (?, ?)`, userName, pid); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetUserExternalProfiles returns all external profiles assigned to a user, ordered by position then id.
func (s *Store) GetUserExternalProfiles(userName string) ([]ExternalProfile, error) {
	rows, err := s.db.Query(`
		SELECT ep.id, ep.name, COALESCE(ep.flag, ''), ep.type, ep.host_ipv4, ep.host_ipv6_file, ep.port, ep.uuid, ep.password,
		       ep.ss_method, ep.ss_server_key, ep.public_key, ep.short_id, ep.server_name, COALESCE(ep.alpn, ''), ep.flow,
		       ep.enabled, ep.position, COALESCE(ep.created_at, 0), COALESCE(ep.updated_at, 0)
		FROM external_profiles ep
		JOIN user_external_profiles uep ON ep.id = uep.external_profile_id
		WHERE uep.user_name = ?
		ORDER BY ep.position ASC, ep.id ASC`, userName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	profiles := make([]ExternalProfile, 0)
	for rows.Next() {
		p, err := scanExternalProfile(rows)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, *p)
	}
	return profiles, rows.Err()
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanExternalProfile(s scanner) (*ExternalProfile, error) {
	var p ExternalProfile
	var enabled int
	err := s.Scan(
		&p.ID, &p.Name, &p.Flag, &p.Type, &p.HostIPv4, &p.HostIPv6File, &p.Port, &p.UUID, &p.Password,
		&p.SSMethod, &p.SSServerKey, &p.PublicKey, &p.ShortID, &p.ServerName, &p.ALPN,
		&p.Flow, &enabled, &p.Position, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	p.Enabled = enabled != 0
	return &p, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
