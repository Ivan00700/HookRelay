package delivery

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"hookrelay/internal/domain"
	"hookrelay/internal/repository"
)

const (
	pollInterval  = 250 * time.Millisecond
	leaseDuration = 30 * time.Second
	jobTimeout    = 10 * time.Second
)

type EventSender interface {
	Send(context.Context, domain.Event, domain.Endpoint) error
}

type Pool struct {
	store  repository.DeliveryStore
	sender EventSender
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewPool(store repository.DeliveryStore, sender EventSender, workers int) (*Pool, error) {
	if store == nil || sender == nil || workers <= 0 {
		return nil, errors.New("store, sender and a positive worker count are required")
	}
	ctx, cancel := context.WithCancel(context.Background())
	pool := &Pool{store: store, sender: sender, ctx: ctx, cancel: cancel}
	pool.wg.Add(workers)
	for i := 0; i < workers; i++ {
		go pool.worker()
	}
	return pool, nil
}

// Close stops claiming jobs and waits for deliveries already in progress.
func (p *Pool) Close() {
	p.cancel()
	p.wg.Wait()
}

func (p *Pool) worker() {
	defer p.wg.Done()
	for p.ctx.Err() == nil {
		ctx, cancel := context.WithTimeout(p.ctx, 5*time.Second)
		job, err := p.store.ClaimEvent(ctx, leaseDuration)
		cancel()
		if err == nil {
			p.deliver(job)
			continue
		}
		if p.ctx.Err() != nil {
			return
		}
		if !errors.Is(err, repository.ErrNoJob) {
			log.Printf("claim delivery: %v", err)
		}
		timer := time.NewTimer(pollInterval)
		select {
		case <-p.ctx.Done():
		case <-timer.C:
		}
		timer.Stop()
	}
}

func (p *Pool) deliver(job repository.DeliveryJob) {
	ctx, cancel := context.WithTimeout(context.Background(), jobTimeout)
	defer cancel()
	endpoint, err := p.store.GetEndpoint(ctx, job.Event.EndpointID)
	if err != nil {
		log.Printf("get endpoint for event %s: %v", job.Event.ID, err)
		return
	}
	status := domain.DeliveryStatusDelivered
	if err := p.sender.Send(ctx, job.Event, endpoint); err != nil {
		log.Printf("delivery of event %s failed: %v", job.Event.ID, err)
		status = domain.DeliveryStatusFailed
	}
	finishCtx, finishCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer finishCancel()
	if err := p.store.FinishEvent(finishCtx, job, status); err != nil {
		log.Printf("finish delivery of event %s: %v", job.Event.ID, err)
	}
}
