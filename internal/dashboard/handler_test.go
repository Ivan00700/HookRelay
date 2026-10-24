package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDashboardRoutes(t *testing.T) {
	mux := http.NewServeMux()
	RegisterRoutes(mux)
	for _, test := range []struct {
		method string
		path   string
		status int
		typeOf string
		text   string
	}{
		{http.MethodGet, "/", http.StatusOK, "text/html", "HookRelay"},
		{http.MethodGet, "/assets/app.css", http.StatusOK, "text/css", ".container"},
		{http.MethodGet, "/assets/app.js", http.StatusOK, "javascript", "/api/v1/events"},
		{http.MethodHead, "/", http.StatusOK, "text/html", ""},
		{http.MethodPost, "/", http.StatusMethodNotAllowed, "", ""},
		{http.MethodGet, "/missing", http.StatusNotFound, "", ""},
		{http.MethodGet, "/assets/", http.StatusNotFound, "", ""},
		{http.MethodGet, "/assets/index.html", http.StatusNotFound, "", ""},
		{http.MethodGet, "/api/v1/events", http.StatusNotFound, "", ""},
	} {
		t.Run(test.method+test.path, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, nil)
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status=%d, want %d", response.Code, test.status)
			}
			if !strings.Contains(response.Header().Get("Content-Type"), test.typeOf) || !strings.Contains(response.Body.String(), test.text) {
				t.Fatalf("unexpected content: %s", response.Header().Get("Content-Type"))
			}
			if test.status == http.StatusOK && response.Header().Get("Content-Security-Policy") == "" {
				t.Fatal("missing content security policy")
			}
			if test.method == http.MethodHead && response.Body.Len() != 0 {
				t.Fatal("HEAD returned a body")
			}
		})
	}
}
