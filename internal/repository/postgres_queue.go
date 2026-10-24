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
	rows, err := s.pool.Query(ctx, `SELECT id, status, lease_version, lease_until, attempt_count, cycle_attempts, next_attempt_at FROM events LIMIT 0`)
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
	var cycleAttempts int
	err = tx.QueryRow(ctx, `
		SELECT id, cycle_attempts FROM events
		WHERE (status = 'pending' AND next_attempt_at <= clock_timestamp())
		   OR (status = 'processing' AND (lease_until IS NULL OR lease_until <= clock_timestamp()))
		ORDER BY created_at, id
		LIMIT 1 FOR UPDATE SKIP LOCKED
	`).Scan(&id, &cycleAttempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return DeliveryJob{}, ErrNoJob
	}
	if err != nil {
		return DeliveryJob{}, databaseError("select event to deliver", err, ErrConflict)
	}
	_, err = tx.Exec(ctx, `
		UPDATE delivery_attempts
		SET finished_at = GREATEST(clock_timestamp(), started_at), error_message = 'delivery interrupted: lease expired'
		WHERE event_id = $1 AND finished_at IS NULL
	`, id)
	if err != nil {
		return DeliveryJob{}, databaseError("close interrupted attempt", err, ErrConflict)
	}
	if cycleAttempts >= domain.MaxDeliveryAttempts {
		if _, err := tx.Exec(ctx, `UPDATE events SET status = 'failed', lease_until = NULL WHERE id = $1`, id); err != nil {
			return DeliveryJob{}, databaseError("stop exhausted delivery", err, ErrConflict)
		}
		if err := tx.Commit(ctx); err != nil {
			return DeliveryJob{}, databaseError("commit exhausted delivery", err, ErrConflict)
		}
		return DeliveryJob{}, ErrNoJob
	}
	var job DeliveryJob
	err = tx.QueryRow(ctx, `
		UPDATE events SET status = 'processing', lease_version = lease_version + 1,
		    lease_until = clock_timestamp() + ($2::bigint * interval '1 microsecond'),
		    attempt_count = attempt_count + 1, cycle_attempts = cycle_attempts + 1
		WHERE id = $1
		RETURNING id, endpoint_id, type, payload, status, created_at, lease_version, lease_until, attempt_count, cycle_attempts
	`, id, lease.Microseconds()).Scan(
		&job.Event.ID, &job.Event.EndpointID, &job.Event.Type, &job.Event.Payload,
		&job.Event.Status, &job.Event.CreatedAt, &job.LeaseVersion, &job.LeaseUntil,
		&job.AttemptNumber, &job.CycleAttempt,
	)
	if err != nil {
		return DeliveryJob{}, databaseError("claim event", err, ErrConflict)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO delivery_attempts (event_id, attempt_number, started_at)
		VALUES ($1, $2, clock_timestamp())
	`, id, job.AttemptNumber)
	if err != nil {
		return DeliveryJob{}, databaseError("start delivery attempt", err, ErrConflict)
	}
	if err := tx.Commit(ctx); err != nil {
		return DeliveryJob{}, databaseError("commit event claim", err, ErrConflict)
	}
	job.Event.CreatedAt = job.Event.CreatedAt.UTC()
	job.LeaseUntil = job.LeaseUntil.UTC()
	return job, nil
}

func (s *PostgresStore) FinishEvent(ctx context.Context, job DeliveryJob, result domain.DeliveryResult) error {
	if !result.Valid() {
		return ErrInvalidTransition
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return databaseError("begin delivery result", err, ErrConflict)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanupCtx)
	}()
	var cycleAttempts int
	err = tx.QueryRow(ctx, `
		SELECT cycle_attempts FROM events
		WHERE id = $1 AND status = 'processing' AND lease_version = $2 AND lease_until > clock_timestamp()
		FOR UPDATE
	`, job.Event.ID, job.LeaseVersion).Scan(&cycleAttempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrLeaseLost
	}
	if err != nil {
		return databaseError("check delivery owner", err, ErrConflict)
	}
	changed, err := tx.Exec(ctx, `
		UPDATE delivery_attempts
		SET finished_at = GREATEST(clock_timestamp(), started_at),
		    http_status = NULLIF($3::integer, 0), error_message = NULLIF($4::text, '')
		WHERE event_id = $1 AND attempt_number = $2 AND finished_at IS NULL
	`, job.Event.ID, job.AttemptNumber, result.HTTPStatus, result.ErrorMessage)
	if err != nil {
		return databaseError("save delivery result", err, ErrConflict)
	}
	if changed.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	status, delay := result.NextStatus(cycleAttempts)
	changed, err = tx.Exec(ctx, `
		UPDATE events SET status = $1, lease_until = NULL,
		    next_attempt_at = CASE WHEN $1 = 'pending'
		        THEN clock_timestamp() + ($4::bigint * interval '1 microsecond') ELSE next_attempt_at END
		WHERE id = $2 AND status = 'processing' AND lease_version = $3 AND lease_until > clock_timestamp()
	`, status, job.Event.ID, job.LeaseVersion, delay.Microseconds())
	if err != nil {
		return databaseError("finish event", err, ErrConflict)
	}
	if changed.RowsAffected() != 1 {
		return ErrLeaseLost
	}
	return databaseError("commit delivery result", tx.Commit(ctx), ErrConflict)
}
