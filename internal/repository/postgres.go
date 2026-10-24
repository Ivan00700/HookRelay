package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"hookrelay/internal/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct {
	pool *pgxpool.Pool
}

var _ Store = (*PostgresStore)(nil)

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) CreateEndpoint(ctx context.Context, endpoint domain.Endpoint) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO endpoints (id, name, url, secret, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, endpoint.ID, endpoint.Name, endpoint.URL, endpoint.Secret, endpoint.CreatedAt)
	return databaseError("create endpoint", err, ErrConflict)
}

func (s *PostgresStore) GetEndpoint(ctx context.Context, id string) (domain.Endpoint, error) {
	endpoint, err := scanEndpoint(s.pool.QueryRow(ctx, `
		SELECT id, name, url, secret, created_at FROM endpoints WHERE id = $1
	`, id))
	return endpoint, databaseError("get endpoint", err, ErrConflict)
}

func (s *PostgresStore) ListEndpoints(ctx context.Context) ([]domain.Endpoint, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, url, secret, created_at FROM endpoints ORDER BY id`)
	if err != nil {
		return nil, databaseError("list endpoints", err, ErrConflict)
	}
	defer rows.Close()
	endpoints := make([]domain.Endpoint, 0)
	for rows.Next() {
		endpoint, err := scanEndpoint(rows)
		if err != nil {
			return nil, databaseError("scan endpoint", err, ErrConflict)
		}
		endpoints = append(endpoints, endpoint)
	}
	if err := rows.Err(); err != nil {
		return nil, databaseError("read endpoints", err, ErrConflict)
	}
	return endpoints, nil
}

func (s *PostgresStore) DeleteEndpoint(ctx context.Context, id string) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM endpoints WHERE id = $1`, id)
	if err != nil {
		return databaseError("delete endpoint", err, ErrConflict)
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) CreateEvent(ctx context.Context, event domain.Event) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO events (id, endpoint_id, type, payload, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, event.ID, event.EndpointID, event.Type, event.Payload, event.Status, event.CreatedAt)
	return databaseError("create event", err, ErrNotFound)
}

func (s *PostgresStore) GetEvent(ctx context.Context, id string) (domain.Event, error) {
	event, err := scanEvent(s.pool.QueryRow(ctx, `
		SELECT id, endpoint_id, type, payload, status, created_at FROM events WHERE id = $1
	`, id))
	return event, databaseError("get event", err, ErrConflict)
}

func (s *PostgresStore) ListEvents(ctx context.Context) ([]domain.Event, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, endpoint_id, type, payload, status, created_at FROM events ORDER BY id`)
	if err != nil {
		return nil, databaseError("list events", err, ErrConflict)
	}
	defer rows.Close()
	events := make([]domain.Event, 0)
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return nil, databaseError("scan event", err, ErrConflict)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, databaseError("read events", err, ErrConflict)
	}
	return events, nil
}

func (s *PostgresStore) DeleteEvent(ctx context.Context, id string) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM events WHERE id = $1`, id)
	if err != nil {
		return databaseError("delete event", err, ErrConflict)
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) UpdateEventStatus(ctx context.Context, id string, status domain.DeliveryStatus) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return databaseError("begin status update", err, ErrConflict)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanupCtx)
	}()

	var current domain.DeliveryStatus
	var leaseUntil *time.Time
	err = tx.QueryRow(ctx, `SELECT status, lease_until FROM events WHERE id = $1 FOR UPDATE`, id).Scan(&current, &leaseUntil)
	if err != nil {
		return databaseError("read event status", err, ErrConflict)
	}
	if leaseUntil != nil {
		return ErrLeaseLost
	}
	if status == domain.DeliveryStatusPending {
		return ErrInvalidTransition
	}
	if !domain.CanTransition(current, status) {
		return ErrInvalidTransition
	}
	if _, err := tx.Exec(ctx, `UPDATE events SET status = $1 WHERE id = $2`, status, id); err != nil {
		return databaseError("update event status", err, ErrConflict)
	}
	return databaseError("commit event status", tx.Commit(ctx), ErrConflict)
}

func scanEndpoint(row pgx.Row) (domain.Endpoint, error) {
	var endpoint domain.Endpoint
	if err := row.Scan(&endpoint.ID, &endpoint.Name, &endpoint.URL, &endpoint.Secret, &endpoint.CreatedAt); err != nil {
		return domain.Endpoint{}, err
	}
	endpoint.CreatedAt = endpoint.CreatedAt.UTC()
	return endpoint, nil
}

func scanEvent(row pgx.Row) (domain.Event, error) {
	var event domain.Event
	if err := row.Scan(&event.ID, &event.EndpointID, &event.Type, &event.Payload, &event.Status, &event.CreatedAt); err != nil {
		return domain.Event{}, err
	}
	event.CreatedAt = event.CreatedAt.UTC()
	return event, nil
}

func databaseError(operation string, err, foreignKeyError error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	} else {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case "23505":
				err = ErrConflict
			case "23503":
				err = foreignKeyError
			}
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}
