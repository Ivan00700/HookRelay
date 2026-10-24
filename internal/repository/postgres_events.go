package repository

import (
	"context"
	"time"

	"hookrelay/internal/domain"
)

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
	// Проверка и смена статуса должны выполняться под одной блокировкой строки.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return databaseError("begin status update", err, ErrConflict)
	}
	defer rollback(tx)

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
