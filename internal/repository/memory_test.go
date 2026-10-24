package repository

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"hookrelay/internal/domain"
)

func TestMemoryStoreEndpoints(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*testing.T, *MemoryStore)
		act     func(*MemoryStore) error
		wantErr error
	}{
		{
			name: "create endpoint",
			act: func(s *MemoryStore) error {
				return s.CreateEndpoint(context.Background(), domain.Endpoint{ID: "ep-1"})
			},
		},
		{
			name: "duplicate ID",
			prepare: func(t *testing.T, s *MemoryStore) {
				if err := s.CreateEndpoint(context.Background(), domain.Endpoint{ID: "ep-1"}); err != nil {
					t.Fatal(err)
				}
			},
			act: func(s *MemoryStore) error {
				return s.CreateEndpoint(context.Background(), domain.Endpoint{ID: "ep-1"})
			},
			wantErr: ErrConflict,
		},
		{
			name: "delete missing endpoint",
			act: func(s *MemoryStore) error {
				return s.DeleteEndpoint(context.Background(), "missing")
			},
			wantErr: ErrNotFound,
		},
		{
			name: "delete endpoint with event",
			prepare: func(t *testing.T, s *MemoryStore) {
				if err := s.CreateEndpoint(context.Background(), domain.Endpoint{ID: "ep-1"}); err != nil {
					t.Fatal(err)
				}
				if err := s.CreateEvent(context.Background(), domain.Event{ID: "ev-1", EndpointID: "ep-1"}); err != nil {
					t.Fatal(err)
				}
			},
			act: func(s *MemoryStore) error {
				return s.DeleteEndpoint(context.Background(), "ep-1")
			},
			wantErr: ErrConflict,
		},
		{
			name: "delete endpoint without events",
			prepare: func(t *testing.T, s *MemoryStore) {
				if err := s.CreateEndpoint(context.Background(), domain.Endpoint{ID: "ep-1"}); err != nil {
					t.Fatal(err)
				}
			},
			act: func(s *MemoryStore) error {
				return s.DeleteEndpoint(context.Background(), "ep-1")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewMemoryStore()
			if tt.prepare != nil {
				tt.prepare(t, s)
			}
			if err := tt.act(s); !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestMemoryStoreEndpointReads(t *testing.T) {
	s := NewMemoryStore()
	if got, err := s.ListEndpoints(context.Background()); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty list = %#v, want non-nil empty slice", got)
	}
	if _, err := s.GetEndpoint(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatal("missing endpoint was found")
	}

	for _, id := range []string{"ep-b", "ep-a"} {
		if err := s.CreateEndpoint(context.Background(), domain.Endpoint{ID: id}); err != nil {
			t.Fatal(err)
		}
	}
	if endpoint, err := s.GetEndpoint(context.Background(), "ep-b"); err != nil || endpoint.ID != "ep-b" {
		t.Fatalf("GetEndpoint = %#v, %v", endpoint, err)
	}
	list, err := s.ListEndpoints(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != "ep-a" || list[1].ID != "ep-b" {
		t.Fatalf("ListEndpoints = %#v, want IDs ep-a, ep-b", list)
	}
	if err := s.DeleteEndpoint(context.Background(), "ep-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetEndpoint(context.Background(), "ep-a"); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted endpoint was found")
	}
}

func TestMemoryStoreEvents(t *testing.T) {
	s := NewMemoryStore()
	if got, err := s.ListEvents(context.Background()); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty list = %#v, want non-nil empty slice", got)
	}
	if _, err := s.GetEvent(context.Background(), "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatal("missing event was found")
	}
	if err := s.CreateEvent(context.Background(), domain.Event{ID: "ev-a", EndpointID: "missing"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("CreateEvent with missing endpoint = %v, want ErrNotFound", err)
	}
	if err := s.CreateEndpoint(context.Background(), domain.Endpoint{ID: "ep-1"}); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"ev-b", "ev-a"} {
		if err := s.CreateEvent(context.Background(), domain.Event{ID: id, EndpointID: "ep-1"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.CreateEvent(context.Background(), domain.Event{ID: "ev-a", EndpointID: "ep-1"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate event error = %v, want ErrConflict", err)
	}
	if event, err := s.GetEvent(context.Background(), "ev-a"); err != nil || event.ID != "ev-a" {
		t.Fatalf("GetEvent = %#v, %v", event, err)
	}
	list, err := s.ListEvents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != "ev-a" || list[1].ID != "ev-b" {
		t.Fatalf("ListEvents = %#v, want IDs ev-a, ev-b", list)
	}
}

func TestMemoryStoreCopiesEventPayload(t *testing.T) {
	s := NewMemoryStore()
	if err := s.CreateEndpoint(context.Background(), domain.Endpoint{ID: "ep-1"}); err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"value":1}`)
	if err := s.CreateEvent(context.Background(), domain.Event{ID: "ev-1", EndpointID: "ep-1", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	want := []byte(`{"value":1}`)

	payload[9] = '9' // изменяем исходный срез после сохранения
	first, err := s.GetEvent(context.Background(), "ev-1")
	if err != nil || !bytes.Equal(first.Payload, want) {
		t.Fatalf("saved payload = %s, want %s", first.Payload, want)
	}

	first.Payload[9] = '8' // изменяем результат GetEvent
	list, err := s.ListEvents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(list[0].Payload, want) {
		t.Fatalf("listed payload = %s, want %s", list[0].Payload, want)
	}
	list[0].Payload[9] = '7' // изменяем результат ListEvents
	last, _ := s.GetEvent(context.Background(), "ev-1")
	if !bytes.Equal(last.Payload, want) {
		t.Fatalf("stored payload changed to %s", last.Payload)
	}
}

func TestMemoryStoreEventStatusTransitions(t *testing.T) {
	s := NewMemoryStore()
	if err := s.CreateEndpoint(context.Background(), domain.Endpoint{ID: "ep-1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateEvent(context.Background(), domain.Event{ID: "ev-1", EndpointID: "ep-1", Status: domain.DeliveryStatusPending}); err != nil {
		t.Fatal(err)
	}

	if err := s.UpdateEventStatus(context.Background(), "missing", domain.DeliveryStatusProcessing); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing event: got %v, want ErrNotFound", err)
	}
	if err := s.UpdateEventStatus(context.Background(), "ev-1", domain.DeliveryStatusDelivered); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("pending to delivered: got %v, want ErrInvalidTransition", err)
	}
	if err := s.UpdateEventStatus(context.Background(), "ev-1", domain.DeliveryStatusProcessing); err != nil {
		t.Fatalf("pending to processing: %v", err)
	}
	if err := s.UpdateEventStatus(context.Background(), "ev-1", domain.DeliveryStatusFailed); err != nil {
		t.Fatalf("processing to failed: %v", err)
	}
	if err := s.UpdateEventStatus(context.Background(), "ev-1", domain.DeliveryStatusProcessing); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("failed to processing: got %v, want ErrInvalidTransition", err)
	}
	if event, err := s.GetEvent(context.Background(), "ev-1"); err != nil || event.Status != domain.DeliveryStatusFailed {
		t.Fatalf("event = %+v, error = %v; want failed", event, err)
	}
}

func TestMemoryStoreOnlyOneWorkerCanStartEvent(t *testing.T) {
	s := NewMemoryStore()
	if err := s.CreateEndpoint(context.Background(), domain.Endpoint{ID: "ep-1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateEvent(context.Background(), domain.Event{ID: "ev-1", EndpointID: "ep-1", Status: domain.DeliveryStatusPending}); err != nil {
		t.Fatal(err)
	}

	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			results <- s.UpdateEventStatus(context.Background(), "ev-1", domain.DeliveryStatusProcessing)
		}()
	}
	var successful, rejected int
	for i := 0; i < 2; i++ {
		switch err := <-results; {
		case err == nil:
			successful++
		case errors.Is(err, ErrInvalidTransition):
			rejected++
		default:
			t.Fatalf("unexpected transition error: %v", err)
		}
	}
	if successful != 1 || rejected != 1 {
		t.Fatalf("successes = %d, rejected = %d; want 1 and 1", successful, rejected)
	}
}

func TestMemoryStoreConcurrentAccess(t *testing.T) {
	s := NewMemoryStore()
	var wg sync.WaitGroup
	errs := make(chan error, 32)

	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("ep-%d", i)
			if err := s.CreateEndpoint(context.Background(), domain.Endpoint{ID: id}); err != nil {
				errs <- err
				return
			}
			if _, err := s.GetEndpoint(context.Background(), id); err != nil {
				errs <- fmt.Errorf("endpoint %s not found", id)
			}
			_, _ = s.ListEndpoints(context.Background())
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	list, err := s.ListEndpoints(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := len(list); got != 32 {
		t.Fatalf("endpoint count = %d, want 32", got)
	}
}
