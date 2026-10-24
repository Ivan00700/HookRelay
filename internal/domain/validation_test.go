package domain

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestCreateEndpointValidation(t *testing.T) {
	tests := []struct {
		name  string
		input CreateEndpointInput
		field string
	}{
		{"http", CreateEndpointInput{"orders", "http://localhost:9000/webhook", "secret"}, ""},
		{"https", CreateEndpointInput{"orders", "https://example.com/hook", "secret"}, ""},
		{"ipv6", CreateEndpointInput{"orders", "http://[::1]:9000/hook", "secret"}, ""},
		{"empty name", CreateEndpointInput{" \t\n", "https://example.com", "secret"}, "name"},
		{"empty url", CreateEndpointInput{"orders", " ", "secret"}, "url"},
		{"relative url", CreateEndpointInput{"orders", "/hook", "secret"}, "url"},
		{"missing scheme", CreateEndpointInput{"orders", "example.com/hook", "secret"}, "url"},
		{"unsupported scheme", CreateEndpointInput{"orders", "ftp://example.com/hook", "secret"}, "url"},
		{"missing host", CreateEndpointInput{"orders", "https:///hook", "secret"}, "url"},
		{"malformed url", CreateEndpointInput{"orders", "http://[invalid", "secret"}, "url"},
		{"empty secret", CreateEndpointInput{"orders", "https://example.com", " \t"}, "secret"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			checkValidationError(t, tc.input.Validate(), tc.field)
		})
	}
}

func TestCreateEventValidation(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		field   string
	}{
		{"empty object", `{}`, ""},
		{"nested object", `{"order":{"items":[1,2]},"paid":true}`, ""},
		{"whitespace around object", " \n {} \t", ""},
		{"missing payload", "", "payload"},
		{"whitespace only", " \t", "payload"},
		{"broken json", `{"id":`, "payload"},
		{"two objects", `{} {}`, "payload"},
		{"array", `[]`, "payload"},
		{"string", `"text"`, "payload"},
		{"number", `42`, "payload"},
		{"boolean", `false`, "payload"},
		{"null", `null`, "payload"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := CreateEventInput{EndpointID: "endpoint", Type: "order.created", Payload: json.RawMessage(tc.payload)}
			checkValidationError(t, input.Validate(), tc.field)
		})
	}
	for _, tc := range []struct {
		name  string
		input CreateEventInput
		field string
	}{
		{"missing endpoint", CreateEventInput{" \t", "order.created", json.RawMessage(`{}`)}, "endpoint_id"},
		{"missing type", CreateEventInput{"endpoint", " \n", json.RawMessage(`{}`)}, "type"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkValidationError(t, tc.input.Validate(), tc.field)
		})
	}
}

// проверяем тип и поле ошибки - HTTP-слой использует их для ответа клиенту
func checkValidationError(t *testing.T, err error, field string) {
	t.Helper()
	if field == "" {
		if err != nil {
			t.Fatalf("valid input rejected: %v", err)
		}
		return
	}
	var validation *ValidationError
	if !errors.As(err, &validation) || validation.Field != field || validation.Message == "" {
		t.Fatalf("validation error=%v, want nonempty message for field %s", err, field)
	}
}
