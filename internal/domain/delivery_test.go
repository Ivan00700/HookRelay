package domain

import (
	"testing"
	"time"
)

func TestRetrySchedule(t *testing.T) {
	for _, tc := range []struct {
		attempt int
		delay   time.Duration
	}{{0, 0}, {1, 5 * time.Second}, {2, 30 * time.Second}, {3, 2 * time.Minute}, {4, 0}, {5, 0}} {
		if got := RetryDelay(tc.attempt); got != tc.delay {
			t.Fatalf("attempt %d: delay=%v, want %v", tc.attempt, got, tc.delay)
		}
	}
	for attempt := 1; attempt <= MaxDeliveryAttempts; attempt++ {
		result := DeliveryResult{HTTPStatus: 500, ErrorMessage: "server error", Retryable: true}
		status, delay := result.NextStatus(attempt)
		if attempt < MaxDeliveryAttempts {
			if status != DeliveryStatusPending || delay != RetryDelay(attempt) {
				t.Fatalf("attempt %d: %s, %v", attempt, status, delay)
			}
		} else if status != DeliveryStatusFailed || delay != 0 {
			t.Fatalf("last attempt must fail: %s, %v", status, delay)
		}
	}
}

func TestDeliveryResult(t *testing.T) {
	for _, tc := range []struct {
		result DeliveryResult
		valid  bool
		status DeliveryStatus
	}{
		{DeliveryResult{HTTPStatus: 204}, true, DeliveryStatusDelivered},
		{DeliveryResult{HTTPStatus: 400, ErrorMessage: "bad request"}, true, DeliveryStatusFailed},
		{DeliveryResult{ErrorMessage: "timeout", Retryable: true}, true, DeliveryStatusPending},
		{DeliveryResult{}, false, ""},
		{DeliveryResult{HTTPStatus: 999, ErrorMessage: "invalid"}, false, ""},
		{DeliveryResult{HTTPStatus: 204, Retryable: true}, false, ""},
	} {
		if tc.result.Valid() != tc.valid {
			t.Fatalf("validation result: %+v", tc.result)
		}
		if tc.valid {
			if status, _ := tc.result.NextStatus(1); status != tc.status {
				t.Fatalf("result %+v: status=%s, want %s", tc.result, status, tc.status)
			}
		}
	}
}
