package repository

import (
	"context"
	"errors"
	"testing"

	"hookrelay/internal/domain"
	"hookrelay/internal/testutil"
)

func checkEventCounts(t *testing.T, store queueStore) {
	t.Helper()
	ctx := context.Background()
	if err := store.CheckReady(ctx); err != nil {
		t.Fatalf("ready store rejected: %v", err)
	}
	for _, status := range []domain.DeliveryStatus{domain.DeliveryStatusPending, domain.DeliveryStatusProcessing, domain.DeliveryStatusDelivered, domain.DeliveryStatusFailed} {
		id := string(status)
		retryFixture(t, store, id)
		if status == domain.DeliveryStatusPending {
			continue
		}
		if err := store.UpdateEventStatus(ctx, id, domain.DeliveryStatusProcessing); err != nil {
			t.Fatal(err)
		}
		if status != domain.DeliveryStatusProcessing {
			if err := store.UpdateEventStatus(ctx, id, status); err != nil {
				t.Fatal(err)
			}
		}
	}
	counts, err := store.EventCounts(ctx)
	if err != nil || len(counts) != 4 {
		t.Fatalf("event counts: %+v, %v", counts, err)
	}
	for status, count := range counts {
		if count != 1 {
			t.Fatalf("%s count=%d, want 1", status, count)
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := store.EventCounts(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled count query: %v", err)
	}
	if err := store.CheckReady(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled readiness check: %v", err)
	}
}

func TestMemoryEventCounts(t *testing.T) {
	checkEventCounts(t, NewMemoryStore())
}

func TestPostgresEventCountsIntegration(t *testing.T) {
	pool, _ := testutil.Database(t)
	store := NewPostgresStore(pool)
	checkEventCounts(t, store)
	pool.Close()
	if err := store.CheckReady(context.Background()); err == nil {
		t.Fatal("closed database pool reported ready")
	}
}
