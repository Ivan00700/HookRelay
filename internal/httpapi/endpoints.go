package httpapi

import (
	"errors"
	"net/http"
	"time"

	"hookrelay/internal/domain"
	"hookrelay/internal/repository"

	"hookrelay/internal/observability"
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
		observability.Logger(r.Context()).Error("list endpoints", "error_kind", observability.ErrorKind(err))
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", "")
		return
	}
	writeJSON(w, http.StatusOK, endpoints)
}

func (h *Handler) createEndpoint(w http.ResponseWriter, r *http.Request) {
	// читаем и проверяем входные данные
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
		observability.Logger(r.Context()).Error("validate endpoint", "error_kind", observability.ErrorKind(err))
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", "")
		return
	}

	// формируем запись с ID и временем создания
	id, err := newID()
	if err != nil {
		observability.Logger(r.Context()).Error("generate endpoint ID", "error_kind", observability.ErrorKind(err))
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
	// сохраняем запись и возвращаем результат клиенту
	if err := h.store.CreateEndpoint(r.Context(), endpoint); err != nil {
		if errors.Is(err, repository.ErrConflict) {
			writeError(w, http.StatusConflict, "conflict", "endpoint ID already exists", "")
			return
		}
		observability.Logger(r.Context()).Error("create endpoint", "error_kind", observability.ErrorKind(err))
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
			observability.Logger(r.Context()).Error("get endpoint", "error_kind", observability.ErrorKind(err))
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
		observability.Logger(r.Context()).Error("delete endpoint", "error_kind", observability.ErrorKind(err))
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", "")
	}
}
