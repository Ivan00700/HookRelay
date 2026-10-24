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

// событие и данные текущего владельца аренды
type DeliveryJob struct {
	Event         domain.Event
	LeaseVersion  int64 // увеличивается при каждом захвате задания
	LeaseUntil    time.Time
	AttemptNumber int // общий номер попытки в истории
	CycleAttempt  int // номер попытки после последнего ручного повтора
}

// интерфейс воркера - захват задания и сохранение результата
type DeliveryStore interface {
	ClaimEvent(ctx context.Context, lease time.Duration) (DeliveryJob, error)
	FinishEvent(ctx context.Context, job DeliveryJob, result domain.DeliveryResult) error
	GetEndpoint(ctx context.Context, id string) (domain.Endpoint, error)
}
