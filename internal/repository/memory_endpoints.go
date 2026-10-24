package repository

import (
	"context"
	"sort"

	"hookrelay/internal/domain"
)

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
