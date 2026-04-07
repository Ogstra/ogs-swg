package core

import (
	"context"
	"database/sql"
	"encoding/json"

	sqlcStore "github.com/Ogstra/ogs-swg/internal/core/store"
)

func (s *Store) CreateSubscriptionWithDestinations(ctx context.Context, params sqlcStore.CreateSubscriptionParams, destinations []string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	q := s.Queries.WithTx(tx)
	id, err := q.CreateSubscription(ctx, params)
	if err != nil {
		return 0, err
	}
	if err := updateSubscriptionDestinationsWithDB(ctx, tx, id, destinations); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Store) UpdateSubscriptionWithDestinations(ctx context.Context, params sqlcStore.UpdateSubscriptionParams, destinations []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	q := s.Queries.WithTx(tx)
	if err := q.UpdateSubscription(ctx, params); err != nil {
		return err
	}
	if err := updateSubscriptionDestinationsWithDB(ctx, tx, params.ID, destinations); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) GetSubscriptionDestinations(ctx context.Context, id int64) ([]string, error) {
	var raw string
	if err := s.db.QueryRowContext(ctx, `SELECT destinations_json FROM subscriptions WHERE id = ?`, id).Scan(&raw); err != nil {
		return nil, err
	}
	return decodeSubscriptionDestinations(raw)
}

func updateSubscriptionDestinationsWithDB(ctx context.Context, db interface {
	ExecContext(context.Context, string, ...interface{}) (sql.Result, error)
}, id int64, destinations []string) error {
	payload, err := json.Marshal(destinations)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `UPDATE subscriptions SET destinations_json = ?, updated_at = strftime('%s','now') WHERE id = ?`, string(payload), id)
	return err
}

func decodeSubscriptionDestinations(raw string) ([]string, error) {
	if raw == "" {
		return []string{}, nil
	}
	var destinations []string
	if err := json.Unmarshal([]byte(raw), &destinations); err != nil {
		return nil, err
	}
	if destinations == nil {
		return []string{}, nil
	}
	return destinations, nil
}
