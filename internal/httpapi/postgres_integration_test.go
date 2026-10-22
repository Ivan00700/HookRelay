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
	RegisterEvents(mux, store, rejectingPublisher{})
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
	assertAPIError(t, request(mux, http.MethodPost, "/api/v1/events", body), http.StatusServiceUnavailable, "delivery_unavailable", "")
	listed := request(mux, http.MethodGet, "/api/v1/events", "")
	if listed.Code != http.StatusOK || strings.TrimSpace(listed.Body.String()) != "[]" {
		t.Fatalf("failed enqueue left an event: %d %s", listed.Code, listed.Body.String())
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
