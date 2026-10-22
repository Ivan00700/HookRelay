package httpapi

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"

	"hookrelay/internal/domain"
	"hookrelay/internal/repository"
)

type EventPublisher interface {
	Submit(eventID string) error
}

func RegisterEvents(mux *http.ServeMux, store repository.Store, publisher EventPublisher) {
	mux.HandleFunc("POST /api/v1/events", withStorageTimeout(handleCreateEvent(store, publisher)))
	mux.HandleFunc("GET /api/v1/events", withStorageTimeout(handleListEvents(store)))
	mux.HandleFunc("GET /api/v1/events/{id}", withStorageTimeout(handleGetEvent(store)))
}

func handleCreateEvent(store repository.Store, publisher EventPublisher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
		if err := store.CreateEvent(r.Context(), event); err != nil {
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
		if err := publisher.Submit(event.ID); err != nil {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if rollbackErr := store.DeleteEvent(cleanupCtx, event.ID); rollbackErr != nil {
				log.Printf("rollback event %s after enqueue error: %v", event.ID, rollbackErr)
			}
			log.Printf("enqueue event %s: %v", event.ID, err)
			writeError(w, http.StatusServiceUnavailable, "delivery_unavailable", "delivery queue is unavailable", "")
			return
		}
		writeJSON(w, http.StatusCreated, event)
	}
}

func handleListEvents(store repository.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		events, err := store.ListEvents(r.Context())
		if err != nil {
			log.Printf("list events: %v", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", "")
			return
		}
		writeJSON(w, http.StatusOK, events)
	}
}

func handleGetEvent(store repository.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		event, err := store.GetEvent(r.Context(), r.PathValue("id"))
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
}
