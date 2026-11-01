package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	sqlcStore "github.com/Ogstra/ogs-swg/internal/core/store"
)

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
	TwoFactor   bool                 `json:"two_factor_enabled"`
}

type SubscriptionDefaults struct {
	ProfileUpdateIntervalHours *int64   `json:"profile_update_interval_hours"`
	UpdateAlways               bool     `json:"update_always"`
	Destinations               []string `json:"destinations"`
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
			COALESCE(created_at, 0),
			COALESCE(totp_enabled, 0)
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
			totpEnabled         int64
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
			&totpEnabled,
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
			TwoFactor:   totpEnabled != 0,
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
	if err := s.Queries.UpdatePanelUserPassword(context.Background(), sqlcStore.UpdatePanelUserPasswordParams{
		PasswordHash: string(hash),
		Username:     username,
	}); err != nil {
		return err
	}
	// Sessions issued before the change stop working.
	_, err = s.db.Exec("UPDATE panel_users SET tokens_valid_after = ? WHERE username = ?", time.Now().Unix(), username)
	return err
}

// GetPanelUserAuthState returns a panel user's current permissions and the
// unix time before which issued tokens are revoked, and whether two-factor is on.
// exists is false when the user is gone.
func (s *Store) GetPanelUserAuthState(username string) (perms *PanelUserPermissions, validAfter int64, twoFactor bool, exists bool, err error) {
	var p PanelUserPermissions
	var flags [12]int64
	var totpEnabled int64
	err = s.db.QueryRowContext(context.Background(), `
		SELECT
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
			COALESCE(tokens_valid_after, 0),
			COALESCE(totp_enabled, 0)
		FROM panel_users WHERE username = ?`, username).Scan(
		&flags[0], &flags[1], &flags[2], &flags[3], &flags[4], &flags[5],
		&flags[6], &flags[7], &flags[8], &flags[9], &flags[10], &flags[11], &validAfter, &totpEnabled)
	if err == sql.ErrNoRows {
		return nil, 0, false, false, nil
	}
	if err != nil {
		return nil, 0, false, false, err
	}
	p.CanReadUsers, p.CanWriteUsers = flags[0] != 0, flags[1] != 0
	p.CanReadWireguard, p.CanWriteWireguard = flags[2] != 0, flags[3] != 0
	p.CanReadConfig, p.CanWriteConfig = flags[4] != 0, flags[5] != 0
	p.CanReadSettings, p.CanWriteSettings = flags[6] != 0, flags[7] != 0
	p.CanReadPanelUsers, p.CanWritePanelUsers = flags[8] != 0, flags[9] != 0
	p.CanReadLogs, p.CanReadLogsCensored = flags[10] != 0, flags[11] != 0
	return &p, validAfter, totpEnabled != 0, true, nil
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

// PanelUserTOTP is a panel user's two-factor state.
type PanelUserTOTP struct {
	Enabled bool
	Secret  string
}

func (s *Store) GetPanelUserTOTP(username string) (PanelUserTOTP, error) {
	var out PanelUserTOTP
	var enabled int64
	err := s.db.QueryRowContext(context.Background(),
		`SELECT COALESCE(totp_secret, ''), COALESCE(totp_enabled, 0) FROM panel_users WHERE username = ?`, username).
		Scan(&out.Secret, &enabled)
	if err == sql.ErrNoRows {
		return PanelUserTOTP{}, nil
	}
	out.Enabled = enabled != 0
	return out, err
}

// SetPanelUserTOTPPending stores a new secret that is not enforced until
// EnablePanelUserTOTP confirms the user can produce a code from it.
func (s *Store) SetPanelUserTOTPPending(username, secret string) error {
	_, err := s.db.ExecContext(context.Background(),
		`UPDATE panel_users SET totp_secret = ?, totp_enabled = 0, totp_last_step = 0, totp_recovery_json = '[]' WHERE username = ?`,
		secret, username)
	return err
}

// EnablePanelUserTOTP turns two-factor on and records the hashed recovery codes.
func (s *Store) EnablePanelUserTOTP(username string, recoveryHashes []string, usedStep int64) error {
	raw, err := json.Marshal(recoveryHashes)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(context.Background(),
		`UPDATE panel_users SET totp_enabled = 1, totp_last_step = ?, totp_recovery_json = ? WHERE username = ?`,
		usedStep, string(raw), username)
	return err
}

func (s *Store) DisablePanelUserTOTP(username string) error {
	_, err := s.db.ExecContext(context.Background(),
		`UPDATE panel_users SET totp_enabled = 0, totp_secret = '', totp_last_step = 0, totp_recovery_json = '[]' WHERE username = ?`,
		username)
	return err
}

// ConsumePanelUserTOTPStep records step as used. It reports false when that
// step (or a later one) was already accepted, which blocks replaying a code.
func (s *Store) ConsumePanelUserTOTPStep(username string, step int64) (bool, error) {
	res, err := s.db.ExecContext(context.Background(),
		`UPDATE panel_users SET totp_last_step = ? WHERE username = ? AND totp_last_step < ?`, step, username, step)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// ConsumePanelUserRecoveryCode removes codeHash from the user's recovery codes
// and reports whether it was present.
func (s *Store) ConsumePanelUserRecoveryCode(username, codeHash string) (bool, error) {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var raw string
	if err := tx.QueryRow(`SELECT COALESCE(totp_recovery_json, '[]') FROM panel_users WHERE username = ?`, username).Scan(&raw); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	var hashes []string
	if err := json.Unmarshal([]byte(raw), &hashes); err != nil {
		return false, nil
	}
	idx := -1
	for i, h := range hashes {
		if h == codeHash {
			idx = i
			break
		}
	}
	if idx < 0 {
		return false, nil
	}
	hashes = append(hashes[:idx], hashes[idx+1:]...)
	updated, err := json.Marshal(hashes)
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(`UPDATE panel_users SET totp_recovery_json = ? WHERE username = ?`, string(updated), username); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// ReplacePanelUserRecoveryCodes swaps the stored recovery code hashes for a new set.
func (s *Store) ReplacePanelUserRecoveryCodes(username string, hashes []string) error {
	raw, err := json.Marshal(hashes)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(context.Background(),
		`UPDATE panel_users SET totp_recovery_json = ? WHERE username = ?`, string(raw), username)
	return err
}

// RevokePanelUserSessions invalidates every token issued to username so far.
func (s *Store) RevokePanelUserSessions(username string) error {
	_, err := s.db.ExecContext(context.Background(),
		`UPDATE panel_users SET tokens_valid_after = ? WHERE username = ?`, time.Now().Unix()+1, username)
	return err
}
