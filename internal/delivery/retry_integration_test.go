package delivery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"hookrelay/internal/domain"
	"hookrelay/internal/repository"
	"hookrelay/internal/testutil"
)

func retryStore(t *testing.T, store repository.Store, receiverURL string) {
	t.Helper()
	ctx := context.Background()
	if err := store.CreateEndpoint(ctx, domain.Endpoint{ID: "endpoint", URL: receiverURL, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateEvent(ctx, domain.Event{ID: "event", EndpointID: "endpoint", Type: "test", Payload: json.RawMessage(`{}`), Status: domain.DeliveryStatusPending, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
}

func TestPoolWaitsFiveSecondsBeforeRetryIntegration(t *testing.T) {
	database, _ := testutil.Database(t)
	store := repository.NewPostgresStore(database)
	type requestTime struct {
		at time.Time
		id string
	}
	requests := make(chan requestTime, 4)
	var count atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- requestTime{at: time.Now(), id: r.Header.Get("X-HookRelay-Event-ID")}
		if count.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()
	retryStore(t, store, receiver.URL)
	pool, err := NewPool(store, NewSender(receiver.Client()), 2)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	var times []requestTime
	for len(times) < 2 {
		select {
		case request := <-requests:
			times = append(times, request)
		case <-ctx.Done():
			t.Fatal("retry was not delivered")
		}
	}
	if delay := times[1].at.Sub(times[0].at); delay < 5*time.Second {
		t.Fatalf("retry happened too early: %v", delay)
	}
	if times[0].id != "event" || times[1].id != times[0].id {
		t.Fatal("retry changed the event ID")
	}
	waitEvents(t, store, map[string]domain.DeliveryStatus{"event": domain.DeliveryStatusDelivered})
	pool.Close()
	attempts, err := store.ListAttempts(ctx, "event")
	if err != nil || len(attempts) != 2 || attempts[0].HTTPStatus == nil || *attempts[0].HTTPStatus != 500 || attempts[1].HTTPStatus == nil || *attempts[1].HTTPStatus != 204 {
		t.Fatalf("retry history: %+v, %v", attempts, err)
	}
	if count.Load() != 2 {
		t.Fatalf("requests=%d, want 2", count.Load())
	}
}

func TestPoolExhaustsRetriesIntegration(t *testing.T) {
	database, _ := testutil.Database(t)
	store := repository.NewPostgresStore(database)
	var requests atomic.Int32
	var healthy atomic.Bool
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if healthy.Load() {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer receiver.Close()
	retryStore(t, store, receiver.URL)
	pool, err := NewPool(store, NewSender(receiver.Client()), 2)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for number := 1; number <= domain.MaxDeliveryAttempts; number++ {
		for {
			attempts, err := store.ListAttempts(ctx, "event")
			if err != nil {
				t.Fatal(err)
			}
			if len(attempts) > number {
				t.Fatalf("an extra attempt was made: %+v", attempts)
			}
			if len(attempts) == number && attempts[number-1].FinishedAt != nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if number < domain.MaxDeliveryAttempts {
			// Check the real schedule, then advance only this test event.
			var delay float64
			if err := database.QueryRow(ctx, `
				SELECT EXTRACT(EPOCH FROM e.next_attempt_at - a.finished_at)
				FROM events e JOIN delivery_attempts a ON a.event_id = e.id
				WHERE e.id = 'event' AND a.attempt_number = $1
			`, number).Scan(&delay); err != nil {
				t.Fatal(err)
			}
			if expected := domain.RetryDelay(number).Seconds(); delay < expected || delay > expected+1 {
				t.Fatalf("scheduled delay=%f, want %f", delay, expected)
			}
			if _, err := database.Exec(ctx, `UPDATE events SET next_attempt_at = clock_timestamp() - interval '1 second' WHERE id = 'event'`); err != nil {
				t.Fatal(err)
			}
		}
	}
	waitEvents(t, store, map[string]domain.DeliveryStatus{"event": domain.DeliveryStatusFailed})
	if requests.Load() != 4 {
		t.Fatalf("requests=%d, want 4", requests.Load())
	}
	healthy.Store(true)
	if _, err := store.RetryEvent(ctx, "event"); err != nil {
		t.Fatal(err)
	}
	waitEvents(t, store, map[string]domain.DeliveryStatus{"event": domain.DeliveryStatusDelivered})
	attempts, err := store.ListAttempts(ctx, "event")
	if err != nil || len(attempts) != 5 || attempts[4].AttemptNumber != 5 || requests.Load() != 5 {
		t.Fatalf("manual retry history: %+v, %v", attempts, err)
	}
}
