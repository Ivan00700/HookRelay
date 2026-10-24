package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"hookrelay/internal/repository"
)

func TestHealthRoute(t *testing.T) {
	mux := http.NewServeMux()
	NewHandler(repository.NewMemoryStore(), nil).RegisterRoutes(mux)
	for _, test := range []struct {
		method string
		status int
		body   string
	}{
		{http.MethodGet, http.StatusOK, `{"status":"ok"}`},
		{http.MethodHead, http.StatusMethodNotAllowed, ""},
		{http.MethodPost, http.StatusMethodNotAllowed, ""},
	} {
		t.Run(test.method, func(t *testing.T) {
			request := httptest.NewRequest(test.method, "/health", nil)
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)
			if response.Code != test.status || response.Body.String() != test.body {
				t.Fatalf("status=%d body=%q, want %d %q", response.Code, response.Body.String(), test.status, test.body)
			}
			if test.status == http.StatusOK && response.Header().Get("Content-Type") != "application/json" {
				t.Fatal("health response is not JSON")
			}
			if test.status == http.StatusMethodNotAllowed && response.Header().Get("Allow") != http.MethodGet {
				t.Fatal("health response is missing Allow: GET")
			}
		})
	}
}
