package domain

import (
	"encoding/json"
	"time"
)

// Статусы описывают состояние доставки, а не HTTP-запроса клиента.
type DeliveryStatus string

const (
	DeliveryStatusPending    DeliveryStatus = "pending"
	DeliveryStatusProcessing DeliveryStatus = "processing"
	DeliveryStatusDelivered  DeliveryStatus = "delivered"
	DeliveryStatusFailed     DeliveryStatus = "failed"
)

type Endpoint struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Secret    string    `json:"-"` // Ключ подписи не включается в ответы API.
	CreatedAt time.Time `json:"created_at"`
}

type CreateEndpointInput struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Secret string `json:"secret"`
}

// Event хранит данные события. Payload передаётся получателю как JSON.
type Event struct {
	ID         string          `json:"id"`
	EndpointID string          `json:"endpoint_id"`
	Type       string          `json:"type"`
	Payload    json.RawMessage `json:"payload"`
	Status     DeliveryStatus  `json:"status"`
	CreatedAt  time.Time       `json:"created_at"`
}

type CreateEventInput struct {
	EndpointID string          `json:"endpoint_id"`
	Type       string          `json:"type"`
	Payload    json.RawMessage `json:"payload"`
}

// DeliveryAttempt хранит результат одной попытки; nil означает отсутствие значения.
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
