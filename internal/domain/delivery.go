package domain

import "time"

const MaxDeliveryAttempts = 4

type DeliveryAttempt struct {
	ID            int64      `json:"id"`
	EventID       string     `json:"event_id"`
	AttemptNumber int        `json:"attempt_number"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at"`
	HTTPStatus    *int       `json:"http_status"`
	ErrorMessage  string     `json:"error_message,omitempty"`
}

type DeliveryResult struct {
	HTTPStatus   int
	ErrorMessage string
	Retryable    bool
}

func (r DeliveryResult) Valid() bool {
	if r.HTTPStatus != 0 && (r.HTTPStatus < 100 || r.HTTPStatus > 599) {
		return false
	}
	if r.ErrorMessage == "" {
		return r.HTTPStatus >= 200 && r.HTTPStatus < 300 && !r.Retryable
	}
	return true
}

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
	if delay := RetryDelay(attempt); r.Retryable && delay > 0 {
		return DeliveryStatusPending, delay
	}
	return DeliveryStatusFailed, 0
}
