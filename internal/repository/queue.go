package repository

import (
	"context"
	"errors"
	"time"

	"hookrelay/internal/domain"
)

var (
	ErrNoJob     = errors.New("no events ready for delivery")
	ErrLeaseLost = errors.New("delivery lease is no longer valid")
)

type DeliveryJob struct {
	Event        domain.Event
	LeaseVersion int64
	LeaseUntil   time.Time
}

type DeliveryStore interface {
	ClaimEvent(context.Context, time.Duration) (DeliveryJob, error)
	FinishEvent(context.Context, DeliveryJob, domain.DeliveryStatus) error
	GetEndpoint(context.Context, string) (domain.Endpoint, error)
}
