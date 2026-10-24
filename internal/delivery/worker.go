package delivery

import (
	"context"
	"errors"
	"log"
	"time"

	"hookrelay/internal/domain"
	"hookrelay/internal/repository"
)

func (p *Pool) worker() {
	defer p.wg.Done()
	for p.ctx.Err() == nil {
		// 1) Забираем готовое задание с ограничением времени обращения к БД.
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
		// 2) Пустую очередь опрашиваем с паузой, которую прерывает остановка.
		timer := time.NewTimer(pollInterval)
		select {
		case <-p.ctx.Done():
		case <-timer.C:
		}
		timer.Stop()
	}
}

func (p *Pool) deliver(job repository.DeliveryJob) {
	// Начатую доставку завершаем даже после остановки захвата новых заданий.
	ctx, cancel := context.WithTimeout(context.Background(), jobTimeout)
	defer cancel()

	// 1) Получаем адрес и секрет получателя.
	endpoint, err := p.store.GetEndpoint(ctx, job.Event.EndpointID)
	if err != nil {
		log.Printf("get endpoint for event %s: %v", job.Event.ID, err)
		return
	}

	// 2) Отправляем событие и определяем, допустим ли повтор.
	status, err := p.sender.Send(ctx, job.Event, endpoint)
	result := domain.DeliveryResult{HTTPStatus: status, Retryable: shouldRetry(status, err)}
	if err != nil {
		log.Printf("delivery of event %s failed: %v", job.Event.ID, err)
		result.ErrorMessage = err.Error()
	}

	// 3) Записываем попытку и состояние события одной операцией хранилища.
	finishCtx, finishCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer finishCancel()
	if err := p.store.FinishEvent(finishCtx, job, result); err != nil {
		log.Printf("finish delivery of event %s: %v", job.Event.ID, err)
	}
}
