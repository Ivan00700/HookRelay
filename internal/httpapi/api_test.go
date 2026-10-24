package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hookrelay/internal/delivery"
	"hookrelay/internal/domain"
	"hookrelay/internal/repository"
)

func testMux() *http.ServeMux {
	mux := http.NewServeMux()
	store := repository.NewMemoryStore()
	NewHandler(store).RegisterRoutes(mux)
	return mux
}

func request(mux *http.ServeMux, method, path, body string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(method, path, strings.NewReader(body)))
	return response
}

func TestEndpointEventLifecycle(t *testing.T) {
	mux := testMux()

	createdEndpoint := request(mux, http.MethodPost, "/api/v1/endpoints", `{"name":"orders","url":"https://example.com/hook","secret":"private"}`)
	if createdEndpoint.Code != http.StatusCreated {
		t.Fatalf("create endpoint: status = %d, body = %s", createdEndpoint.Code, createdEndpoint.Body.String())
	}
	if strings.Contains(createdEndpoint.Body.String(), "private") || strings.Contains(createdEndpoint.Body.String(), "secret") {
		t.Fatalf("endpoint response exposes secret: %s", createdEndpoint.Body.String())
	}
	var endpoint domain.Endpoint
	if err := json.Unmarshal(createdEndpoint.Body.Bytes(), &endpoint); err != nil {
		t.Fatal(err)
	}
	if len(endpoint.ID) != 32 || endpoint.Name != "orders" || endpoint.CreatedAt.IsZero() || endpoint.CreatedAt.Location() != time.UTC {
		t.Fatalf("created endpoint = %#v", endpoint)
	}

	gotEndpoint := request(mux, http.MethodGet, "/api/v1/endpoints/"+endpoint.ID, "")
	if gotEndpoint.Code != http.StatusOK || !strings.Contains(gotEndpoint.Body.String(), endpoint.ID) {
		t.Fatalf("get endpoint: status = %d, body = %s", gotEndpoint.Code, gotEndpoint.Body.String())
	}
	listedEndpoints := request(mux, http.MethodGet, "/api/v1/endpoints", "")
	if listedEndpoints.Code != http.StatusOK || !strings.Contains(listedEndpoints.Body.String(), endpoint.ID) {
		t.Fatalf("list endpoints: status = %d, body = %s", listedEndpoints.Code, listedEndpoints.Body.String())
	}
	if strings.Contains(listedEndpoints.Body.String(), "private") {
		t.Fatalf("endpoint list exposes secret: %s", listedEndpoints.Body.String())
	}

	eventBody := `{"endpoint_id":"` + endpoint.ID + `","type":"order.created","payload":{"order_id":42}}`
	createdEvent := request(mux, http.MethodPost, "/api/v1/events", eventBody)
	if createdEvent.Code != http.StatusCreated {
		t.Fatalf("create event: status = %d, body = %s", createdEvent.Code, createdEvent.Body.String())
	}
	var event domain.Event
	if err := json.Unmarshal(createdEvent.Body.Bytes(), &event); err != nil {
		t.Fatal(err)
	}
	if len(event.ID) != 32 || event.EndpointID != endpoint.ID || event.Status != domain.DeliveryStatusPending || event.CreatedAt.IsZero() {
		t.Fatalf("created event = %#v", event)
	}
	if string(event.Payload) != `{"order_id":42}` {
		t.Fatalf("payload = %s, want JSON object", event.Payload)
	}
	gotEvent := request(mux, http.MethodGet, "/api/v1/events/"+event.ID, "")
	if gotEvent.Code != http.StatusOK || !strings.Contains(gotEvent.Body.String(), event.ID) {
		t.Fatalf("get event: status = %d, body = %s", gotEvent.Code, gotEvent.Body.String())
	}
	listedEvents := request(mux, http.MethodGet, "/api/v1/events", "")
	if listedEvents.Code != http.StatusOK || !strings.Contains(listedEvents.Body.String(), event.ID) {
		t.Fatalf("list events: status = %d, body = %s", listedEvents.Code, listedEvents.Body.String())
	}
	blockedDelete := request(mux, http.MethodDelete, "/api/v1/endpoints/"+endpoint.ID, "")
	assertAPIError(t, blockedDelete, http.StatusConflict, "conflict", "")
}

func TestDeleteEndpointWithoutEvents(t *testing.T) {
	mux := testMux()
	created := request(mux, http.MethodPost, "/api/v1/endpoints", `{"name":"orders","url":"https://example.com/hook","secret":"private"}`)
	var endpoint domain.Endpoint
	if err := json.Unmarshal(created.Body.Bytes(), &endpoint); err != nil {
		t.Fatal(err)
	}
	deleted := request(mux, http.MethodDelete, "/api/v1/endpoints/"+endpoint.ID, "")
	if deleted.Code != http.StatusNoContent || deleted.Body.Len() != 0 {
		t.Fatalf("delete endpoint: status = %d, body = %q", deleted.Code, deleted.Body.String())
	}
	assertAPIError(t, request(mux, http.MethodGet, "/api/v1/endpoints/"+endpoint.ID, ""), http.StatusNotFound, "not_found", "")
	assertAPIError(t, request(mux, http.MethodDelete, "/api/v1/endpoints/"+endpoint.ID, ""), http.StatusNotFound, "not_found", "")
	assertAPIError(t, request(mux, http.MethodGet, "/api/v1/events/missing", ""), http.StatusNotFound, "not_found", "")
}

