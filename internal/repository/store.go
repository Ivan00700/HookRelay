package repository

import (
	"errors"

	"hookrelay/internal/domain"
)

var (
	ErrNotFound          = errors.New("not found")
	ErrConflict          = errors.New("conflict")
	ErrInvalidTransition = errors.New("invalid status transition")
)

type Store interface {
	CreateEndpoint(domain.Endpoint) error
	GetEndpoint(id string) (domain.Endpoint, bool)
	ListEndpoints() []domain.Endpoint
	DeleteEndpoint(id string) error
	CreateEvent(domain.Event) error
	DeleteEvent(id string) error
	UpdateEventStatus(id string, status domain.DeliveryStatus) error
	GetEvent(id string) (domain.Event, bool)
	ListEvents() []domain.Event
}
