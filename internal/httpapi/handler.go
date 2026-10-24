package httpapi

import (
	"net/http"

	"hookrelay/internal/repository"
)

// Handler хранит зависимости обработчиков HTTP.
type Handler struct {
	store repository.Store
}

func NewHandler(store repository.Store) *Handler {
	return &Handler{
		store: store,
	}
}

// RegisterRoutes подключает проверку здоровья и все маршруты API.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/health", healthHandler)
	h.RegisterEndpoints(mux)
	h.RegisterEvents(mux)
}
