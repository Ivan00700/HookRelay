package repository

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"hookrelay/internal/domain"
	"hookrelay/internal/postgres"
	"hookrelay/internal/testutil"
)

func TestPostgresStoreIntegration(t *testing.T) {
	pool, databaseURL := testutil.Database(t)
	store := NewPostgresStore(pool)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	endpoints, err := store.ListEndpoints(ctx)
	if err != nil || endpoints == nil || len(endpoints) != 0 {
		t.Fatalf("empty endpoints: %v, %v", endpoints, err)
	}
	events, err := store.ListEvents(ctx)
	if err != nil || events == nil || len(events) != 0 {
		t.Fatalf("empty events: %v, %v", events, err)
	}
	if _, err := store.GetEndpoint(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing endpoint: %v", err)
	}
	if _, err := store.GetEvent(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing event: %v", err)
	}
	if err := store.DeleteEndpoint(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete missing endpoint: %v", err)
	}
	if err := store.DeleteEvent(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete missing event: %v", err)
	}
	createdAt := time.Now().UTC().Truncate(time.Microsecond)
	for _, id := range []string{"endpoint-b", "endpoint-a"} {
		endpoint := domain.Endpoint{ID: id, Name: "orders", URL: "http://example.test/webhook", Secret: "private", CreatedAt: createdAt}
		if err := store.CreateEndpoint(ctx, endpoint); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.CreateEndpoint(ctx, domain.Endpoint{ID: "endpoint-a", CreatedAt: createdAt}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate endpoint: %v", err)
	}
	endpoints, err = store.ListEndpoints(ctx)
	if err != nil || len(endpoints) != 2 || endpoints[0].ID != "endpoint-a" || endpoints[1].ID != "endpoint-b" {
		t.Fatalf("endpoint order: %+v, %v", endpoints, err)
	}
	endpoint, err := store.GetEndpoint(ctx, "endpoint-a")
	if err != nil || endpoint.Secret != "private" || !endpoint.CreatedAt.Equal(createdAt) || endpoint.CreatedAt.Location() != time.UTC {
		t.Fatalf("read endpoint: %+v, %v", endpoint, err)
	}
	if _, err := store.GetEndpoint(ctx, "' OR true --"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SQL-looking ID must be treated as data: %v", err)
	}
	missing := domain.Event{ID: "missing-parent", EndpointID: "missing", Type: "order.created", Payload: json.RawMessage(`{}`), Status: domain.DeliveryStatusPending, CreatedAt: createdAt}
	if err := store.CreateEvent(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing parent: %v", err)
	}
	for _, id := range []string{"event-b", "event-a"} {
		event := domain.Event{ID: id, EndpointID: "endpoint-a", Type: "order.created", Payload: json.RawMessage(`{"order_id":42}`), Status: domain.DeliveryStatusPending, CreatedAt: createdAt}
		if err := store.CreateEvent(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	event, err := store.GetEvent(ctx, "event-a")
	if err != nil || event.Type != "order.created" || event.Status != domain.DeliveryStatusPending || !event.CreatedAt.Equal(createdAt) {
		t.Fatalf("read event: %+v, %v", event, err)
	}
	var payload map[string]int
	if err := json.Unmarshal(event.Payload, &payload); err != nil || payload["order_id"] != 42 {
		t.Fatalf("payload: %s, %v", event.Payload, err)
	}
	if err := store.CreateEvent(ctx, event); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate event: %v", err)
	}
	events, err = store.ListEvents(ctx)
	if err != nil || len(events) != 2 || events[0].ID != "event-a" || events[1].ID != "event-b" {
		t.Fatalf("event order: %+v, %v", events, err)
	}
	if err := store.DeleteEndpoint(ctx, "endpoint-a"); !errors.Is(err, ErrConflict) {
		t.Fatalf("delete referenced endpoint: %v", err)
	}
	if err := store.UpdateEventStatus(ctx, "missing", domain.DeliveryStatusProcessing); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update missing event: %v", err)
	}
	if err := store.UpdateEventStatus(ctx, "event-a", domain.DeliveryStatusDelivered); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("skip processing: %v", err)
	}
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- store.UpdateEventStatus(ctx, "event-a", domain.DeliveryStatusProcessing)
		}()
	}
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, ErrInvalidTransition) {
			conflicts++
		} else {
			t.Fatalf("concurrent update: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent update: successes=%d conflicts=%d", successes, conflicts)
	}
	if err := store.UpdateEventStatus(ctx, "event-a", domain.DeliveryStatusDelivered); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateEventStatus(ctx, "event-a", domain.DeliveryStatusProcessing); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("restart terminal event: %v", err)
	}

	reopenedPool, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer reopenedPool.Close()
	reopened := NewPostgresStore(reopenedPool)
	persisted, err := reopened.GetEvent(ctx, "event-a")
	if err != nil || persisted.Status != domain.DeliveryStatusDelivered {
		t.Fatalf("read from another pool: %+v, %v", persisted, err)
	}
	if err := store.DeleteEvent(ctx, "event-b"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetEvent(ctx, "event-b"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted event: %v", err)
	}
	if err := store.DeleteEndpoint(ctx, "endpoint-b"); err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO delivery_attempts (event_id, attempt_number, started_at) VALUES ($1, 1, CURRENT_TIMESTAMP)`, "event-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteEvent(ctx, "event-a"); !errors.Is(err, ErrConflict) {
		t.Fatalf("delete event with history: %v", err)
	}

	canceled, stop := context.WithCancel(ctx)
	stop()
	if _, err := store.ListEndpoints(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled query: %v", err)
	}
	if _, err := store.GetEvent(canceled, "event-a"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read: %v", err)
	}
	if err := store.UpdateEventStatus(canceled, "event-b", domain.DeliveryStatusProcessing); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled update: %v", err)
	}
}

func TestPostgresStoreLockTimeoutIntegration(t *testing.T) {
	pool, _ := testutil.Database(t)
	store := NewPostgresStore(pool)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := store.CreateEndpoint(ctx, domain.Endpoint{ID: "endpoint", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateEvent(ctx, domain.Event{
		ID: "event", EndpointID: "endpoint", Type: "test", Payload: json.RawMessage(`{}`),
		Status: domain.DeliveryStatusPending, CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	lock, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Second)
		defer cleanupCancel()
		_ = lock.Rollback(cleanupCtx)
	}()
	var status domain.DeliveryStatus
	if err := lock.QueryRow(ctx, `SELECT status FROM events WHERE id = $1 FOR UPDATE`, "event").Scan(&status); err != nil {
		t.Fatal(err)
	}
	waitCtx, stop := context.WithTimeout(ctx, 50*time.Millisecond)
	err = store.UpdateEventStatus(waitCtx, "event", domain.DeliveryStatusProcessing)
	stop()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting for row lock: %v", err)
	}
	if err := lock.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	event, err := store.GetEvent(ctx, "event")
	if err != nil || event.Status != domain.DeliveryStatusPending {
		t.Fatalf("timed-out update changed event: %+v, %v", event, err)
	}
	if err := store.UpdateEventStatus(ctx, "event", domain.DeliveryStatusProcessing); err != nil {
		t.Fatalf("update after lock release: %v", err)
	}
}
