package domain

import (
	"encoding/json"
	"time"
)

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

func CanTransition(from, to DeliveryStatus) bool {
	switch from {
	case DeliveryStatusPending:
		return to == DeliveryStatusProcessing
	case DeliveryStatusProcessing:
		return to == DeliveryStatusDelivered || to == DeliveryStatusFailed
	default:
		return false
	}
}
