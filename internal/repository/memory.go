package repository

import (
	"bytes"
	"sort"
	"sync"

	"hookrelay/internal/domain"
)

type MemoryStore struct {
	mu        sync.RWMutex
	endpoints map[string]domain.Endpoint
	events    map[string]domain.Event
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		endpoints: make(map[string]domain.Endpoint),
		events:    make(map[string]domain.Event),
	}
}

func (s *MemoryStore) GetEndpoint(id string) (domain.Endpoint, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	endpoint, ok := s.endpoints[id]
	return endpoint, ok
}

func (s *MemoryStore) CreateEndpoint(endpoint domain.Endpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.endpoints[endpoint.ID]; ok {
		return ErrConflict
	}

	s.endpoints[endpoint.ID] = endpoint
	return nil
}

func (s *MemoryStore) ListEndpoints() []domain.Endpoint {
	s.mu.RLock()
	endpoints := make([]domain.Endpoint, 0, len(s.endpoints))
	for _, endpoint := range s.endpoints {
		endpoints = append(endpoints, endpoint)
	}
	s.mu.RUnlock()

	sort.Slice(endpoints, func(i, j int) bool {
		return endpoints[i].ID < endpoints[j].ID
	})
	return endpoints
}

func (s *MemoryStore) DeleteEndpoint(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

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

func (s *MemoryStore) CreateEvent(event domain.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.endpoints[event.EndpointID]; !ok {
		return ErrNotFound
	}
	if _, ok := s.events[event.ID]; ok {
		return ErrConflict
	}

	event.Payload = bytes.Clone(event.Payload)
	s.events[event.ID] = event
	return nil
}

func (s *MemoryStore) GetEvent(id string) (domain.Event, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	event, ok := s.events[id]
	if !ok {
		return domain.Event{}, false
	}
	event.Payload = bytes.Clone(event.Payload)
	return event, true
}

func (s *MemoryStore) ListEvents() []domain.Event {
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
	return events
}
