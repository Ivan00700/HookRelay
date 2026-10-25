package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"hookrelay/internal/domain"
)

func TestRunReportsOnlyOwnEvents(t *testing.T) {
	for _, reject := range []bool{false, true} {
		t.Run(fmt.Sprintf("reject=%t", reject), func(t *testing.T) {
			var mu sync.Mutex
			events := []domain.Event{{ID: "other", Status: domain.DeliveryStatusFailed}}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method + " " + r.URL.Path {
				case "POST /api/v1/endpoints":
					var input domain.CreateEndpointInput
					if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.Secret != "private" || input.URL != "http://receiver.test/webhook" {
						t.Error("invalid endpoint request")
					}
					w.WriteHeader(http.StatusCreated)
					json.NewEncoder(w).Encode(domain.Endpoint{ID: "endpoint"})
				case "POST /api/v1/events":
					if reject {
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					var input domain.CreateEventInput
					if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.EndpointID != "endpoint" || input.Type != "load.test" || !json.Valid(input.Payload) {
						t.Error("invalid event request")
					}
					mu.Lock()
					defer mu.Unlock()
					event := domain.Event{ID: fmt.Sprint(len(events)), Status: domain.DeliveryStatusDelivered}
					events = append(events, event)
					w.WriteHeader(http.StatusCreated)
					json.NewEncoder(w).Encode(event)
				case "GET /api/v1/events":
					mu.Lock()
					defer mu.Unlock()
					json.NewEncoder(w).Encode(events)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			config := options{server.URL, "http://receiver.test/webhook", "private", 12, 3, time.Second}
			ctx, cancel := context.WithTimeout(context.Background(), config.timeout)
			defer cancel()
			report, err := run(ctx, server.Client(), config)
			if reject {
				if err == nil || report.Rejected != 12 || report.Accepted != 0 {
					t.Fatalf("rejected report=%+v error=%v", report, err)
				}
			} else if err != nil || report.Accepted != 12 || report.Delivered != 12 || report.Failed != 0 || report.Unfinished != 0 {
				t.Fatalf("delivered report=%+v error=%v", report, err)
			}
		})
	}
}

func TestRunRejectsInvalidOptions(t *testing.T) {
	config := options{"http://localhost", "http://receiver.test", "secret", 0, 1, time.Second}
	if _, err := run(context.Background(), http.DefaultClient, config); err == nil {
		t.Fatal("zero event count accepted")
	}
}

func TestRunStopsWaitingOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/endpoints":
			json.NewEncoder(w).Encode(domain.Endpoint{ID: "endpoint"})
		case "/api/v1/events":
			if r.Method == http.MethodPost {
				json.NewEncoder(w).Encode(domain.Event{ID: "event"})
				return
			}
			json.NewEncoder(w).Encode([]domain.Event{{ID: "event", Status: domain.DeliveryStatusPending}})
			// отменяем ожидание после приёма события - статус остаётся pending
			cancel()
		}
	}))
	defer server.Close()
	config := options{server.URL, "http://receiver.test", "secret", 1, 1, time.Second}
	report, err := run(ctx, server.Client(), config)
	if err == nil || report.Accepted != 1 || report.Unfinished != 1 || report.Delivered != 0 {
		t.Fatalf("unfinished report=%+v error=%v", report, err)
	}
}
