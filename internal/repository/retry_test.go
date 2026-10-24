package repository

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"hookrelay/internal/domain"
)

func retryFixture(t *testing.T, store queueStore, id string) {
	t.Helper()
	ctx := context.Background()
	if err := store.CreateEndpoint(ctx, domain.Endpoint{ID: id, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateEvent(ctx, domain.Event{ID: id, EndpointID: id, Type: "test", Payload: json.RawMessage(`{}`), Status: domain.DeliveryStatusPending, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
}

func checkRetries(t *testing.T, store queueStore, due func(string), scheduled func(string) time.Time) {
	t.Helper()
	ctx := context.Background()
	if _, err := store.ListAttempts(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown history: %v", err)
	}
	if _, err := store.RetryEvent(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown retry: %v", err)
	}
	retryFixture(t, store, "event")
	attempts, err := store.ListAttempts(ctx, "event")
	if err != nil || attempts == nil || len(attempts) != 0 {
		t.Fatalf("empty history: %+v, %v", attempts, err)
	}
	if _, err := store.RetryEvent(ctx, "event"); !errors.Is(err, ErrConflict) {
		t.Fatalf("pending event can be retried: %v", err)
	}
	for number := 1; number <= domain.MaxDeliveryAttempts; number++ {
		job, err := store.ClaimEvent(ctx, time.Minute)
		if err != nil || job.AttemptNumber != number || job.CycleAttempt != number {
			t.Fatalf("claim %d: %+v, %v", number, job, err)
		}
		attempts, err = store.ListAttempts(ctx, "event")
		if err != nil || len(attempts) != number || attempts[number-1].FinishedAt != nil || attempts[number-1].HTTPStatus != nil {
			t.Fatalf("unfinished attempt %d: %+v, %v", number, attempts, err)
		}
		if _, err := store.RetryEvent(ctx, "event"); !errors.Is(err, ErrConflict) {
			t.Fatalf("processing retry: %v", err)
		}
		if err := store.FinishEvent(ctx, job, domain.DeliveryResult{HTTPStatus: 500, ErrorMessage: "server error", Retryable: true}); err != nil {
			t.Fatal(err)
		}
		attempts, err = store.ListAttempts(ctx, "event")
		if err != nil {
			t.Fatal(err)
		}
		attempt := attempts[number-1]
		if attempt.AttemptNumber != number || attempt.ID <= 0 || attempt.FinishedAt == nil || attempt.HTTPStatus == nil || *attempt.HTTPStatus != 500 || attempt.ErrorMessage != "server error" {
			t.Fatalf("completed attempt: %+v", attempt)
		}
		event, err := store.GetEvent(ctx, "event")
		if err != nil {
			t.Fatal(err)
		}
		if number < domain.MaxDeliveryAttempts {
			if event.Status != domain.DeliveryStatusPending {
				t.Fatalf("retry status: %s", event.Status)
			}
			delay := scheduled("event").Sub(*attempt.FinishedAt)
			if delay < domain.RetryDelay(number) || delay > domain.RetryDelay(number)+time.Second {
				t.Fatalf("attempt %d delay=%v, want %v", number, delay, domain.RetryDelay(number))
			}
			if _, err := store.ClaimEvent(ctx, time.Minute); !errors.Is(err, ErrNoJob) {
				t.Fatalf("future retry was claimed early: %v", err)
			}
			due("event")
		} else if event.Status != domain.DeliveryStatusFailed {
			t.Fatalf("fourth failure status: %s", event.Status)
		}
	}
	if _, err := store.ClaimEvent(ctx, time.Minute); !errors.Is(err, ErrNoJob) {
		t.Fatalf("exhausted event was claimed: %v", err)
	}
	if event, err := store.RetryEvent(ctx, "event"); err != nil || event.Status != domain.DeliveryStatusPending {
		t.Fatalf("manual retry: %+v, %v", event, err)
	}
	job, err := store.ClaimEvent(ctx, time.Minute)
	if err != nil || job.AttemptNumber != 5 || job.CycleAttempt != 1 {
		t.Fatalf("new cycle: %+v, %v", job, err)
	}
	if err := store.FinishEvent(ctx, job, domain.DeliveryResult{ErrorMessage: "timeout", Retryable: true}); err != nil {
		t.Fatal(err)
	}
	attempts, err = store.ListAttempts(ctx, "event")
	if err != nil || len(attempts) != 5 || attempts[4].HTTPStatus != nil || attempts[4].ErrorMessage != "timeout" {
		t.Fatalf("network error history: %+v, %v", attempts, err)
	}
	due("event")
	job, err = store.ClaimEvent(ctx, time.Minute)
	if err != nil || job.AttemptNumber != 6 || job.CycleAttempt != 2 {
		t.Fatalf("continued cycle: %+v, %v", job, err)
	}
	if err := store.FinishEvent(ctx, job, domain.DeliveryResult{HTTPStatus: 204}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RetryEvent(ctx, "event"); !errors.Is(err, ErrConflict) {
		t.Fatalf("delivered retry: %v", err)
	}
	attempts, err = store.ListAttempts(ctx, "event")
	if err != nil || len(attempts) != 6 || attempts[5].HTTPStatus == nil || *attempts[5].HTTPStatus != 204 || attempts[5].ErrorMessage != "" {
		t.Fatalf("retained history: %+v, %v", attempts, err)
	}
}

func checkConcurrentRetry(t *testing.T, store queueStore) {
	t.Helper()
	ctx := context.Background()
	retryFixture(t, store, "concurrent")
	job, err := store.ClaimEvent(ctx, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.FinishEvent(ctx, job, domain.DeliveryResult{HTTPStatus: 400, ErrorMessage: "bad request"}); err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.RetryEvent(ctx, "concurrent")
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("manual retries: successes=%d conflicts=%d", successes, conflicts)
	}
}

func TestMemoryRetries(t *testing.T) {
	store := NewMemoryStore()
	checkRetries(t, store, func(id string) {
		store.mu.Lock()
		store.next[id] = time.Now().Add(-time.Second)
		store.mu.Unlock()
	}, func(id string) time.Time {
		store.mu.RLock()
		defer store.mu.RUnlock()
		return store.next[id]
	})
	attempts, _ := store.ListAttempts(context.Background(), "event")
	*attempts[0].HTTPStatus = 999
	*attempts[0].FinishedAt = time.Time{}
	stored, _ := store.ListAttempts(context.Background(), "event")
	if *stored[0].HTTPStatus != 500 || stored[0].FinishedAt.IsZero() {
		t.Fatal("history shares mutable pointers")
	}
}

func TestMemoryConcurrentRetry(t *testing.T) {
	checkConcurrentRetry(t, NewMemoryStore())
}
