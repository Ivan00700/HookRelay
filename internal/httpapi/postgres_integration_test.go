package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"hookrelay/internal/domain"
	"hookrelay/internal/repository"
	"hookrelay/internal/testutil"
)

func TestPostgresAPIErrorsIntegration(t *testing.T) {
	pool, _ := testutil.Database(t)
	store := repository.NewPostgresStore(pool)
	mux := http.NewServeMux()
	RegisterEndpoints(mux, store)
	RegisterEvents(mux, store)
	created := request(mux, http.MethodPost, "/api/v1/endpoints", `{"name":"orders","url":"http://example.test","secret":"private"}`)
	if created.Code != http.StatusCreated || strings.Contains(created.Body.String(), "private") {
		t.Fatalf("create endpoint: %d %s", created.Code, created.Body.String())
	}
	var endpoint domain.Endpoint
	if err := json.Unmarshal(created.Body.Bytes(), &endpoint); err != nil {
		t.Fatal(err)
	}
	var persisted domain.Endpoint
	read := request(mux, http.MethodGet, "/api/v1/endpoints/"+endpoint.ID, "")
	if read.Code != http.StatusOK {
		t.Fatalf("read endpoint: %d %s", read.Code, read.Body.String())
	}
	if err := json.Unmarshal(read.Body.Bytes(), &persisted); err != nil {
		t.Fatal(err)
	}
	if !endpoint.CreatedAt.Equal(persisted.CreatedAt) {
		t.Fatalf("created_at changed after reading: %v != %v", endpoint.CreatedAt, persisted.CreatedAt)
	}
	body := fmt.Sprintf(`{"endpoint_id":%q,"type":"order.created","payload":{}}`, endpoint.ID)
	createdEvent := request(mux, http.MethodPost, "/api/v1/events", body)
	if createdEvent.Code != http.StatusCreated {
		t.Fatalf("create event without workers: %d %s", createdEvent.Code, createdEvent.Body.String())
	}
	listed := request(mux, http.MethodGet, "/api/v1/events", "")
	var saved []domain.Event
	if err := json.Unmarshal(listed.Body.Bytes(), &saved); err != nil || listed.Code != http.StatusOK || len(saved) != 1 || saved[0].Status != domain.DeliveryStatusPending {
		t.Fatalf("pending event without workers: %d %s, %v", listed.Code, listed.Body.String(), err)
	}
	assertAPIError(t, request(mux, http.MethodGet, "/api/v1/endpoints/missing", ""), http.StatusNotFound, "not_found", "")
	assertAPIError(t, request(mux, http.MethodGet, "/api/v1/events/missing", ""), http.StatusNotFound, "not_found", "")
	assertAPIError(t, request(mux, http.MethodPost, "/api/v1/events", `{"endpoint_id":"missing","type":"order.created","payload":{}}`), http.StatusNotFound, "not_found", "")

	pool.Close()
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/endpoints", ""},
		{http.MethodGet, "/api/v1/endpoints/" + endpoint.ID, ""},
		{http.MethodGet, "/api/v1/events", ""},
		{http.MethodGet, "/api/v1/events/missing", ""},
		{http.MethodGet, "/api/v1/events/missing/attempts", ""},
		{http.MethodPost, "/api/v1/events/missing/retry", ""},
		{http.MethodPost, "/api/v1/endpoints", `{"name":"orders","url":"http://example.test","secret":"private"}`},
		{http.MethodPost, "/api/v1/events", body},
		{http.MethodDelete, "/api/v1/endpoints/" + endpoint.ID, ""},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			response := request(mux, tc.method, tc.path, tc.body)
			assertAPIError(t, response, http.StatusInternalServerError, "internal_error", "")
			if strings.Contains(response.Body.String(), "closed") || strings.Contains(response.Body.String(), "private") {
				t.Fatalf("database details exposed: %s", response.Body.String())
			}
		})
	}
}
