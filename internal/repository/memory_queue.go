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
	now := time.Now().UTC()
	var selected domain.Event
	found := false
	for id, event := range s.events {
		ready := event.Status == domain.DeliveryStatusPending ||
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
	selected.Status = domain.DeliveryStatusProcessing
	s.events[selected.ID] = selected
	s.versions[selected.ID]++
	s.leases[selected.ID] = now.Add(lease)
	selected.Payload = bytes.Clone(selected.Payload)
	return DeliveryJob{Event: selected, LeaseVersion: s.versions[selected.ID], LeaseUntil: s.leases[selected.ID]}, nil
}

func (s *MemoryStore) FinishEvent(ctx context.Context, job DeliveryJob, status domain.DeliveryStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if status != domain.DeliveryStatusDelivered && status != domain.DeliveryStatusFailed {
		return ErrInvalidTransition
	}
	event, ok := s.events[job.Event.ID]
	if !ok || event.Status != domain.DeliveryStatusProcessing ||
		s.versions[event.ID] != job.LeaseVersion || !s.leases[event.ID].After(time.Now()) {
		return ErrLeaseLost
	}
	event.Status = status
	s.events[event.ID] = event
	delete(s.leases, event.ID)
	return nil
}
