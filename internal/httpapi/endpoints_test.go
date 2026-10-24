package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hookrelay/internal/domain"
	"hookrelay/internal/repository"
)

func TestListEndpointsEmpty(t *testing.T) {
	mux := http.NewServeMux()
	NewHandler(repository.NewMemoryStore(), nil).RegisterEndpoints(mux)

	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/endpoints", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if got := strings.TrimSpace(response.Body.String()); got != "[]" {
		t.Fatalf("body = %q, want []", got)
	}
}

func TestListEndpointsReturnsStoredEndpointWithoutSecret(t *testing.T) {
	store := repository.NewMemoryStore()
	if err := store.CreateEndpoint(context.Background(), domain.Endpoint{
		ID: "ep-1", Name: "orders", URL: "https://example.com/hook", Secret: "private",
	}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	NewHandler(store, nil).RegisterEndpoints(mux)

	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/endpoints", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var endpoints []map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &endpoints); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(endpoints) != 1 {
		t.Fatalf("endpoint count = %d, want 1", len(endpoints))
	}
	if got := string(endpoints[0]["id"]); got != `"ep-1"` {
		t.Fatalf("id = %s, want ep-1", got)
	}
	if _, ok := endpoints[0]["secret"]; ok {
		t.Fatal("response must not contain secret")
	}
}