func TestCreateEventPersistsWithoutWorkers(t *testing.T) {
	store := repository.NewMemoryStore()
	if err := store.CreateEndpoint(context.Background(), domain.Endpoint{ID: "ep-1"}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	NewHandler(store).RegisterEvents(mux)
	response := request(mux, http.MethodPost, "/api/v1/events", `{"endpoint_id":"ep-1","type":"order.created","payload":{}}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("create event: %d %s", response.Code, response.Body.String())
	}
	if got, err := store.ListEvents(context.Background()); err != nil || len(got) != 1 || got[0].Status != domain.DeliveryStatusPending {
		t.Fatalf("pending event was not saved: %+v, %v", got, err)
	}
}

func TestEventIsDeliveredByPool(t *testing.T) {
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("X-HookRelay-Event-ID") == "" {
			t.Errorf("unexpected delivery request: method=%s headers=%v", r.Method, r.Header)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()

	store := repository.NewMemoryStore()
	pool, err := delivery.NewPool(store, delivery.NewSender(receiver.Client()), 2)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	mux := http.NewServeMux()
	NewHandler(store).RegisterRoutes(mux)

	endpointBody := fmt.Sprintf(`{"name":"orders","url":%q,"secret":"private"}`, receiver.URL)
	createdEndpoint := request(mux, http.MethodPost, "/api/v1/endpoints", endpointBody)
	if createdEndpoint.Code != http.StatusCreated {
		t.Fatalf("create endpoint: %d %s", createdEndpoint.Code, createdEndpoint.Body.String())
	}
	var endpoint domain.Endpoint
	if err := json.Unmarshal(createdEndpoint.Body.Bytes(), &endpoint); err != nil {
		t.Fatal(err)
	}
	eventBody := fmt.Sprintf(`{"endpoint_id":%q,"type":"order.created","payload":{"order_id":42}}`, endpoint.ID)
	createdEvent := request(mux, http.MethodPost, "/api/v1/events", eventBody)
	if createdEvent.Code != http.StatusCreated {
		t.Fatalf("create event: %d %s", createdEvent.Code, createdEvent.Body.String())
	}
	var event domain.Event
	if err := json.Unmarshal(createdEvent.Body.Bytes(), &event); err != nil {
		t.Fatal(err)
	}
	if event.Status != domain.DeliveryStatusPending {
		t.Fatalf("created event status = %s, want pending", event.Status)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		response := request(mux, http.MethodGet, "/api/v1/events/"+event.ID, "")
		if response.Code != http.StatusOK {
			t.Fatalf("get event: %d %s", response.Code, response.Body.String())
		}
		var current domain.Event
		if err := json.Unmarshal(response.Body.Bytes(), &current); err != nil {
			t.Fatal(err)
		}
		if current.Status == domain.DeliveryStatusDelivered {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("event %s was not delivered", event.ID)
}

func TestCreateEndpointErrors(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		status int
		code   string
		field  string
	}{
		{"invalid JSON", `{bad`, http.StatusBadRequest, "invalid_json", ""},
		{"null", `null`, http.StatusBadRequest, "invalid_json", ""},
		{"two objects", `{} {}`, http.StatusBadRequest, "invalid_json", ""},
		{"unknown field", `{"name":"orders","url":"https://example.com","secret":"x","extra":1}`, http.StatusBadRequest, "invalid_json", ""},
		{"missing name", `{"url":"https://example.com","secret":"x"}`, http.StatusBadRequest, "validation_error", "name"},
		{"invalid URL", `{"name":"orders","url":"ftp://example.com","secret":"x"}`, http.StatusBadRequest, "validation_error", "url"},
		{"too large", strings.Repeat(" ", maxRequestBodyBytes) + `{}`, http.StatusRequestEntityTooLarge, "invalid_json", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertAPIError(t, request(testMux(), http.MethodPost, "/api/v1/endpoints", tt.body), tt.status, tt.code, tt.field)
		})
	}
}

func TestCreateEventErrors(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		status int
		code   string
		field  string
	}{
		{"missing endpoint", `{"endpoint_id":"missing","type":"order.created","payload":{}}`, http.StatusNotFound, "not_found", ""},
		{"missing type", `{"endpoint_id":"missing","payload":{}}`, http.StatusBadRequest, "validation_error", "type"},
		{"non-object payload", `{"endpoint_id":"missing","type":"order.created","payload":"text"}`, http.StatusBadRequest, "validation_error", "payload"},
		{"client status", `{"endpoint_id":"missing","type":"order.created","payload":{},"status":"delivered"}`, http.StatusBadRequest, "invalid_json", ""},
		{"too large", strings.Repeat(" ", maxRequestBodyBytes) + `{}`, http.StatusRequestEntityTooLarge, "invalid_json", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertAPIError(t, request(testMux(), http.MethodPost, "/api/v1/events", tt.body), tt.status, tt.code, tt.field)
		})
	}
}

func assertAPIError(t *testing.T, response *httptest.ResponseRecorder, status int, code, field string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, status, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	var body struct {
		Error map[string]json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if got := string(body.Error["code"]); got != `"`+code+`"` {
		t.Fatalf("error code = %s, want %s", got, code)
	}
	if _, ok := body.Error["message"]; !ok {
		t.Fatal("error message missing")
	}
	gotField, hasField := body.Error["field"]
	if field == "" && hasField {
		t.Fatalf("unexpected error field = %s", gotField)
	}
	if field != "" && string(gotField) != `"`+field+`"` {
		t.Fatalf("error field = %s, want %s", gotField, field)
	}
}
