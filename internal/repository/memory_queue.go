package repository

import (
	"bytes"
	"context"
	"errors"
	"time"

	"hookrelay/internal/domain"
)

var _ DeliveryStore = (*MemoryStore)(nil)

func (s *MemoryStore) ClaimEvent(ctx context.Context, lease time.Duration) (DeliveryJob, error) {
	if lease.Microseconds() <= 0 {
		return DeliveryJob{}, errors.New("lease must be at least one microsecond")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return DeliveryJob{}, err
	}

	// выбираем самое старое готовое событие под общей блокировкой
	now := time.Now().UTC()
	var selected domain.Event
	found := false
	for id, event := range s.events {
		ready := (event.Status == domain.DeliveryStatusPending && !s.next[id].After(now)) ||
			(event.Status == domain.DeliveryStatusProcessing && !s.leases[id].After(now))
		if !ready {
			continue
		}
		if !found || event.CreatedAt.Before(selected.CreatedAt) ||
			(event.CreatedAt.Equal(selected.CreatedAt) && event.ID < selected.ID) {
			selected = event
			found = true
		}
	}
	if !found {
		return DeliveryJob{}, ErrNoJob
	}

	// закрываем прерванную попытку и ограничиваем число захватов
	for i := range s.attempts[selected.ID] {
		attempt := &s.attempts[selected.ID][i]
		if attempt.FinishedAt == nil {
			finished := now
			attempt.FinishedAt = &finished
			attempt.ErrorMessage = "delivery interrupted: lease expired"
		}
	}
	if s.cycles[selected.ID] >= domain.MaxDeliveryAttempts {
		selected.Status = domain.DeliveryStatusFailed
		s.events[selected.ID] = selected
		delete(s.leases, selected.ID)
		return DeliveryJob{}, ErrNoJob
	}

	// сохраняем аренду и начало новой попытки
	selected.Status = domain.DeliveryStatusProcessing
	s.events[selected.ID] = selected
	s.versions[selected.ID]++
	s.leases[selected.ID] = now.Add(lease)
	s.cycles[selected.ID]++
	s.lastAttemptID++
	attemptNumber := len(s.attempts[selected.ID]) + 1
	s.attempts[selected.ID] = append(s.attempts[selected.ID], domain.DeliveryAttempt{
		ID:            s.lastAttemptID,
		EventID:       selected.ID,
		AttemptNumber: attemptNumber,
		StartedAt:     now,
	})
	selected.Payload = bytes.Clone(selected.Payload)
	return DeliveryJob{
		Event:         selected,
		LeaseVersion:  s.versions[selected.ID],
		LeaseUntil:    s.leases[selected.ID],
		AttemptNumber: attemptNumber,
		CycleAttempt:  s.cycles[selected.ID],
	}, nil
}

func (s *MemoryStore) FinishEvent(ctx context.Context, job DeliveryJob, result domain.DeliveryResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if !result.Valid() {
		return ErrInvalidTransition
	}

	// проверяем аренду и наличие незавершённой попытки
	event, ok := s.events[job.Event.ID]
	if !ok || event.Status != domain.DeliveryStatusProcessing ||
		s.versions[event.ID] != job.LeaseVersion || !s.leases[event.ID].After(time.Now()) {
		return ErrLeaseLost
	}
	index := job.AttemptNumber - 1
	if index < 0 || index >= len(s.attempts[event.ID]) || s.attempts[event.ID][index].FinishedAt != nil {
		return ErrLeaseLost
	}

	// обновляем историю и событие под одной блокировкой
	attempt := &s.attempts[event.ID][index]
	finished := time.Now().UTC()
	attempt.FinishedAt = &finished
	attempt.ErrorMessage = result.ErrorMessage
	if result.HTTPStatus != 0 {
		code := result.HTTPStatus
		attempt.HTTPStatus = &code
	}
	status, delay := result.NextStatus(s.cycles[event.ID])
	event.Status = status
	s.events[event.ID] = event
	if status == domain.DeliveryStatusPending {
		s.next[event.ID] = finished.Add(delay)
	}
	delete(s.leases, event.ID)
	return nil
}
