package observability

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"hookrelay/internal/domain"
	"hookrelay/internal/postgres"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestBusinessCounters(t *testing.T) {
	monitor := NewMonitor(nil, nil)
	monitor.Accepted()
	monitor.Accepted()
	monitor.Delivery(domain.DeliveryResult{HTTPStatus: 503, ErrorMessage: "unavailable", Retryable: true}, 1)
	monitor.Delivery(domain.DeliveryResult{HTTPStatus: 400, ErrorMessage: "invalid"}, 2)
	monitor.Delivery(domain.DeliveryResult{HTTPStatus: 204}, 1)
	if testutil.ToFloat64(monitor.accepted) != 2 || testutil.ToFloat64(monitor.attempts.WithLabelValues("success")) != 1 || testutil.ToFloat64(monitor.attempts.WithLabelValues("error")) != 2 {
		t.Fatal("accepted or attempt counters are incorrect")
	}
	if testutil.ToFloat64(monitor.deliveries.WithLabelValues("delivered")) != 1 || testutil.ToFloat64(monitor.deliveries.WithLabelValues("failed")) != 1 {
		t.Fatal("a scheduled retry was counted as a terminal result")
	}
}

func TestQueueMetricsAndDatabaseFailure(t *testing.T) {
	var unavailable atomic.Bool
	monitor := NewMonitor(nil, func(ctx context.Context) (map[domain.DeliveryStatus]int64, error) {
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 2*time.Second {
			t.Error("queue collection is not bounded")
		}
		if unavailable.Load() {
			return nil, errors.New("private-database-error")
		}
		return map[domain.DeliveryStatus]int64{domain.DeliveryStatusPending: 3, domain.DeliveryStatusProcessing: 2, domain.DeliveryStatusDelivered: 7, domain.DeliveryStatusFailed: 1}, nil
	})
	handler := monitor.MetricsHandler()
	scrape := func() string {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		if response.Code != http.StatusOK {
			t.Fatalf("scrape status=%d", response.Code)
		}
		return response.Body.String()
	}
	body := scrape()
	for _, expected := range []string{"hookrelay_database_up 1", "hookrelay_queue_events 5", `hookrelay_events{status="failed"} 1`, `hookrelay_events{status="delivered"} 7`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("missing metric %q", expected)
		}
	}
	unavailable.Store(true)
	body = scrape()
	if !strings.Contains(body, "hookrelay_database_up 0") || strings.Contains(body, "\nhookrelay_queue_events ") || strings.Contains(body, "\nhookrelay_events{") || strings.Contains(body, "private-database-error") {
		t.Fatal("failed collection exposed stale queue data or an error message")
	}
}

func TestConcurrentObservations(t *testing.T) {
	monitor := NewMonitor(nil, nil)
	var workers sync.WaitGroup
	for i := 0; i < 20; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for n := 0; n < 100; n++ {
				monitor.Accepted()
				monitor.Delivery(domain.DeliveryResult{HTTPStatus: 204}, 1)
			}
		}()
	}
	workers.Wait()
	if testutil.ToFloat64(monitor.accepted) != 2000 || testutil.ToFloat64(monitor.deliveries.WithLabelValues("delivered")) != 2000 {
		t.Fatal("concurrent observations were lost")
	}
	// новый экземпляр имеет отдельный registry и не наследует счётчики процесса
	second := NewMonitor(nil, nil)
	if testutil.ToFloat64(second.accepted) != 0 {
		t.Fatal("metrics leaked between monitors")
	}
}

func TestErrorKindDoesNotExposePrivateData(t *testing.T) {
	tests := []struct {
		err  error
		kind string
	}{
		{context.DeadlineExceeded, "timeout"},
		{context.Canceled, "canceled"},
		{fmt.Errorf("open: %w", postgres.ErrMissingURL), "missing_database_url"},
		{postgres.ErrInvalidConfig, "invalid_database_config"},
		{&pgconn.PgError{Code: "23505", Message: "private-message"}, "postgres_23505"},
		{&url.Error{Op: "POST", URL: "https://user:private-password@example.test/?token=private-token", Err: errors.New("private-message")}, "network"},
		{errors.New("private-message"), "internal"},
	}
	for _, test := range tests {
		if got := ErrorKind(test.err); got != test.kind || strings.Contains(got, "private") {
			t.Fatalf("error kind=%q, want %q", got, test.kind)
		}
	}
}
