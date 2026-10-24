package repository

import (
	"bytes"
	"context"
	"sort"
	"time"

	"hookrelay/internal/domain"
)

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
