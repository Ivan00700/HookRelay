package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"hookrelay/internal/domain"
)

func testEvent() domain.Event {
	return domain.Event{
		ID:      "event-123",
		Type:    "order.created",
		Payload: json.RawMessage(`{"order_id":42}`),
	}
}

func TestSenderSendsPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
		if got := r.Header.Get("X-HookRelay-Event-ID"); got != "event-123" {
			t.Errorf("event ID header = %q, want event-123", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		} else if string(body) != `{"order_id":42}` {
			t.Errorf("body = %s, want original JSON", body)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	sender := NewSender(server.Client())
	if err := sender.Send(context.Background(), testEvent(), domain.Endpoint{URL: server.URL}); err != nil {
		t.Fatalf("send: %v", err)
	}
}

func TestSenderRejectsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	err := NewSender(server.Client()).Send(context.Background(), testEvent(), domain.Endpoint{URL: server.URL})
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("error = %v, want HTTP 500 error", err)
	}
}

func TestSenderDoesNotFollowRedirect(t *testing.T) {
	var redirected atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/other" {
			redirected.Store(true)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Redirect(w, r, "/other", http.StatusTemporaryRedirect)
	}))
	defer server.Close()

	err := NewSender(server.Client()).Send(context.Background(), testEvent(), domain.Endpoint{URL: server.URL})
	if err == nil || !strings.Contains(err.Error(), "307") {
		t.Fatalf("error = %v, want HTTP 307 error", err)
	}
	if redirected.Load() {
		t.Fatal("sender followed redirect")
	}
}

func TestSenderRespectsContextDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	err := NewSender(server.Client()).Send(ctx, testEvent(), domain.Endpoint{URL: server.URL})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context deadline exceeded", err)
	}
}

func TestSenderReturnsNetworkError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	sender := NewSender(server.Client())
	endpoint := domain.Endpoint{URL: server.URL}
	server.Close()

	if err := sender.Send(context.Background(), testEvent(), endpoint); err == nil {
		t.Fatal("send to closed server succeeded")
	}
}

func TestSenderRejectsMalformedURL(t *testing.T) {
	err := NewSender(nil).Send(context.Background(), testEvent(), domain.Endpoint{URL: "://invalid"})
	if err == nil || !strings.Contains(err.Error(), "create delivery request") {
		t.Fatalf("error = %v, want request creation error", err)
	}
}
