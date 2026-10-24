package delivery

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"hookrelay/internal/domain"
	"hookrelay/internal/postgres"
	"hookrelay/internal/repository"
	"hookrelay/internal/signature"
	"hookrelay/internal/testutil"
)

func TestPoolRecoversWhenSuccessfulDeliveryCannotBeSavedIntegration(t *testing.T) {
	database, databaseURL := testutil.Database(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	control, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer control.Close()
	store := repository.NewPostgresStore(database)
	var requests atomic.Int32
	accepted := make(chan struct{})
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil || !signature.Verify(body, "retry-secret", r.Header.Get(signature.Header)) {
			t.Error("invalid delivery signature")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("X-HookRelay-Event-ID") != "event" {
			t.Error("recovery changed the event ID")
		}
		if requests.Add(1) == 1 {
			// получатель принял событие - соединения воркера закрываются до записи результата
			database.Close()
			close(accepted)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()
	retryStore(t, store, receiver.URL)
	first, err := NewPool(store, NewSender(receiver.Client()), 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	select {
	case <-accepted:
	case <-ctx.Done():
		t.Fatal("receiver did not accept the first delivery")
	}
	first.Close()

	// отдельное соединение показывает сохранённое состояние после отказа записи
	recovered := repository.NewPostgresStore(control)
	event, err := recovered.GetEvent(ctx, "event")
	if err != nil || event.Status != domain.DeliveryStatusProcessing {
		t.Fatalf("unsaved result must stay processing: %+v, %v", event, err)
	}
	attempts, err := recovered.ListAttempts(ctx, "event")
	if err != nil || len(attempts) != 1 || attempts[0].FinishedAt != nil || attempts[0].HTTPStatus != nil {
		t.Fatalf("unsaved attempt must stay unfinished: %+v, %v", attempts, err)
	}
	// ускоряем только тестовую аренду - рабочие данные не затрагиваются
	if _, err := control.Exec(ctx, `UPDATE events SET lease_until = clock_timestamp() - interval '1 second' WHERE id = 'event'`); err != nil {
		t.Fatal(err)
	}
	second, err := NewPool(recovered, NewSender(receiver.Client()), 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	waitEvents(t, recovered, map[string]domain.DeliveryStatus{"event": domain.DeliveryStatusDelivered})
	second.Close()
	attempts, err = recovered.ListAttempts(ctx, "event")
	if err != nil || len(attempts) != 2 {
		t.Fatalf("recovered history: %+v, %v", attempts, err)
	}
	if attempts[0].FinishedAt == nil || attempts[0].HTTPStatus != nil || attempts[0].ErrorMessage == "" {
		t.Fatalf("interrupted result was recorded as HTTP success: %+v", attempts[0])
	}
	if attempts[1].AttemptNumber != 2 || attempts[1].FinishedAt == nil || attempts[1].HTTPStatus == nil || *attempts[1].HTTPStatus != http.StatusNoContent || attempts[1].ErrorMessage != "" {
		t.Fatalf("recovered attempt did not succeed: %+v", attempts[1])
	}
	if requests.Load() != 2 {
		t.Fatalf("deliveries=%d, want original and recovered delivery", requests.Load())
	}
}
