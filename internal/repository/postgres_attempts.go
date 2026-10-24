package repository

import (
	"context"
	"errors"

	"hookrelay/internal/domain"

	"github.com/jackc/pgx/v5"
)

func (s *PostgresStore) ListAttempts(ctx context.Context, eventID string) ([]domain.DeliveryAttempt, error) {
	if _, err := s.GetEvent(ctx, eventID); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, event_id, attempt_number, started_at, finished_at, http_status, COALESCE(error_message, '')
		FROM delivery_attempts WHERE event_id = $1 ORDER BY attempt_number
	`, eventID)
	if err != nil {
		return nil, databaseError("list delivery attempts", err, ErrConflict)
	}
	defer rows.Close()
	attempts := make([]domain.DeliveryAttempt, 0)
	for rows.Next() {
		var attempt domain.DeliveryAttempt
		if err := rows.Scan(&attempt.ID, &attempt.EventID, &attempt.AttemptNumber, &attempt.StartedAt,
			&attempt.FinishedAt, &attempt.HTTPStatus, &attempt.ErrorMessage); err != nil {
			return nil, databaseError("scan delivery attempt", err, ErrConflict)
		}
		attempt.StartedAt = attempt.StartedAt.UTC()
		if attempt.FinishedAt != nil {
			finished := attempt.FinishedAt.UTC()
			attempt.FinishedAt = &finished
		}
		attempts = append(attempts, attempt)
	}
	if err := rows.Err(); err != nil {
		return nil, databaseError("read delivery attempts", err, ErrConflict)
	}
	return attempts, nil
}

func (s *PostgresStore) RetryEvent(ctx context.Context, id string) (domain.Event, error) {
	event, err := scanEvent(s.pool.QueryRow(ctx, `
		UPDATE events SET status = 'pending', cycle_attempts = 0,
		    next_attempt_at = clock_timestamp(), lease_until = NULL
		WHERE id = $1 AND status = 'failed'
		RETURNING id, endpoint_id, type, payload, status, created_at
	`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		if _, readErr := s.GetEvent(ctx, id); readErr != nil {
			return domain.Event{}, readErr
		}
		return domain.Event{}, ErrConflict
	}
	return event, databaseError("retry event", err, ErrConflict)
}
