package core

import (
	"database/sql"
	"os"
	"strings"
)

// ExternalProfile represents a manually configured VLESS or Shadowsocks profile
// for a homelab server not managed by the VPS sing-box instance.
type ExternalProfile struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Type         string `json:"type"`           // "vless" | "shadowsocks"
	HostIPv4     string `json:"host_ipv4"`
	HostIPv6File string `json:"host_ipv6_file"` // path on VPS, kept empty if not used
	Port         int    `json:"port"`
	UUID         string `json:"uuid"`           // VLESS only
	Password     string `json:"password"`       // Shadowsocks only
	SSMethod     string `json:"ss_method"`      // Shadowsocks only (e.g. "2022-blake3-aes-128-gcm")
	SSServerKey  string `json:"ss_server_key"`  // Shadowsocks 2022 only
	PublicKey    string `json:"public_key"`     // Reality public key (homelab server key)
	ShortID      string `json:"short_id"`
	ServerName   string `json:"server_name"`    // SNI
	Flow         string `json:"flow"`
	Enabled      bool   `json:"enabled"`
	Position     int    `json:"position"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
}

// ReadExternalIPv6 reads the current IPv6 address from the file at path.
// Returns "" if path is empty or the file cannot be read.
func ReadExternalIPv6(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// UpsertExternalProfile inserts or updates an external profile.
// If p.ID == 0, a new row is inserted and the new ID is returned.
// If p.ID != 0, the existing row is updated and p.ID is returned.
func (s *Store) UpsertExternalProfile(p ExternalProfile) (int64, error) {
	if p.ID == 0 {
		result, err := s.db.Exec(`
			INSERT INTO external_profiles
				(name, type, host_ipv4, host_ipv6_file, port, uuid, password, ss_method, ss_server_key,
				 public_key, short_id, server_name, flow, enabled, position)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			p.Name, p.Type, p.HostIPv4, p.HostIPv6File, p.Port, p.UUID, p.Password,
			p.SSMethod, p.SSServerKey, p.PublicKey, p.ShortID, p.ServerName, p.Flow,
			boolToInt(p.Enabled), p.Position,
		)
		if err != nil {
			return 0, err
		}
		return result.LastInsertId()
	}
	_, err := s.db.Exec(`
		UPDATE external_profiles SET
			name = ?, type = ?, host_ipv4 = ?, host_ipv6_file = ?, port = ?, uuid = ?,
			password = ?, ss_method = ?, ss_server_key = ?, public_key = ?, short_id = ?,
			server_name = ?, flow = ?, enabled = ?, position = ?,
			updated_at = strftime('%s','now')
		WHERE id = ?`,
		p.Name, p.Type, p.HostIPv4, p.HostIPv6File, p.Port, p.UUID, p.Password,
		p.SSMethod, p.SSServerKey, p.PublicKey, p.ShortID, p.ServerName, p.Flow,
		boolToInt(p.Enabled), p.Position, p.ID,
	)
	return p.ID, err
}

// GetExternalProfile retrieves a single external profile by ID.
// Returns nil, nil if not found.
func (s *Store) GetExternalProfile(id int64) (*ExternalProfile, error) {
	row := s.db.QueryRow(`
		SELECT id, name, type, host_ipv4, host_ipv6_file, port, uuid, password, ss_method,
		       ss_server_key, public_key, short_id, server_name, flow, enabled, position,
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
		SELECT id, name, type, host_ipv4, host_ipv6_file, port, uuid, password, ss_method,
		       ss_server_key, public_key, short_id, server_name, flow, enabled, position,
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

// DeleteExternalProfile removes a profile by ID. Cascade handles user_external_profiles.
func (s *Store) DeleteExternalProfile(id int64) error {
	_, err := s.db.Exec(`DELETE FROM external_profiles WHERE id = ?`, id)
	return err
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
		SELECT ep.id, ep.name, ep.type, ep.host_ipv4, ep.host_ipv6_file, ep.port, ep.uuid, ep.password,
		       ep.ss_method, ep.ss_server_key, ep.public_key, ep.short_id, ep.server_name, ep.flow,
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
		&p.ID, &p.Name, &p.Type, &p.HostIPv4, &p.HostIPv6File, &p.Port, &p.UUID, &p.Password,
		&p.SSMethod, &p.SSServerKey, &p.PublicKey, &p.ShortID, &p.ServerName, &p.Flow,
		&enabled, &p.Position, &p.CreatedAt, &p.UpdatedAt,
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
