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

type Store interface {
	CreateEndpoint(context.Context, domain.Endpoint) error
	GetEndpoint(context.Context, string) (domain.Endpoint, error)
	ListEndpoints(context.Context) ([]domain.Endpoint, error)
	DeleteEndpoint(context.Context, string) error
	CreateEvent(context.Context, domain.Event) error
	DeleteEvent(context.Context, string) error
	UpdateEventStatus(context.Context, string, domain.DeliveryStatus) error
	GetEvent(context.Context, string) (domain.Event, error)
	ListEvents(context.Context) ([]domain.Event, error)
}
