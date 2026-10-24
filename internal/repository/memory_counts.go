package repository

import (
	"context"

	"hookrelay/internal/domain"
)

func (s *MemoryStore) CheckReady(ctx context.Context) error {
	return ctx.Err()
}

func (s *MemoryStore) EventCounts(ctx context.Context) (map[domain.DeliveryStatus]int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	counts := make(map[domain.DeliveryStatus]int64)
	for _, event := range s.events {
		counts[event.Status]++
	}
	return counts, nil
}
