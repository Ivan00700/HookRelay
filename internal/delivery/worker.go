package delivery

import (
	"context"
	"errors"
	"time"

	"hookrelay/internal/domain"
	"hookrelay/internal/observability"
	"hookrelay/internal/repository"
)

func (p *Pool) worker() {
	defer p.wg.Done()
	for p.ctx.Err() == nil {
		// забираем готовое задание с ограничением времени обращения к БД
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
			p.monitor.Logger().Error("delivery claim failed", "error_kind", observability.ErrorKind(err))
		}
		// ждём перед следующим опросом пустой очереди - остановка прерывает паузу
		timer := time.NewTimer(pollInterval)
		select {
		case <-p.ctx.Done():
		case <-timer.C:
		}
		timer.Stop()
	}
}

func (p *Pool) deliver(job repository.DeliveryJob) {
	logger := p.monitor.Logger().With("event_id", job.Event.ID, "attempt_number", job.AttemptNumber, "cycle_attempt", job.CycleAttempt)
	logger.Info("delivery started")
	// начатую доставку завершаем даже после остановки захвата новых заданий
	ctx, cancel := context.WithTimeout(context.Background(), jobTimeout)
	defer cancel()

	// получаем адрес и секрет получателя
	endpoint, err := p.store.GetEndpoint(ctx, job.Event.EndpointID)
	if err != nil {
		logger.Error("delivery endpoint lookup failed", "error_kind", observability.ErrorKind(err))
		return
	}

	// отправляем событие и определяем возможность повтора
	status, err := p.sender.Send(ctx, job.Event, endpoint)
	result := domain.DeliveryResult{HTTPStatus: status, Retryable: shouldRetry(status, err)}
	if err != nil {
		result.ErrorMessage = err.Error()
	}

	// записываем попытку и состояние события одной операцией хранилища
	finishCtx, finishCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer finishCancel()
	if err := p.store.FinishEvent(finishCtx, job, result); err != nil {
		kind := observability.ErrorKind(err)
		if errors.Is(err, repository.ErrLeaseLost) {
			kind = "lease_lost"
		}
		logger.Error("delivery result save failed", "error_kind", kind)
		return
	}
	p.monitor.Delivery(result, job.CycleAttempt)
	statusAfter, delay := result.NextStatus(job.CycleAttempt)
	if result.ErrorMessage != "" {
		kind := "http_response"
		if status == 0 {
			kind = observability.ErrorKind(err)
			if errors.Is(err, ErrInvalidRequest) {
				kind = "invalid_request"
			}
		}
		logger.Warn("delivery attempt failed", "http_status", status, "retryable", result.Retryable, "status", statusAfter, "retry_delay_seconds", delay.Seconds(), "error_kind", kind)
		return
	}
	logger.Info("delivery completed", "http_status", status, "status", statusAfter)
}
