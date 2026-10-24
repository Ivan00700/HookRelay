package repository

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"hookrelay/internal/domain"
)

type queueStore interface {
	Store
	DeliveryStore
}

func checkQueue(t *testing.T, store queueStore, expire func(string)) {
	t.Helper()
	ctx := context.Background()
	if err := store.CreateEndpoint(ctx, domain.Endpoint{ID: "endpoint", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		if err := store.CreateEvent(ctx, domain.Event{ID: id, EndpointID: "endpoint", Type: "test", Payload: json.RawMessage(`{}`), Status: domain.DeliveryStatusPending, CreatedAt: time.Unix(1, 0).UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := store.ClaimEvent(ctx, time.Minute)
	if err != nil || first.Event.ID != "a" || first.Event.Status != domain.DeliveryStatusProcessing || first.LeaseVersion < 1 {
		t.Fatalf("first claim: %+v, %v", first, err)
	}
	second, err := store.ClaimEvent(ctx, time.Minute)
	if err != nil || second.Event.ID != "b" {
		t.Fatalf("second claim: %+v, %v", second, err)
	}
	if _, err := store.ClaimEvent(ctx, time.Minute); !errors.Is(err, ErrNoJob) {
		t.Fatalf("active jobs must not be claimed again: %v", err)
	}
	if err := store.FinishEvent(ctx, first, domain.DeliveryStatusPending); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("invalid finish: %v", err)
	}
	if err := store.UpdateEventStatus(ctx, "a", domain.DeliveryStatusDelivered); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("unfenced status update was accepted: %v", err)
	}
	expire("a")
	if err := store.FinishEvent(ctx, first, domain.DeliveryStatusDelivered); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("expired lease was accepted: %v", err)
	}
	reclaimed, err := store.ClaimEvent(ctx, time.Minute)
	if err != nil || reclaimed.Event.ID != "a" || reclaimed.LeaseVersion <= first.LeaseVersion {
		t.Fatalf("reclaimed job: %+v, %v", reclaimed, err)
	}
	if err := store.FinishEvent(ctx, first, domain.DeliveryStatusFailed); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("old owner overwrote new lease: %v", err)
	}
	if err := store.FinishEvent(ctx, reclaimed, domain.DeliveryStatusDelivered); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishEvent(ctx, second, domain.DeliveryStatusFailed); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimEvent(ctx, time.Minute); !errors.Is(err, ErrNoJob) {
		t.Fatalf("terminal events must not be redelivered: %v", err)
	}
	if err := store.CreateEvent(ctx, domain.Event{ID: "legacy", EndpointID: "endpoint", Type: "test", Payload: json.RawMessage(`{}`), Status: domain.DeliveryStatusProcessing, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	legacy, err := store.ClaimEvent(ctx, time.Minute)
	if err != nil || legacy.Event.ID != "legacy" {
		t.Fatalf("legacy processing event was not recovered: %+v, %v", legacy, err)
	}
	if err := store.FinishEvent(ctx, legacy, domain.DeliveryStatusDelivered); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ClaimEvent(ctx, 0); err == nil {
		t.Fatal("zero lease accepted")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := store.ClaimEvent(canceled, time.Minute); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled claim: %v", err)
	}
}

func TestMemoryQueue(t *testing.T) {
	store := NewMemoryStore()
	checkQueue(t, store, func(id string) {
		store.mu.Lock()
		store.leases[id] = time.Now().Add(-time.Second)
		store.mu.Unlock()
	})
}
