package httpapi

import (
	"errors"
	"net/http"
	"time"

	"hookrelay/internal/domain"
	"hookrelay/internal/repository"

	"hookrelay/internal/observability"
)

func (h *Handler) RegisterEvents(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/events", withStorageTimeout(h.createEvent))
	mux.HandleFunc("GET /api/v1/events", withStorageTimeout(h.listEvents))
	mux.HandleFunc("GET /api/v1/events/{id}", withStorageTimeout(h.getEvent))
	mux.HandleFunc("GET /api/v1/events/{id}/attempts", withStorageTimeout(h.listAttempts))
	mux.HandleFunc("POST /api/v1/events/{id}/retry", withStorageTimeout(h.retryEvent))
}

func (h *Handler) listAttempts(w http.ResponseWriter, r *http.Request) {
	attempts, err := h.store.ListAttempts(r.Context(), r.PathValue("id"))
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "event not found", "")
		} else {
			observability.Logger(r.Context()).Error("list delivery attempts", "error_kind", observability.ErrorKind(err))
			writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", "")
		}
		return
	}
	writeJSON(w, http.StatusOK, attempts)
}

func (h *Handler) retryEvent(w http.ResponseWriter, r *http.Request) {
	event, err := h.store.RetryEvent(r.Context(), r.PathValue("id"))
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "event not found", "")
		case errors.Is(err, repository.ErrConflict):
			writeError(w, http.StatusConflict, "conflict", "only failed events can be retried", "")
		default:
			observability.Logger(r.Context()).Error("retry event", "error_kind", observability.ErrorKind(err))
			writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", "")
		}
		return
	}
	observability.Logger(r.Context()).Info("event retry requested", "event_id", event.ID)
	writeJSON(w, http.StatusAccepted, event)
}

func (h *Handler) createEvent(w http.ResponseWriter, r *http.Request) {
	// читаем и проверяем входные данные
	var input *domain.CreateEventInput
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
		observability.Logger(r.Context()).Error("validate event", "error_kind", observability.ErrorKind(err))
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", "")
		return
	}

	// формируем запись с ID и временем создания
	id, err := newID()
	if err != nil {
		observability.Logger(r.Context()).Error("generate event ID", "error_kind", observability.ErrorKind(err))
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", "")
		return
	}
	event := domain.Event{
		ID:         id,
		EndpointID: input.EndpointID,
		Type:       input.Type,
		Payload:    input.Payload,
		Status:     domain.DeliveryStatusPending,
		CreatedAt:  time.Now().UTC().Truncate(time.Microsecond),
	}
	// сохраняем запись и возвращаем результат клиенту
	if err := h.store.CreateEvent(r.Context(), event); err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "endpoint not found", "")
		case errors.Is(err, repository.ErrConflict):
			writeError(w, http.StatusConflict, "conflict", "event ID already exists", "")
		default:
			observability.Logger(r.Context()).Error("create event", "error_kind", observability.ErrorKind(err))
			writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", "")
		}
		return
	}
	h.monitor.Accepted()
	observability.Logger(r.Context()).Info("event accepted", "event_id", event.ID, "endpoint_id", event.EndpointID)
	writeJSON(w, http.StatusCreated, event)
}

func (h *Handler) listEvents(w http.ResponseWriter, r *http.Request) {
	events, err := h.store.ListEvents(r.Context())
	if err != nil {
		observability.Logger(r.Context()).Error("list events", "error_kind", observability.ErrorKind(err))
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", "")
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (h *Handler) getEvent(w http.ResponseWriter, r *http.Request) {
	event, err := h.store.GetEvent(r.Context(), r.PathValue("id"))
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "event not found", "")
		} else {
			observability.Logger(r.Context()).Error("get event", "error_kind", observability.ErrorKind(err))
			writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", "")
		}
		return
	}
	writeJSON(w, http.StatusOK, event)
}
