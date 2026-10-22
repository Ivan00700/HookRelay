package delivery

import (
	"context"
	"errors"
	"fmt"
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
	if err := store.CreateEndpoint(context.Background(), domain.Endpoint{ID: "ep-1", URL: "http://example.com/webhook"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if err := store.CreateEvent(context.Background(), domain.Event{ID: id, EndpointID: "ep-1", Status: domain.DeliveryStatusPending}); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

func TestPoolDeliversAndDrainsOnClose(t *testing.T) {
	store := poolStore(t, "ok", "fail")
	sender := senderFunc(func(_ context.Context, event domain.Event, endpoint domain.Endpoint) error {
		if endpoint.ID != "ep-1" || event.Status != domain.DeliveryStatusProcessing {
			t.Errorf("sender received event %+v and endpoint %+v", event, endpoint)
		}
		if event.ID == "fail" {
			return errors.New("receiver failed")
		}
		return nil
	})
	pool, err := NewPool(store, sender, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"ok", "fail"} {
		if err := pool.Submit(id); err != nil {
			t.Fatal(err)
		}
	}
	pool.Close()
	pool.Close()

	for id, want := range map[string]domain.DeliveryStatus{
		"ok":   domain.DeliveryStatusDelivered,
		"fail": domain.DeliveryStatusFailed,
	} {
		event, err := store.GetEvent(context.Background(), id)
		if err != nil || event.Status != want {
			t.Errorf("event %s = %+v, error = %v; want %s", id, event, err, want)
		}
	}
	if err := pool.Submit("ok"); !errors.Is(err, ErrClosed) {
		t.Fatalf("submit after close = %v, want ErrClosed", err)
	}
}

func TestPoolWorkerLimit(t *testing.T) {
	ids := []string{"one", "two", "three", "four"}
	store := poolStore(t, ids...)
	started := make(chan string, len(ids))
	release := make(chan struct{})
	var active atomic.Int32
	var maximum atomic.Int32
	sender := senderFunc(func(_ context.Context, event domain.Event, _ domain.Endpoint) error {
		current := active.Add(1)
		for {
			old := maximum.Load()
			if current <= old || maximum.CompareAndSwap(old, current) {
				break
			}
		}
		started <- event.ID
		<-release
		active.Add(-1)
		return nil
	})
	pool, err := NewPool(store, sender, 2, len(ids))
	if err != nil {
		t.Fatal(err)
	}
	finish := sync.OnceFunc(func() {
		close(release)
		pool.Close()
	})
	defer finish()
	for _, id := range ids {
		if err := pool.Submit(id); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("workers did not start")
		}
	}
	if got := maximum.Load(); got != 2 {
		t.Errorf("maximum active workers = %d, want 2", got)
	}
	finish()
	if got := maximum.Load(); got > 2 {
		t.Errorf("maximum active workers = %d, want at most 2", got)
	}
	for _, id := range ids {
		event, _ := store.GetEvent(context.Background(), id)
		if event.Status != domain.DeliveryStatusDelivered {
			t.Errorf("event %s status = %s", id, event.Status)
		}
	}
}

func TestNewPoolRejectsInvalidConfiguration(t *testing.T) {
	store := poolStore(t)
	sender := senderFunc(func(context.Context, domain.Event, domain.Endpoint) error { return nil })
	for _, tt := range []struct {
		workers, capacity int
	}{
		{0, 1},
		{1, 0},
		{-1, 1},
	} {
		t.Run(fmt.Sprintf("workers=%d_capacity=%d", tt.workers, tt.capacity), func(t *testing.T) {
			if _, err := NewPool(store, sender, tt.workers, tt.capacity); err == nil {
				t.Fatal("invalid pool configuration accepted")
			}
		})
	}
}
