package httpapi

import (
	"errors"
	"log"
	"net/http"
	"time"

	"hookrelay/internal/domain"
	"hookrelay/internal/repository"
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
			log.Printf("list delivery attempts: %v", err)
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
			log.Printf("retry event: %v", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", "")
		}
		return
	}
	writeJSON(w, http.StatusAccepted, event)
}

func (h *Handler) createEvent(w http.ResponseWriter, r *http.Request) {
	// 1) Читаем и проверяем входные данные.
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
		log.Printf("validate event: %v", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", "")
		return
	}

	// 2) Формируем запись с ID и временем создания.
	id, err := newID()
	if err != nil {
		log.Printf("generate event ID: %v", err)
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
	// 3) Сохраняем запись и возвращаем результат клиенту.
	if err := h.store.CreateEvent(r.Context(), event); err != nil {
		switch {
		case errors.Is(err, repository.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "endpoint not found", "")
		case errors.Is(err, repository.ErrConflict):
			writeError(w, http.StatusConflict, "conflict", "event ID already exists", "")
		default:
			log.Printf("create event: %v", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", "")
		}
		return
	}
	writeJSON(w, http.StatusCreated, event)
}

func (h *Handler) listEvents(w http.ResponseWriter, r *http.Request) {
	events, err := h.store.ListEvents(r.Context())
	if err != nil {
		log.Printf("list events: %v", err)
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
			log.Printf("get event: %v", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", "")
		}
		return
	}
	writeJSON(w, http.StatusOK, event)
}
