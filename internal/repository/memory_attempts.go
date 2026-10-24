package repository

import (
	"bytes"
	"context"
	"time"

	"hookrelay/internal/domain"
)

func (s *MemoryStore) ListAttempts(ctx context.Context, eventID string) ([]domain.DeliveryAttempt, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, ok := s.events[eventID]; !ok {
		return nil, ErrNotFound
	}
	attempts := make([]domain.DeliveryAttempt, 0, len(s.attempts[eventID]))
	// Копируем значения указателей, чтобы вызывающий код не изменил хранилище.
	for _, attempt := range s.attempts[eventID] {
		if attempt.FinishedAt != nil {
			finished := *attempt.FinishedAt
			attempt.FinishedAt = &finished
		}
		if attempt.HTTPStatus != nil {
			code := *attempt.HTTPStatus
			attempt.HTTPStatus = &code
		}
		attempts = append(attempts, attempt)
	}
	return attempts, nil
}

func (s *MemoryStore) RetryEvent(ctx context.Context, id string) (domain.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return domain.Event{}, err
	}
	event, ok := s.events[id]
	if !ok {
		return domain.Event{}, ErrNotFound
	}
	if event.Status != domain.DeliveryStatusFailed {
		return domain.Event{}, ErrConflict
	}
	// Начинаем новый цикл, сохраняя историю и её общую нумерацию.
	event.Status = domain.DeliveryStatusPending
	s.events[id] = event
	s.cycles[id] = 0
	s.next[id] = time.Now().UTC()
	delete(s.leases, id)
	event.Payload = bytes.Clone(event.Payload)
	return event, nil
}
