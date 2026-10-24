package httpapi

import (
	"errors"
	"log"
	"net/http"
	"time"

	"hookrelay/internal/domain"
	"hookrelay/internal/repository"
)

func (h *Handler) RegisterEndpoints(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/endpoints", withStorageTimeout(h.createEndpoint))
	mux.HandleFunc("GET /api/v1/endpoints", withStorageTimeout(h.listEndpoints))
	mux.HandleFunc("GET /api/v1/endpoints/{id}", withStorageTimeout(h.getEndpoint))
	mux.HandleFunc("DELETE /api/v1/endpoints/{id}", withStorageTimeout(h.deleteEndpoint))
}

func (h *Handler) listEndpoints(w http.ResponseWriter, r *http.Request) {
	endpoints, err := h.store.ListEndpoints(r.Context())
	if err != nil {
		log.Printf("list endpoints: %v", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", "")
		return
	}
	writeJSON(w, http.StatusOK, endpoints)
}

func (h *Handler) createEndpoint(w http.ResponseWriter, r *http.Request) {
	// 1) Читаем и проверяем входные данные.
	var input *domain.CreateEndpointInput
	if requestErr := decodeJSON(w, r, &input); requestErr != nil {
		writeError(w, requestErr.status, requestErr.code, requestErr.message, "")
		return
	}
	if input == nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be a JSON object", "")
		return
	}
	if err := input.Validate(); err != nil {
		var validationErr *domain.ValidationError
		if errors.As(err, &validationErr) {
			writeError(w, http.StatusBadRequest, "validation_error", validationErr.Message, validationErr.Field)
			return
		}
		log.Printf("validate endpoint: %v", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", "")
		return
	}

	// 2) Формируем запись с ID и временем создания.
	id, err := newID()
	if err != nil {
		log.Printf("generate endpoint ID: %v", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", "")
		return
	}
	endpoint := domain.Endpoint{
		ID:        id,
		Name:      input.Name,
		URL:       input.URL,
		Secret:    input.Secret,
		CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
	}
	// 3) Сохраняем запись и возвращаем результат клиенту.
	if err := h.store.CreateEndpoint(r.Context(), endpoint); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			writeError(w, http.StatusConflict, "conflict", "endpoint ID already exists", "")
			return
		}
		log.Printf("create endpoint: %v", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", "")
		return
	}
	writeJSON(w, http.StatusCreated, endpoint)
}

func (h *Handler) getEndpoint(w http.ResponseWriter, r *http.Request) {
	endpoint, err := h.store.GetEndpoint(r.Context(), r.PathValue("id"))
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "endpoint not found", "")
		} else {
			log.Printf("get endpoint: %v", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", "")
		}
		return
	}
	writeJSON(w, http.StatusOK, endpoint)
}

func (h *Handler) deleteEndpoint(w http.ResponseWriter, r *http.Request) {
	err := h.store.DeleteEndpoint(r.Context(), r.PathValue("id"))
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, repository.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "endpoint not found", "")
	case errors.Is(err, repository.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", "endpoint has events", "")
	default:
		log.Printf("delete endpoint: %v", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", "")
	}
}
