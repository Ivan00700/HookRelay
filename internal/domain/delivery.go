package domain

import "time"

// четыре попытки - первая отправка и три повтора
const MaxDeliveryAttempts = 4

func (r DeliveryResult) Valid() bool {
	if r.HTTPStatus != 0 && (r.HTTPStatus < 100 || r.HTTPStatus > 599) {
		return false
	}
	if r.ErrorMessage == "" {
		return r.HTTPStatus >= 200 && r.HTTPStatus < 300 && !r.Retryable
	}
	return true
}

// задержка после неудачной попытки - ноль означает конец цикла
func RetryDelay(attempt int) time.Duration {
	switch attempt {
	case 1:
		return 5 * time.Second
	case 2:
		return 30 * time.Second
	case 3:
		return 2 * time.Minute
	default:
		return 0
	}
}

func (r DeliveryResult) NextStatus(attempt int) (DeliveryStatus, time.Duration) {
	if r.ErrorMessage == "" && r.HTTPStatus >= 200 && r.HTTPStatus < 300 {
		return DeliveryStatusDelivered, 0
	}
	delay := RetryDelay(attempt)
	if r.Retryable && delay > 0 {
		return DeliveryStatusPending, delay
	}
	return DeliveryStatusFailed, 0
}
