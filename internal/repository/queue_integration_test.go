package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"hookrelay/internal/domain"
	"hookrelay/internal/testutil"
)

func TestPostgresQueueIntegration(t *testing.T) {
	pool, _ := testutil.Database(t)
	store := NewPostgresStore(pool)
	checkQueue(t, store, func(id string) {
		if _, err := pool.Exec(context.Background(), `UPDATE events SET lease_until = clock_timestamp() - interval '1 second' WHERE id = $1`, id); err != nil {
			t.Fatal(err)
		}
	})
}

func TestPostgresQueueSkipsLockedRowsIntegration(t *testing.T) {
	pool, _ := testutil.Database(t)
	store := NewPostgresStore(pool)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := store.CreateEndpoint(ctx, domain.Endpoint{ID: "endpoint", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		if err := store.CreateEvent(ctx, domain.Event{ID: id, EndpointID: "endpoint", Type: "test", Payload: json.RawMessage(`{}`), Status: domain.DeliveryStatusPending, CreatedAt: time.Unix(1, 0).UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var id string
	if err := tx.QueryRow(ctx, `SELECT id FROM events WHERE id = 'a' FOR UPDATE`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	job, err := store.ClaimEvent(ctx, time.Minute)
	if err != nil || job.Event.ID != "b" {
		t.Fatalf("claim did not skip locked row: %+v, %v", job, err)
	}
}

func TestPostgresQueueConcurrentClaimsIntegration(t *testing.T) {
	pool, _ := testutil.Database(t)
	store := NewPostgresStore(pool)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := store.CreateEndpoint(ctx, domain.Endpoint{ID: "endpoint", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		if err := store.CreateEvent(ctx, domain.Event{ID: fmt.Sprintf("event-%02d", i), EndpointID: "endpoint", Type: "test", Payload: json.RawMessage(`{}`), Status: domain.DeliveryStatusPending, CreatedAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	jobs := make(chan DeliveryJob, 12)
	errs := make(chan error, 4)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			workerStore := NewPostgresStore(pool)
			for {
				job, err := workerStore.ClaimEvent(ctx, time.Minute)
				if errors.Is(err, ErrNoJob) {
					return
				}
				if err != nil {
					errs <- err
					return
				}
				jobs <- job
			}
		}()
	}
	wg.Wait()
	close(jobs)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for job := range jobs {
		if seen[job.Event.ID] {
			t.Fatalf("event was claimed twice: %s", job.Event.ID)
		}
		seen[job.Event.ID] = true
		if err := store.FinishEvent(ctx, job, domain.DeliveryStatusDelivered); err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != 12 {
		t.Fatalf("claimed %d events, want 12", len(seen))
	}
}

func TestPostgresQueueLeaseExpiresIntegration(t *testing.T) {
	pool, _ := testutil.Database(t)
	store := NewPostgresStore(pool)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := store.CreateEndpoint(ctx, domain.Endpoint{ID: "endpoint", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateEvent(ctx, domain.Event{ID: "event", EndpointID: "endpoint", Type: "test", Payload: json.RawMessage(`{}`), Status: domain.DeliveryStatusPending, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	first, err := store.ClaimEvent(ctx, 100*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	for ctx.Err() == nil {
		second, err := store.ClaimEvent(ctx, time.Minute)
		if err == nil {
			if second.Event.ID != first.Event.ID || second.LeaseVersion <= first.LeaseVersion {
				t.Fatalf("wrong recovered job: %+v", second)
			}
			if err := store.FinishEvent(ctx, first, domain.DeliveryStatusFailed); !errors.Is(err, ErrLeaseLost) {
				t.Fatalf("expired owner finished the event: %v", err)
			}
			return
		}
		if !errors.Is(err, ErrNoJob) {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("lease did not expire")
}
