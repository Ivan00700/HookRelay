package repository

import (
	"context"
	"errors"
	"time"

	"hookrelay/internal/domain"

	"github.com/jackc/pgx/v5"
)

var _ DeliveryStore = (*PostgresStore)(nil)

func (s *PostgresStore) CheckQueueSchema(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, `SELECT id, status, lease_version, lease_until FROM events LIMIT 0`)
	if err != nil {
		return databaseError("check delivery queue schema", err, ErrConflict)
	}
	rows.Close()
	return databaseError("check delivery queue schema", rows.Err(), ErrConflict)
}

func (s *PostgresStore) ClaimEvent(ctx context.Context, lease time.Duration) (DeliveryJob, error) {
	if lease.Microseconds() <= 0 {
		return DeliveryJob{}, errors.New("lease must be at least one microsecond")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return DeliveryJob{}, databaseError("begin event claim", err, ErrConflict)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanupCtx)
	}()

	var id string
	err = tx.QueryRow(ctx, `
		SELECT id FROM events
		WHERE status = 'pending'
		   OR (status = 'processing' AND (lease_until IS NULL OR lease_until <= clock_timestamp()))
		ORDER BY created_at, id
		LIMIT 1 FOR UPDATE SKIP LOCKED
	`).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return DeliveryJob{}, ErrNoJob
	}
	if err != nil {
		return DeliveryJob{}, databaseError("select event to deliver", err, ErrConflict)
	}
	var job DeliveryJob
	err = tx.QueryRow(ctx, `
		UPDATE events SET status = 'processing', lease_version = lease_version + 1,
		    lease_until = clock_timestamp() + ($2::bigint * interval '1 microsecond')
		WHERE id = $1
		RETURNING id, endpoint_id, type, payload, status, created_at, lease_version, lease_until
	`, id, lease.Microseconds()).Scan(
		&job.Event.ID, &job.Event.EndpointID, &job.Event.Type, &job.Event.Payload,
		&job.Event.Status, &job.Event.CreatedAt, &job.LeaseVersion, &job.LeaseUntil,
	)
	if err != nil {
		return DeliveryJob{}, databaseError("claim event", err, ErrConflict)
	}
	if err := tx.Commit(ctx); err != nil {
		return DeliveryJob{}, databaseError("commit event claim", err, ErrConflict)
	}
	job.Event.CreatedAt = job.Event.CreatedAt.UTC()
	job.LeaseUntil = job.LeaseUntil.UTC()
	return job, nil
}

func (s *PostgresStore) FinishEvent(ctx context.Context, job DeliveryJob, status domain.DeliveryStatus) error {
	if status != domain.DeliveryStatusDelivered && status != domain.DeliveryStatusFailed {
		return ErrInvalidTransition
	}
	result, err := s.pool.Exec(ctx, `
		UPDATE events SET status = $1, lease_until = NULL
		WHERE id = $2 AND status = 'processing' AND lease_version = $3
		  AND lease_until > clock_timestamp()
	`, status, job.Event.ID, job.LeaseVersion)
	if err != nil {
		return databaseError("finish event", err, ErrConflict)
	}
	if result.RowsAffected() == 0 {
		return ErrLeaseLost
	}
	return nil
}
