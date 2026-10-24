package repository

import (
	"context"
	"errors"

	"hookrelay/internal/domain"
)

var (
	ErrNotFound          = errors.New("not found")
	ErrConflict          = errors.New("conflict")
	ErrInvalidTransition = errors.New("invalid status transition")
)

// Store описывает операции с записями независимо от способа хранения.
type Store interface {
	CreateEndpoint(ctx context.Context, endpoint domain.Endpoint) error
	GetEndpoint(ctx context.Context, id string) (domain.Endpoint, error)
	ListEndpoints(ctx context.Context) ([]domain.Endpoint, error)
	DeleteEndpoint(ctx context.Context, id string) error

	CreateEvent(ctx context.Context, event domain.Event) error
	GetEvent(ctx context.Context, id string) (domain.Event, error)
	ListEvents(ctx context.Context) ([]domain.Event, error)
	DeleteEvent(ctx context.Context, id string) error
	UpdateEventStatus(ctx context.Context, id string, status domain.DeliveryStatus) error

	ListAttempts(ctx context.Context, eventID string) ([]domain.DeliveryAttempt, error)
	RetryEvent(ctx context.Context, id string) (domain.Event, error)
}
