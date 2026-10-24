package repository

import (
	"bytes"
	"context"
	"sort"
	"sync"
	"time"

	"hookrelay/internal/domain"
)

type MemoryStore struct {
	mu            sync.RWMutex
	endpoints     map[string]domain.Endpoint
	events        map[string]domain.Event
	leases        map[string]time.Time
	versions      map[string]int64
	attempts      map[string][]domain.DeliveryAttempt
	cycles        map[string]int
	next          map[string]time.Time
	lastAttemptID int64
}

var _ Store = (*MemoryStore)(nil)

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		endpoints: make(map[string]domain.Endpoint),
		events:    make(map[string]domain.Event),
		leases:    make(map[string]time.Time),
		versions:  make(map[string]int64),
		attempts:  make(map[string][]domain.DeliveryAttempt),
		cycles:    make(map[string]int),
		next:      make(map[string]time.Time),
	}
}

func (s *MemoryStore) GetEndpoint(ctx context.Context, id string) (domain.Endpoint, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return domain.Endpoint{}, err
	}

	endpoint, ok := s.endpoints[id]
	if !ok {
		return domain.Endpoint{}, ErrNotFound
	}
	return endpoint, nil
}

func (s *MemoryStore) CreateEndpoint(ctx context.Context, endpoint domain.Endpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	if _, ok := s.endpoints[endpoint.ID]; ok {
		return ErrConflict
	}

	s.endpoints[endpoint.ID] = endpoint
	return nil
}

func (s *MemoryStore) ListEndpoints(ctx context.Context) ([]domain.Endpoint, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	endpoints := make([]domain.Endpoint, 0, len(s.endpoints))
	for _, endpoint := range s.endpoints {
		endpoints = append(endpoints, endpoint)
	}
	s.mu.RUnlock()

	sort.Slice(endpoints, func(i, j int) bool {
		return endpoints[i].ID < endpoints[j].ID
	})
	return endpoints, nil
}

func (s *MemoryStore) DeleteEndpoint(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	if _, ok := s.endpoints[id]; !ok {
		return ErrNotFound
	}

	for _, event := range s.events {
		if event.EndpointID == id {
			return ErrConflict
		}
	}

	delete(s.endpoints, id)
	return nil
}

func (s *MemoryStore) CreateEvent(ctx context.Context, event domain.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	if _, ok := s.endpoints[event.EndpointID]; !ok {
		return ErrNotFound
	}
	if _, ok := s.events[event.ID]; ok {
		return ErrConflict
	}

	event.Payload = bytes.Clone(event.Payload)
	s.events[event.ID] = event
	s.next[event.ID] = time.Now().UTC()
	return nil
}

func (s *MemoryStore) DeleteEvent(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, ok := s.events[id]; !ok {
		return ErrNotFound
	}
	if len(s.attempts[id]) > 0 {
		return ErrConflict
	}
	delete(s.events, id)
	delete(s.leases, id)
	delete(s.cycles, id)
	delete(s.next, id)
	return nil
}

func (s *MemoryStore) UpdateEventStatus(ctx context.Context, id string, status domain.DeliveryStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}

	event, ok := s.events[id]
	if !ok {
		return ErrNotFound
	}
	if _, leased := s.leases[id]; leased {
		return ErrLeaseLost
	}
	if status == domain.DeliveryStatusPending {
		return ErrInvalidTransition
	}
	if !domain.CanTransition(event.Status, status) {
		return ErrInvalidTransition
	}
	event.Status = status
	s.events[id] = event
	return nil
}

func (s *MemoryStore) GetEvent(ctx context.Context, id string) (domain.Event, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return domain.Event{}, err
	}

	event, ok := s.events[id]
	if !ok {
		return domain.Event{}, ErrNotFound
	}
	event.Payload = bytes.Clone(event.Payload)
	return event, nil
}

func (s *MemoryStore) ListEvents(ctx context.Context) ([]domain.Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	events := make([]domain.Event, 0, len(s.events))
	for _, event := range s.events {
		event.Payload = bytes.Clone(event.Payload)
		events = append(events, event)
	}
	s.mu.RUnlock()

	sort.Slice(events, func(i, j int) bool {
		return events[i].ID < events[j].ID
	})
	return events, nil
}
