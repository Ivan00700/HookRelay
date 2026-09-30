package delivery

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"

	"hookrelay/internal/domain"
	"hookrelay/internal/repository"
)

var ErrClosed = errors.New("delivery pool is closed")

type EventSender interface {
	Send(context.Context, domain.Event, domain.Endpoint) error
}

type Pool struct {
	store  repository.Store
	sender EventSender
	jobs   chan string
	wg     sync.WaitGroup
	mu     sync.Mutex
	closed bool
}

func NewPool(store repository.Store, sender EventSender, workers, queueCapacity int) (*Pool, error) {
	if store == nil || sender == nil || workers <= 0 || queueCapacity <= 0 {
		return nil, errors.New("store, sender, workers and queue capacity must be set")
	}

	pool := &Pool{
		store:  store,
		sender: sender,
		jobs:   make(chan string, queueCapacity),
	}
	pool.wg.Add(workers)
	for i := 0; i < workers; i++ {
		go pool.worker()
	}
	return pool, nil
}

// Submit waits for space in the bounded queue. The lock also prevents a send
// racing with Close. The HTTP server must stop accepting submissions first.
func (p *Pool) Submit(eventID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrClosed
	}
	p.jobs <- eventID
	return nil
}

// Close drains queued jobs and waits for every worker. It is safe to call twice.
func (p *Pool) Close() {
	p.mu.Lock()
	if !p.closed {
		p.closed = true
		close(p.jobs)
	}
	p.mu.Unlock()
	p.wg.Wait()
}

func (p *Pool) worker() {
	defer p.wg.Done()
	for eventID := range p.jobs {
		p.deliver(eventID)
	}
}

func (p *Pool) deliver(eventID string) {
	if err := p.store.UpdateEventStatus(eventID, domain.DeliveryStatusProcessing); err != nil {
		log.Printf("start delivery for event %s: %v", eventID, err)
		return
	}

	event, ok := p.store.GetEvent(eventID)
	if !ok {
		log.Printf("event %s disappeared during delivery", eventID)
		return
	}
	endpoint, ok := p.store.GetEndpoint(event.EndpointID)
	if !ok {
		p.finish(eventID, domain.DeliveryStatusFailed, fmt.Errorf("endpoint %s not found", event.EndpointID))
		return
	}

	err := p.sender.Send(context.Background(), event, endpoint)
	if err != nil {
		p.finish(eventID, domain.DeliveryStatusFailed, err)
		return
	}
	p.finish(eventID, domain.DeliveryStatusDelivered, nil)
}

func (p *Pool) finish(eventID string, status domain.DeliveryStatus, deliveryErr error) {
	if deliveryErr != nil {
		log.Printf("delivery of event %s failed: %v", eventID, deliveryErr)
	}
	if err := p.store.UpdateEventStatus(eventID, status); err != nil {
		log.Printf("finish delivery of event %s: %v", eventID, err)
	}
}
