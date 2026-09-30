package repository

import (
	"errors"

	"hookrelay/internal/domain"
)

var (
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

type Store interface {
	CreateEndpoint(domain.Endpoint) error
	GetEndpoint(id string) (domain.Endpoint, bool)
	ListEndpoints() []domain.Endpoint
	DeleteEndpoint(id string) error
	CreateEvent(domain.Event) error
	GetEvent(id string) (domain.Event, bool)
	ListEvents() []domain.Event
}
