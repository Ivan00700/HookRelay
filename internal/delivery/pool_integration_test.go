package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hookrelay/internal/domain"
	"hookrelay/internal/postgres"
	"hookrelay/internal/repository"
	"hookrelay/internal/testutil"
)

func TestPoolsShareQueueIntegration(t *testing.T) {
	database, _ := testutil.Database(t)
	store := repository.NewPostgresStore(database)
	ctx := context.Background()
	if err := store.CreateEndpoint(ctx, domain.Endpoint{ID: "endpoint", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	expected := make(map[string]domain.DeliveryStatus)
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("event-%02d", i)
		if err := store.CreateEvent(ctx, domain.Event{ID: id, EndpointID: "endpoint", Type: "test", Payload: json.RawMessage(`{}`), Status: domain.DeliveryStatusPending, CreatedAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
		expected[id] = domain.DeliveryStatusDelivered
	}
	counts := make(map[string]int)
	var mu sync.Mutex
	sender := senderFunc(func(_ context.Context, event domain.Event, _ domain.Endpoint) error {
		mu.Lock()
		counts[event.ID]++
		mu.Unlock()
		return nil
	})
	first, err := NewPool(store, sender, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := NewPool(repository.NewPostgresStore(database), sender, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	waitEvents(t, store, expected)
	first.Close()
	second.Close()
	for id := range expected {
		if counts[id] != 1 {
			t.Fatalf("deliveries for %s=%d, want 1", id, counts[id])
		}
	}
}

func TestQueueRecoversAfterProcessCrashIntegration(t *testing.T) {
	database, databaseURL := testutil.Database(t)
	store := repository.NewPostgresStore(database)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	firstRequest := make(chan struct{})
	var requests atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			return
		}
		if requests.Add(1) == 1 {
			close(firstRequest)
			select {
			case <-r.Context().Done():
			case <-ctx.Done():
			}
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()
	if err := store.CreateEndpoint(ctx, domain.Endpoint{ID: "endpoint", URL: receiver.URL, Secret: "crash-secret", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateEvent(ctx, domain.Event{ID: "event", EndpointID: "endpoint", Type: "test", Payload: json.RawMessage(`{}`), Status: domain.DeliveryStatusPending, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.CommandContext(ctx, executable, "-test.run=^TestQueueCrashHelper$")
	child.Env = append(os.Environ(), "HOOKRELAY_CRASH_HELPER=1", "TEST_DATABASE_URL="+databaseURL)
	var output bytes.Buffer
	child.Stdout = &output
	child.Stderr = &output
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	stopChild := sync.OnceFunc(func() {
		_ = child.Process.Kill()
		_ = child.Wait()
	})
	defer stopChild()
	select {
	case <-firstRequest:
	case <-ctx.Done():
		stopChild()
		t.Fatalf("helper did not start delivery: %s", output.String())
	}
	stopChild()
	event, err := store.GetEvent(ctx, "event")
	if err != nil || event.Status != domain.DeliveryStatusProcessing {
		t.Fatalf("interrupted event: %+v, %v; helper output: %s", event, err, output.String())
	}
	if _, err := store.ClaimEvent(ctx, time.Minute); !errors.Is(err, repository.ErrNoJob) {
		t.Fatalf("unexpired lease must remain owned after restart: %v", err)
	}
	// Сдвигаем только тестовую аренду, чтобы не ждать тридцать секунд.
	if _, err := database.Exec(ctx, `UPDATE events SET lease_until = clock_timestamp() - interval '1 second' WHERE id = 'event'`); err != nil {
		t.Fatal(err)
	}
	pool, err := NewPool(store, NewSender(receiver.Client()), 1)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	waitEvents(t, store, map[string]domain.DeliveryStatus{"event": domain.DeliveryStatusDelivered})
	if requests.Load() != 2 {
		t.Fatalf("requests=%d, want interrupted request and recovered request", requests.Load())
	}
	attempts, err := store.ListAttempts(ctx, "event")
	if err != nil || len(attempts) != 2 || attempts[0].FinishedAt == nil || attempts[0].HTTPStatus != nil || attempts[0].ErrorMessage == "" || attempts[1].HTTPStatus == nil || *attempts[1].HTTPStatus != 204 {
		t.Fatalf("history after process crash: %+v, %v", attempts, err)
	}
}

func TestQueueCrashHelper(t *testing.T) {
	if os.Getenv("HOOKRELAY_CRASH_HELPER") != "1" {
		t.Skip("only used by the process crash integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	database, err := postgres.Open(ctx, os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	pool, err := NewPool(repository.NewPostgresStore(database), NewSender(&http.Client{}), 1)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	select {}
}
