package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
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

type unreadyStore struct {
	repository.Store
	bounded bool
}

func (s *unreadyStore) CheckReady(ctx context.Context) error {
	deadline, ok := ctx.Deadline()
	s.bounded = ok && time.Until(deadline) <= 2*time.Second
	return errors.New("private-database-password")
}

func TestReadinessAndHealth(t *testing.T) {
	store := &unreadyStore{Store: repository.NewMemoryStore()}
	var logs bytes.Buffer
	monitor := observability.NewMonitor(observability.NewLogger(&logs), store.EventCounts)
	mux := http.NewServeMux()
	NewHandler(store, monitor).RegisterRoutes(mux)
	handler := monitor.HTTP(mux)
	for _, test := range []struct {
		path   string
		status int
		body   string
	}{
		{"/ready", http.StatusServiceUnavailable, `{"status":"not_ready"}`},
		{"/health", http.StatusOK, `{"status":"ok"}`},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
		if response.Code != test.status || strings.TrimSpace(response.Body.String()) != test.body || response.Header().Get(observability.RequestIDHeader) == "" {
			t.Fatalf("%s: status=%d body=%s", test.path, response.Code, response.Body.String())
		}
	}
	if !store.bounded || strings.Contains(logs.String(), "private-database-password") {
		t.Fatal("readiness has no timeout or exposed a private error")
	}
}

func TestAcceptedCounterOnlyCountsSavedEvents(t *testing.T) {
	store := repository.NewMemoryStore()
	monitor := observability.NewMonitor(nil, store.EventCounts)
	mux := http.NewServeMux()
	NewHandler(store, monitor).RegisterRoutes(mux)
	handler := monitor.HTTP(mux)
	created := request(mux, http.MethodPost, "/api/v1/endpoints", `{"name":"receiver","url":"http://example.test","secret":"private"}`)
	var endpoint domain.Endpoint
	if created.Code != http.StatusCreated {
		t.Fatalf("endpoint creation: %d", created.Code)
	}
	if err := json.Unmarshal(created.Body.Bytes(), &endpoint); err != nil {
		t.Fatal(err)
	}
	endpointID := endpoint.ID
	for _, test := range []struct {
		body   string
		status int
	}{
		{`{"endpoint_id":"` + endpointID + `","type":"test","payload":{}}`, http.StatusCreated},
		{`{"endpoint_id":"` + endpointID + `","type":"test","payload":[]}`, http.StatusBadRequest},
		{`{"endpoint_id":"unknown","type":"test","payload":{}}`, http.StatusNotFound},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/events", strings.NewReader(test.body)))
		if response.Code != test.status {
			t.Fatalf("event status=%d, want %d", response.Code, test.status)
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "hookrelay_events_accepted_total 1\n") || !strings.Contains(response.Body.String(), "hookrelay_queue_events 1\n") {
		t.Fatalf("incorrect business metrics: %s", response.Body.String())
	}
	ready := request(mux, http.MethodGet, "/ready", "")
	if ready.Code != http.StatusOK || strings.TrimSpace(ready.Body.String()) != `{"status":"ready"}` {
		t.Fatalf("memory store readiness: %d %s", ready.Code, ready.Body.String())
	}
}
