package delivery

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hookrelay/internal/domain"
	"hookrelay/internal/repository"
)

type senderFunc func(context.Context, domain.Event, domain.Endpoint) error

func (f senderFunc) Send(ctx context.Context, event domain.Event, endpoint domain.Endpoint) error {
	return f(ctx, event, endpoint)
}

func poolStore(t *testing.T, ids ...string) *repository.MemoryStore {
	t.Helper()
	store := repository.NewMemoryStore()
	if err := store.CreateEndpoint(context.Background(), domain.Endpoint{ID: "ep-1", URL: "http://example.test/webhook"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if err := store.CreateEvent(context.Background(), domain.Event{ID: id, EndpointID: "ep-1", Status: domain.DeliveryStatusPending}); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

func waitEvents(t *testing.T, store repository.Store, expected map[string]domain.DeliveryStatus) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ready := true
		for id, status := range expected {
			event, err := store.GetEvent(context.Background(), id)
			if err != nil {
				t.Fatal(err)
			}
			if event.Status != status {
				ready = false
			}
		}
		if ready {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("events did not reach the expected statuses")
}

func TestPoolDeliversSavedEvents(t *testing.T) {
	store := poolStore(t, "ok", "fail")
	sender := senderFunc(func(_ context.Context, event domain.Event, endpoint domain.Endpoint) error {
		if event.Status != domain.DeliveryStatusProcessing || endpoint.ID != "ep-1" {
			t.Error("sender received an unclaimed event or wrong endpoint")
		}
		if event.ID == "fail" {
			return errors.New("receiver failed")
		}
		return nil
	})
	pool, err := NewPool(store, sender, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	waitEvents(t, store, map[string]domain.DeliveryStatus{"ok": domain.DeliveryStatusDelivered, "fail": domain.DeliveryStatusFailed})
	pool.Close()
	pool.Close()
}

func TestPoolWorkerLimit(t *testing.T) {
	store := poolStore(t, "one", "two", "three", "four")
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	var active, maximum atomic.Int32
	sender := senderFunc(func(context.Context, domain.Event, domain.Endpoint) error {
		current := active.Add(1)
		for {
			old := maximum.Load()
			if current <= old || maximum.CompareAndSwap(old, current) {
				break
			}
		}
		started <- struct{}{}
		<-release
		active.Add(-1)
		return nil
	})
	pool, err := NewPool(store, sender, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { unblock(); pool.Close() }()
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("workers did not start")
		}
	}
	if maximum.Load() != 2 {
		t.Fatalf("active workers=%d, want 2", maximum.Load())
	}
	unblock()
	waitEvents(t, store, map[string]domain.DeliveryStatus{
		"one": domain.DeliveryStatusDelivered, "two": domain.DeliveryStatusDelivered,
		"three": domain.DeliveryStatusDelivered, "four": domain.DeliveryStatusDelivered,
	})
	if maximum.Load() > 2 {
		t.Fatalf("worker limit exceeded: %d", maximum.Load())
	}
}

func TestCloseFinishesActiveJobAndLeavesPending(t *testing.T) {
	store := poolStore(t, "a", "b")
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	pool, err := NewPool(store, senderFunc(func(context.Context, domain.Event, domain.Endpoint) error {
		started <- struct{}{}
		<-release
		return nil
	}), 1)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { unblock(); pool.Close() }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("delivery did not start")
	}
	done := make(chan struct{})
	go func() { pool.Close(); close(done) }()
	<-pool.ctx.Done()
	unblock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("pool did not stop")
	}
	waitEvents(t, store, map[string]domain.DeliveryStatus{"a": domain.DeliveryStatusDelivered, "b": domain.DeliveryStatusPending})
}

func TestNewPoolRejectsInvalidConfiguration(t *testing.T) {
	store := poolStore(t)
	sender := senderFunc(func(context.Context, domain.Event, domain.Endpoint) error { return nil })
	for _, workers := range []int{0, -1} {
		if _, err := NewPool(store, sender, workers); err == nil {
			t.Fatal("invalid worker count accepted")
		}
	}
	if _, err := NewPool(nil, sender, 1); err == nil {
		t.Fatal("nil store accepted")
	}
	if _, err := NewPool(store, nil, 1); err == nil {
		t.Fatal("nil sender accepted")
	}
}

type temporaryClaimError struct {
	repository.DeliveryStore
	failed atomic.Bool
}

func (s *temporaryClaimError) ClaimEvent(ctx context.Context, lease time.Duration) (repository.DeliveryJob, error) {
	if s.failed.CompareAndSwap(false, true) {
		return repository.DeliveryJob{}, errors.New("temporary database error")
	}
	return s.DeliveryStore.ClaimEvent(ctx, lease)
}

func TestPoolContinuesAfterClaimError(t *testing.T) {
	store := poolStore(t, "event")
	queue := &temporaryClaimError{DeliveryStore: store}
	pool, err := NewPool(queue, senderFunc(func(context.Context, domain.Event, domain.Endpoint) error { return nil }), 1)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	waitEvents(t, store, map[string]domain.DeliveryStatus{"event": domain.DeliveryStatusDelivered})
}
