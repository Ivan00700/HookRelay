package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"hookrelay/internal/observability"
	"hookrelay/internal/repository"
	"hookrelay/internal/testutil"
)

func TestMonitoringDatabaseFailureIntegration(t *testing.T) {
	pool, _ := testutil.Database(t)
	store := repository.NewPostgresStore(pool)
	monitor := observability.NewMonitor(nil, store.EventCounts)
	mux := http.NewServeMux()
	NewHandler(store, monitor).RegisterRoutes(mux)
	if response := request(mux, http.MethodGet, "/ready", ""); response.Code != http.StatusOK {
		t.Fatalf("ready database rejected: %d", response.Code)
	}
	if response := request(mux, http.MethodGet, "/metrics", ""); !strings.Contains(response.Body.String(), "hookrelay_database_up 1") {
		t.Fatal("database success not exposed")
	}
	pool.Close()
	if response := request(mux, http.MethodGet, "/ready", ""); response.Code != http.StatusServiceUnavailable || strings.TrimSpace(response.Body.String()) != `{"status":"not_ready"}` {
		t.Fatalf("unavailable database reported ready: %d %s", response.Code, response.Body.String())
	}
	if response := request(mux, http.MethodGet, "/health", ""); response.Code != http.StatusOK {
		t.Fatal("database failure broke liveness")
	}
	response := request(mux, http.MethodGet, "/metrics", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "hookrelay_database_up 0") || strings.Contains(response.Body.String(), "\nhookrelay_queue_events ") {
		t.Fatal("unavailable database produced a fake empty queue")
	}
}
