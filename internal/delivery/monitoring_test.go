package delivery

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hookrelay/internal/domain"
	"hookrelay/internal/observability"
	"hookrelay/internal/repository"
)

func TestPoolRecordsSavedDeliveryResults(t *testing.T) {
	store := poolStore(t, "ok", "fail")
	var logs bytes.Buffer
	monitor := observability.NewMonitor(observability.NewLogger(&logs), store.EventCounts)
	sender := senderFunc(func(ctx context.Context, event domain.Event, endpoint domain.Endpoint) error {
		if event.ID == "fail" {
			return errors.New("private-endpoint-secret")
		}
		return nil
	})
	pool, err := NewPool(store, sender, 2, monitor)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	waitEvents(t, store, map[string]domain.DeliveryStatus{"ok": domain.DeliveryStatusDelivered, "fail": domain.DeliveryStatusFailed})
	pool.Close()
	response := httptest.NewRecorder()
	monitor.MetricsHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	for _, sample := range []string{`hookrelay_delivery_attempts_total{result="success"} 1`, `hookrelay_delivery_attempts_total{result="error"} 1`, `hookrelay_delivery_results_total{status="delivered"} 1`, `hookrelay_delivery_results_total{status="failed"} 1`, "hookrelay_queue_events 0"} {
		if !strings.Contains(response.Body.String(), sample) {
			t.Fatalf("missing sample %q", sample)
		}
	}
	if strings.Contains(logs.String(), "private-endpoint-secret") || !strings.Contains(logs.String(), `"event_id":"fail"`) || !strings.Contains(logs.String(), `"attempt_number":1`) {
		t.Fatalf("unsafe or uncorrelated delivery logs: %s", logs.String())
	}
}

type lostResultStore struct {
	repository.DeliveryStore
	done chan struct{}
}

func (s *lostResultStore) FinishEvent(ctx context.Context, job repository.DeliveryJob, result domain.DeliveryResult) error {
	close(s.done)
	return repository.ErrLeaseLost
}

func TestPoolDoesNotCountLostLeaseAsDelivery(t *testing.T) {
	store := poolStore(t, "event")
	monitor := observability.NewMonitor(nil, store.EventCounts)
	queue := &lostResultStore{DeliveryStore: store, done: make(chan struct{})}
	pool, err := NewPool(queue, senderFunc(func(context.Context, domain.Event, domain.Endpoint) error { return nil }), 1, monitor)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	select {
	case <-queue.done:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not finish the attempt")
	}
	pool.Close()
	response := httptest.NewRecorder()
	monitor.MetricsHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(response.Body.String(), `hookrelay_delivery_attempts_total{result="success"} 0`) || !strings.Contains(response.Body.String(), `hookrelay_delivery_results_total{status="delivered"} 0`) {
		t.Fatal("an uncommitted delivery was counted")
	}
}
