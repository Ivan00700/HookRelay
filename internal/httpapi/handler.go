package httpapi

import (
	"net/http"

	"hookrelay/internal/observability"
	"hookrelay/internal/repository"
)

// хранит зависимости обработчиков HTTP
type Handler struct {
	store   repository.Store
	monitor *observability.Monitor
}

func NewHandler(store repository.Store, monitor *observability.Monitor) *Handler {
	return &Handler{
		store:   store,
		monitor: monitor,
	}
}

// подключает проверку здоровья и все маршруты API
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("GET /ready", h.ready)
	if h.monitor != nil {
		mux.Handle("GET /metrics", h.monitor.MetricsHandler())
	}
	h.RegisterEndpoints(mux)
	h.RegisterEvents(mux)
}
