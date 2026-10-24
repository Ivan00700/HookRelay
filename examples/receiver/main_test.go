package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hookrelay/internal/delivery"
	"hookrelay/internal/domain"
	"hookrelay/internal/signature"
)

func TestReceiverChecksSignature(t *testing.T) {
	secret := "receiver-secret"
	body := `{"message":"Привет","order_id":42}`
	valid := signature.Sign([]byte(body), secret)
	tests := []struct {
		name   string
		body   string
		values []string
		status int
	}{
		{"valid", body, []string{valid}, http.StatusNoContent},
		{"missing", body, nil, http.StatusUnauthorized},
		{"wrong secret", body, []string{signature.Sign([]byte(body), "other-secret")}, http.StatusUnauthorized},
		{"changed body", `{"message":"Привет","order_id":43}`, []string{valid}, http.StatusUnauthorized},
		{"changed whitespace", body + "\n", []string{valid}, http.StatusUnauthorized},
		{"malformed", body, []string{"sha256=invalid"}, http.StatusUnauthorized},
		{"duplicate headers", body, []string{valid, valid}, http.StatusUnauthorized},
		{"combined headers", body, []string{valid + ", " + valid}, http.StatusUnauthorized},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(test.body))
			request.Header.Set("X-HookRelay-Event-ID", "event-1")
			for _, value := range test.values {
				request.Header.Add(signature.Header, value)
			}
			response := httptest.NewRecorder()
			newHandler(secret).ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status=%d, want %d", response.Code, test.status)
			}
		})
	}
}

type brokenBody struct{}

func (brokenBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestReceiverBodyErrors(t *testing.T) {
	for _, test := range []struct {
		name   string
		body   io.Reader
		status int
	}{
		{"too large", strings.NewReader(strings.Repeat("x", (1<<20)+1)), http.StatusRequestEntityTooLarge},
		{"read error", brokenBody{}, http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/webhook", test.body)
			response := httptest.NewRecorder()
			newHandler("receiver-secret").ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status=%d, want %d", response.Code, test.status)
			}
		})
	}
}

func TestSenderAndReceiver(t *testing.T) {
	server := httptest.NewServer(newHandler("receiver-secret"))
	defer server.Close()
	sender := delivery.NewSender(server.Client())
	event := domain.Event{ID: "event-1", Payload: json.RawMessage(`{ "message": "Привет", "id": 42 }`)}
	for _, test := range []struct {
		secret string
		status int
	}{
		{"receiver-secret", http.StatusNoContent},
		{"wrong-secret", http.StatusUnauthorized},
	} {
		status, err := sender.Send(context.Background(), event, domain.Endpoint{URL: server.URL + "/webhook", Secret: test.secret})
		if status != test.status || (err != nil) != (test.status != http.StatusNoContent) {
			t.Fatalf("status=%d error=%v, want %d", status, err, test.status)
		}
	}
}
