package repository

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"hookrelay/internal/domain"
	"hookrelay/internal/postgres"
	"hookrelay/internal/testutil"
)

func TestPostgresRetrySurvivesReconnectIntegration(t *testing.T) {
	pool, databaseURL := testutil.Database(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	store := NewPostgresStore(pool)
	retryFixture(t, store, "reconnect")
	job, err := store.ClaimEvent(ctx, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.FinishEvent(ctx, job, domain.DeliveryResult{HTTPStatus: 503, ErrorMessage: "unavailable", Retryable: true}); err != nil {
		t.Fatal(err)
	}
	var scheduled time.Time
	if err := pool.QueryRow(ctx, `SELECT next_attempt_at FROM events WHERE id = 'reconnect'`).Scan(&scheduled); err != nil {
		t.Fatal(err)
	}
	pool.Close()
	reopened, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	store = NewPostgresStore(reopened)
	var afterReconnect time.Time
	if err := reopened.QueryRow(ctx, `SELECT next_attempt_at FROM events WHERE id = 'reconnect'`).Scan(&afterReconnect); err != nil {
		t.Fatal(err)
	}
	if !scheduled.Equal(afterReconnect) {
		t.Fatal("retry schedule changed after reconnect")
	}
	if _, err := store.ClaimEvent(ctx, time.Minute); !errors.Is(err, ErrNoJob) {
		t.Fatalf("future retry claimed after reconnect: %v", err)
	}
	attempts, err := store.ListAttempts(ctx, "reconnect")
	if err != nil || len(attempts) != 1 || attempts[0].HTTPStatus == nil || *attempts[0].HTTPStatus != 503 {
		t.Fatalf("history after reconnect: %+v, %v", attempts, err)
	}
	if _, err := reopened.Exec(ctx, `UPDATE events SET next_attempt_at = clock_timestamp() - interval '1 second' WHERE id = 'reconnect'`); err != nil {
		t.Fatal(err)
	}
	job, err = store.ClaimEvent(ctx, time.Minute)
	if err != nil || job.AttemptNumber != 2 || job.CycleAttempt != 2 {
		t.Fatalf("retry counters after reconnect: %+v, %v", job, err)
	}
}

func TestPostgresRetriesIntegration(t *testing.T) {
	pool, _ := testutil.Database(t)
	store := NewPostgresStore(pool)
	checkRetries(t, store, func(id string) {
		if _, err := pool.Exec(context.Background(), `UPDATE events SET next_attempt_at = clock_timestamp() - interval '1 second' WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
	}, func(id string) time.Time {
		var next time.Time
		if err := pool.QueryRow(context.Background(), `SELECT next_attempt_at FROM events WHERE id = $1`, id).Scan(&next); err != nil {
			t.Fatal(err)
		}
		return next
	})
}

func TestPostgresConcurrentRetryIntegration(t *testing.T) {
	pool, _ := testutil.Database(t)
	checkConcurrentRetry(t, NewPostgresStore(pool))
}

func checkInterruptedAttemptLimit(t *testing.T, store queueStore, expire func(string)) {
	t.Helper()
	ctx := context.Background()
	retryFixture(t, store, "interrupted")
	var job DeliveryJob
	for i := 0; i < domain.MaxDeliveryAttempts; i++ {
		var err error
		job, err = store.ClaimEvent(ctx, time.Minute)
		if err != nil || job.CycleAttempt != i+1 {
			t.Fatalf("interrupted claim: %+v, %v", job, err)
		}
		expire("interrupted")
	}
	if _, err := store.ClaimEvent(ctx, time.Minute); !errors.Is(err, ErrNoJob) {
		t.Fatalf("fifth interrupted attempt was allowed: %v", err)
	}
	event, err := store.GetEvent(ctx, "interrupted")
	if err != nil || event.Status != domain.DeliveryStatusFailed {
		t.Fatalf("exhausted interrupted event: %+v, %v", event, err)
	}
	attempts, err := store.ListAttempts(ctx, "interrupted")
	if err != nil || len(attempts) != domain.MaxDeliveryAttempts {
		t.Fatalf("interrupted history: %+v, %v", attempts, err)
	}
	for _, attempt := range attempts {
		if attempt.FinishedAt == nil || attempt.HTTPStatus != nil || !strings.Contains(attempt.ErrorMessage, "interrupted") {
			t.Fatalf("incomplete interrupted history: %+v", attempt)
		}
	}
	if err := store.FinishEvent(ctx, job, domain.DeliveryResult{HTTPStatus: 204}); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("expired final attempt finished: %v", err)
	}
	if _, err := store.RetryEvent(ctx, "interrupted"); err != nil {
		t.Fatal(err)
	}
	job, err = store.ClaimEvent(ctx, time.Minute)
	if err != nil || job.CycleAttempt != 1 || job.AttemptNumber != 5 {
		t.Fatalf("manual retry after interruption: %+v, %v", job, err)
	}
}

func TestPostgresInterruptedAttemptLimitIntegration(t *testing.T) {
	pool, _ := testutil.Database(t)
	checkInterruptedAttemptLimit(t, NewPostgresStore(pool), func(id string) {
		if _, err := pool.Exec(context.Background(), `UPDATE events SET lease_until = clock_timestamp() - interval '1 second' WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
	})
}

func TestMemoryInterruptedAttemptLimit(t *testing.T) {
	store := NewMemoryStore()
	checkInterruptedAttemptLimit(t, store, func(id string) {
		store.mu.Lock()
		store.leases[id] = time.Now().Add(-time.Second)
		store.mu.Unlock()
	})
}

func TestPostgresDeliveryResultIsAtomicIntegration(t *testing.T) {
	pool, _ := testutil.Database(t)
	store := NewPostgresStore(pool)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	retryFixture(t, store, "atomic")
	job, err := store.ClaimEvent(ctx, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE events ADD CONSTRAINT test_block_delivered CHECK (status <> 'delivered')`); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishEvent(ctx, job, domain.DeliveryResult{HTTPStatus: 204}); err == nil {
		t.Fatal("expected the final event update to fail")
	}
	attempts, err := store.ListAttempts(ctx, "atomic")
	if err != nil || len(attempts) != 1 || attempts[0].FinishedAt != nil || attempts[0].HTTPStatus != nil {
		t.Fatalf("partial history update was committed: %+v, %v", attempts, err)
	}
	event, err := store.GetEvent(ctx, "atomic")
	if err != nil || event.Status != domain.DeliveryStatusProcessing {
		t.Fatalf("event changed after rollback: %+v, %v", event, err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE events DROP CONSTRAINT test_block_delivered`); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishEvent(ctx, job, domain.DeliveryResult{HTTPStatus: 204}); err != nil {
		t.Fatal(err)
	}
}
