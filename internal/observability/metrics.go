package observability

import (
	"io"
	"log/slog"
	"net/http"

	"hookrelay/internal/domain"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// каждый экземпляр сервиса хранит свой registry - глобальной регистрации нет
type Monitor struct {
	logger      *slog.Logger
	registry    *prometheus.Registry
	accepted    prometheus.Counter
	attempts    *prometheus.CounterVec
	deliveries  *prometheus.CounterVec
	requests    *prometheus.CounterVec
	requestTime *prometheus.HistogramVec
}

func NewMonitor(logger *slog.Logger, readCounts CountsReader) *Monitor {
	if logger == nil {
		logger = NewLogger(io.Discard)
	}
	m := &Monitor{
		logger:   logger,
		registry: prometheus.NewRegistry(),
		accepted: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "hookrelay_events_accepted_total",
			Help: "Events accepted through the HTTP API in this process.",
		}),
		attempts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "hookrelay_delivery_attempts_total",
			Help: "Delivery attempt results saved by workers in this process.",
		}, []string{"result"}),
		deliveries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "hookrelay_delivery_results_total",
			Help: "Terminal delivery results saved by workers in this process.",
		}, []string{"status"}),
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "hookrelay_http_requests_total",
			Help: "Completed HTTP requests by method, route and status.",
		}, []string{"method", "route", "status"}),
		requestTime: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "hookrelay_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "route"}),
	}
	for _, result := range []string{"success", "error"} {
		m.attempts.WithLabelValues(result)
	}
	for _, status := range []string{"delivered", "failed"} {
		m.deliveries.WithLabelValues(status)
	}
	m.registry.MustRegister(m.accepted, m.attempts, m.deliveries, m.requests, m.requestTime, newQueueCollector(readCounts))
	return m
}

func (m *Monitor) Logger() *slog.Logger {
	if m == nil {
		return slog.Default()
	}
	return m.logger
}

func (m *Monitor) Accepted() {
	if m != nil {
		m.accepted.Inc()
	}
}

// считаем только сохранённый результат - потеря аренды не означает новую доставку
func (m *Monitor) Delivery(result domain.DeliveryResult, cycleAttempt int) {
	if m == nil {
		return
	}
	label := "error"
	if result.ErrorMessage == "" && result.HTTPStatus >= 200 && result.HTTPStatus < 300 {
		label = "success"
	}
	m.attempts.WithLabelValues(label).Inc()
	status, _ := result.NextStatus(cycleAttempt)
	if status == domain.DeliveryStatusDelivered || status == domain.DeliveryStatusFailed {
		m.deliveries.WithLabelValues(string(status)).Inc()
	}
}

func (m *Monitor) MetricsHandler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{
		EnableOpenMetrics:   true,
		MaxRequestsInFlight: 1,
	})
}
