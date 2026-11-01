package core

import (
	"context"
	"database/sql"
	"errors"
)

// UserTemplate is a preset for creating Sing-box users: which inbound they join,
// their flow, and their quota.
type UserTemplate struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	InboundTag  string `json:"inbound_tag"`
	Flow        string `json:"flow"`
	QuotaLimit  int64  `json:"quota_limit"`
	QuotaPeriod string `json:"quota_period"`
	CreatedAt   int64  `json:"created_at"`
}

var ErrUserTemplateExists = errors.New("a template with that name already exists")
var ErrUserTemplateNotFound = errors.New("template not found")

func (s *Store) ListUserTemplates(ctx context.Context) ([]UserTemplate, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, inbound_tag, flow, quota_limit, quota_period, created_at
		FROM user_templates ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UserTemplate{}
	for rows.Next() {
		var t UserTemplate
		if err := rows.Scan(&t.ID, &t.Name, &t.InboundTag, &t.Flow, &t.QuotaLimit, &t.QuotaPeriod, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) nameTaken(ctx context.Context, name string, exceptID int64) (bool, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_templates WHERE name = ? COLLATE NOCASE AND id != ?`, name, exceptID).Scan(&n)
	return n > 0, err
}

func (s *Store) CreateUserTemplate(ctx context.Context, t UserTemplate) (int64, error) {
	if taken, err := s.nameTaken(ctx, t.Name, 0); err != nil {
		return 0, err
	} else if taken {
		return 0, ErrUserTemplateExists
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO user_templates (name, inbound_tag, flow, quota_limit, quota_period)
		VALUES (?, ?, ?, ?, ?)`, t.Name, t.InboundTag, t.Flow, t.QuotaLimit, t.QuotaPeriod)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateUserTemplate(ctx context.Context, t UserTemplate) error {
	if taken, err := s.nameTaken(ctx, t.Name, t.ID); err != nil {
		return err
	} else if taken {
		return ErrUserTemplateExists
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE user_templates SET name = ?, inbound_tag = ?, flow = ?, quota_limit = ?, quota_period = ?
		WHERE id = ?`, t.Name, t.InboundTag, t.Flow, t.QuotaLimit, t.QuotaPeriod, t.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrUserTemplateNotFound
	}
	return nil
}

func (s *Store) DeleteUserTemplate(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM user_templates WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
