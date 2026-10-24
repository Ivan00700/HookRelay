package domain

import (
	"bytes"
	"encoding/json"
	"net/url"
	"strings"
)

type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e *ValidationError) Error() string {
	return e.Field + ": " + e.Message
}

func (in CreateEndpointInput) Validate() error {
	if strings.TrimSpace(in.Name) == "" {
		return &ValidationError{Field: "name", Message: "name is required"}
	}

	if strings.TrimSpace(in.URL) == "" {
		return &ValidationError{Field: "url", Message: "URL is required"}
	}

	parsedURL, err := url.Parse(in.URL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Hostname() == "" {
		return &ValidationError{Field: "url", Message: "must be an absolute HTTP URL"}
	}

	if strings.TrimSpace(in.Secret) == "" {
		return &ValidationError{Field: "secret", Message: "secret is required"}
	}

	return nil
}

func (in CreateEventInput) Validate() error {
	if strings.TrimSpace(in.EndpointID) == "" {
		return &ValidationError{Field: "endpoint_id", Message: "endpoint ID is required"}
	}

	if strings.TrimSpace(in.Type) == "" {
		return &ValidationError{Field: "type", Message: "type is required"}
	}

	if !json.Valid(in.Payload) {
		return &ValidationError{Field: "payload", Message: "must be valid JSON"}
	}

	// валидный JSON может быть массивом или скаляром - для события нужен объект
	if bytes.TrimSpace(in.Payload)[0] != '{' {
		return &ValidationError{Field: "payload", Message: "must be a JSON object"}
	}

	return nil
}

func (s DeliveryStatus) Valid() bool {
	switch s {
	case DeliveryStatusPending,
		DeliveryStatusProcessing,
		DeliveryStatusDelivered,
		DeliveryStatusFailed:
		return true
	default:
		return false
	}
}
