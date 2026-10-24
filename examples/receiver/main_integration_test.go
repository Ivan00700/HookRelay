package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hookrelay/internal/delivery"
	"hookrelay/internal/domain"
	"hookrelay/internal/httpapi"
	"hookrelay/internal/repository"
	"hookrelay/internal/testutil"
)

func TestSignedDeliveryIntegration(t *testing.T) {
	database, _ := testutil.Database(t)
	store := repository.NewPostgresStore(database)
	receiver := httptest.NewServer(newHandler("receiver-secret"))
	defer receiver.Close()
	mux := http.NewServeMux()
	httpapi.NewHandler(store, nil).RegisterRoutes(mux)
	api := httptest.NewServer(mux)
	defer api.Close()
	pool, err := delivery.NewPool(store, delivery.NewSender(receiver.Client()), 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	post := func(path, body string, value any) {
		t.Helper()
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, api.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		response, err := api.Client().Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusCreated {
			t.Fatalf("POST %s: status=%d", path, response.StatusCode)
		}
		if err := json.NewDecoder(response.Body).Decode(value); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		secret string
		status domain.DeliveryStatus
		code   int
	}{
		{"receiver-secret", domain.DeliveryStatusDelivered, http.StatusNoContent},
		{"wrong-secret", domain.DeliveryStatusFailed, http.StatusUnauthorized},
	} {
		endpointBody, err := json.Marshal(domain.CreateEndpointInput{Name: "receiver", URL: receiver.URL + "/webhook", Secret: test.secret})
		if err != nil {
			t.Fatal(err)
		}
		var endpoint domain.Endpoint
		post("/api/v1/endpoints", string(endpointBody), &endpoint)
		if endpoint.Secret != "" {
			t.Fatal("API returned the endpoint secret")
		}
		eventBody, err := json.Marshal(domain.CreateEventInput{EndpointID: endpoint.ID, Type: "order.created", Payload: json.RawMessage(`{ "message": "Привет", "order_id": 42 }`)})
		if err != nil {
			t.Fatal(err)
		}
		var event domain.Event
		post("/api/v1/events", string(eventBody), &event)
		for {
			event, err = store.GetEvent(ctx, event.ID)
			if err != nil {
				t.Fatal(err)
			}
			if event.Status == test.status {
				break
			}
			if event.Status == domain.DeliveryStatusFailed || event.Status == domain.DeliveryStatusDelivered {
				t.Fatalf("event status=%s, want %s", event.Status, test.status)
			}
			time.Sleep(10 * time.Millisecond)
		}
		attempts, err := store.ListAttempts(ctx, event.ID)
		if err != nil || len(attempts) != 1 || attempts[0].HTTPStatus == nil || *attempts[0].HTTPStatus != test.code {
			t.Fatalf("delivery history: %+v, %v", attempts, err)
		}
		if test.status == domain.DeliveryStatusFailed {
			if _, err := store.ClaimEvent(ctx, time.Minute); err != repository.ErrNoJob {
				t.Fatalf("401 was scheduled for retry: %v", err)
			}
		}
	}
}
