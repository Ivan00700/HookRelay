package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hookrelay/internal/domain"
	"hookrelay/internal/observability"
	"hookrelay/internal/postgres"
	"hookrelay/internal/signature"
	"hookrelay/internal/testutil"
)

func TestRunRequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if err := Run(context.Background()); !errors.Is(err, postgres.ErrMissingURL) {
		t.Fatalf("expected missing DATABASE_URL error, got %v", err)
	}
}

func TestRunRejectsMissingQueueMigrationIntegration(t *testing.T) {
	database, databaseURL := testutil.Database(t)
	t.Setenv("DATABASE_URL", databaseURL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := database.Exec(ctx, `ALTER TABLE events DROP COLUMN lease_until`); err != nil {
		t.Fatal(err)
	}
	if err := Run(ctx); err == nil || !strings.Contains(err.Error(), "queue schema") {
		t.Fatalf("expected queue schema startup error, got %v", err)
	}
}

func TestRunPostgresIntegration(t *testing.T) {
	_, databaseURL := testutil.Database(t)
	t.Setenv("DATABASE_URL", databaseURL)
	client := &http.Client{Timeout: time.Second}
	start := func() (string, func()) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := listener.Addr().(*net.TCPAddr).Port
		if err := listener.Close(); err != nil {
			t.Fatal(err)
		}
		t.Setenv("HTTP_PORT", strconv.Itoa(port))
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- Run(ctx) }()
		stop := sync.OnceFunc(func() {
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("server shutdown: %v", err)
				}
			case <-time.After(15 * time.Second):
				t.Error("server did not stop")
			}
		})
		t.Cleanup(stop)
		base := fmt.Sprintf("http://127.0.0.1:%d", port)
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			response, err := client.Get(base + "/health")
			if err == nil {
				response.Body.Close()
				if response.StatusCode == http.StatusOK {
					return base, stop
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("HTTP server did not become ready")
		return "", stop
	}
	exchange := func(base, method, path, body string, wantStatus int) []byte {
		t.Helper()
		req, err := http.NewRequest(method, base+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.Header.Get(observability.RequestIDHeader) == "" {
			t.Fatal("API response is missing a request ID")
		}
		data, err := io.ReadAll(response.Body)
		if err != nil || response.StatusCode != wantStatus {
			t.Fatalf("%s %s: status=%d body=%s err=%v", method, path, response.StatusCode, data, err)
		}
		return data
	}
	var deliveries atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil || !signature.Verify(body, "private", r.Header.Get(signature.Header)) {
			t.Error("signature from the stored endpoint does not match the delivered body")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		deliveries.Add(1)
		if r.Header.Get("X-HookRelay-Event-ID") == "" {
			t.Error("delivery event ID is missing")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()
	var acceptRetry atomic.Bool
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil || !signature.Verify(body, "private", r.Header.Get(signature.Header)) {
			t.Error("manual retry has an invalid signature")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if acceptRetry.Load() {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer failing.Close()

	base, stop := start()
	// проверяем страницу и API на одном сервере
	for path, expected := range map[string]string{
		"/":               "Доставка событий",
		"/assets/app.js":  "/api/v1/events",
		"/assets/app.css": ".container",
	} {
		response, err := client.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != http.StatusOK || !strings.Contains(string(body), expected) {
			t.Fatalf("dashboard %s: status=%d error=%v", path, response.StatusCode, err)
		}
	}
	var savedEndpoint domain.Endpoint
	var savedEvents []domain.Event
	for _, tc := range []struct {
		url    string
		status domain.DeliveryStatus
	}{
		{receiver.URL, domain.DeliveryStatusDelivered},
		{failing.URL, domain.DeliveryStatusFailed},
	} {
		var endpoint domain.Endpoint
		data := exchange(base, http.MethodPost, "/api/v1/endpoints", fmt.Sprintf(`{"name":"orders","url":%q,"secret":"private"}`, tc.url), http.StatusCreated)
		if strings.Contains(string(data), "private") {
			t.Fatal("endpoint response exposes its secret")
		}
		if err := json.Unmarshal(data, &endpoint); err != nil {
			t.Fatal(err)
		}
		savedEndpoint = endpoint
		var event domain.Event
		data = exchange(base, http.MethodPost, "/api/v1/events", fmt.Sprintf(`{"endpoint_id":%q,"type":"order.created","payload":{"order_id":42}}`, endpoint.ID), http.StatusCreated)
		if err := json.Unmarshal(data, &event); err != nil {
			t.Fatal(err)
		}
		createdAt := event.CreatedAt
		deadline := time.Now().Add(5 * time.Second)
		for event.Status != tc.status && time.Now().Before(deadline) {
			data = exchange(base, http.MethodGet, "/api/v1/events/"+event.ID, "", http.StatusOK)
			if err := json.Unmarshal(data, &event); err != nil {
				t.Fatal(err)
			}
			time.Sleep(10 * time.Millisecond)
		}
		if event.Status != tc.status || !event.CreatedAt.Equal(createdAt) {
			t.Fatalf("event status=%s, want %s; created_at=%v, want %v", event.Status, tc.status, event.CreatedAt, createdAt)
		}
		savedEvents = append(savedEvents, event)
		exchange(base, http.MethodDelete, "/api/v1/endpoints/"+endpoint.ID, "", http.StatusConflict)
	}
	// счётчики относятся к процессу - очередь и статусы продолжают храниться в БД
	metrics := string(exchange(base, http.MethodGet, "/metrics", "", http.StatusOK))
	if !strings.Contains(metrics, "hookrelay_events_accepted_total 2") || !strings.Contains(metrics, "hookrelay_queue_events 0") {
		t.Fatalf("unexpected metrics before restart: %s", metrics)
	}
	exchange(base, http.MethodGet, "/ready", "", http.StatusOK)
	stop()

	base, _ = start()
	metrics = string(exchange(base, http.MethodGet, "/metrics", "", http.StatusOK))
	if !strings.Contains(metrics, "hookrelay_events_accepted_total 0") || !strings.Contains(metrics, `hookrelay_events{status="failed"} 1`) {
		t.Fatalf("process counters or persisted states after restart: %s", metrics)
	}
	exchange(base, http.MethodGet, "/api/v1/endpoints/"+savedEndpoint.ID, "", http.StatusOK)
	for _, expected := range savedEvents {
		data := exchange(base, http.MethodGet, "/api/v1/events/"+expected.ID, "", http.StatusOK)
		var got domain.Event
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		if got.Status != expected.Status || got.EndpointID != expected.EndpointID || got.Type != expected.Type || !got.CreatedAt.Equal(expected.CreatedAt) {
			t.Fatalf("event changed after restart: %+v", got)
		}
		var payload map[string]int
		if err := json.Unmarshal(got.Payload, &payload); err != nil || payload["order_id"] != 42 {
			t.Fatalf("payload after restart: %s, %v", got.Payload, err)
		}
	}
	if deliveries.Load() != 1 {
		t.Fatalf("successful deliveries=%d, want 1", deliveries.Load())
	}
	acceptRetry.Store(true)
	failed := savedEvents[len(savedEvents)-1]
	exchange(base, http.MethodPost, "/api/v1/events/"+failed.ID+"/retry", "", http.StatusAccepted)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data := exchange(base, http.MethodGet, "/api/v1/events/"+failed.ID, "", http.StatusOK)
		if err := json.Unmarshal(data, &failed); err != nil {
			t.Fatal(err)
		}
		if failed.Status == domain.DeliveryStatusDelivered {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if failed.Status != domain.DeliveryStatusDelivered {
		t.Fatal("manual retry was not delivered")
	}
	data := exchange(base, http.MethodGet, "/api/v1/events/"+failed.ID+"/attempts", "", http.StatusOK)
	var attempts []domain.DeliveryAttempt
	if err := json.Unmarshal(data, &attempts); err != nil || len(attempts) != 2 || attempts[0].HTTPStatus == nil || *attempts[0].HTTPStatus != 400 || attempts[1].HTTPStatus == nil || *attempts[1].HTTPStatus != 204 {
		t.Fatalf("manual retry HTTP history: %s, %v", data, err)
	}
	exchange(base, http.MethodPost, "/api/v1/events/"+failed.ID+"/retry", "", http.StatusConflict)
}
