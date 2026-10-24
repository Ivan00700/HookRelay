package delivery

import (
	"context"
	"errors"
	"sync"
	"time"

	"hookrelay/internal/domain"
	"hookrelay/internal/observability"
	"hookrelay/internal/repository"
)

const (
	pollInterval  = 250 * time.Millisecond
	leaseDuration = 30 * time.Second
	jobTimeout    = 10 * time.Second
)

type EventSender interface {
	Send(ctx context.Context, event domain.Event, endpoint domain.Endpoint) (int, error)
}

// хранит зависимости воркеров и управляет их завершением
type Pool struct {
	store   repository.DeliveryStore
	sender  EventSender
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	monitor *observability.Monitor
}

func NewPool(store repository.DeliveryStore, sender EventSender, workers int, monitor *observability.Monitor) (*Pool, error) {
	if store == nil || sender == nil || workers <= 0 {
		return nil, errors.New("store, sender and a positive worker count are required")
	}
	ctx, cancel := context.WithCancel(context.Background())
	pool := &Pool{
		store:   store,
		sender:  sender,
		ctx:     ctx,
		cancel:  cancel,
		monitor: monitor,
	}
	pool.wg.Add(workers)
	for i := 0; i < workers; i++ {
		go pool.worker()
	}
	return pool, nil
}

// прекращает захват заданий и дожидается уже начатых отправок
func (p *Pool) Close() {
	p.cancel()
	p.wg.Wait()
}
