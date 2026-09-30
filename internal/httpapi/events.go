package httpapi

import (
	"errors"
	"log"
	"net/http"
	"time"

	"hookrelay/internal/domain"
	"hookrelay/internal/repository"
)

func RegisterEvents(mux *http.ServeMux, store repository.Store) {
	mux.HandleFunc("POST /api/v1/events", handleCreateEvent(store))
	mux.HandleFunc("GET /api/v1/events", handleListEvents(store))
	mux.HandleFunc("GET /api/v1/events/{id}", handleGetEvent(store))
}

func handleCreateEvent(store repository.Store) http.HandlerFunc {
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
			CreatedAt:  time.Now().UTC(),
		}
		if err := store.CreateEvent(event); err != nil {
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
}

func handleListEvents(store repository.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, store.ListEvents())
	}
}

func handleGetEvent(store repository.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		event, ok := store.GetEvent(r.PathValue("id"))
		if !ok {
			writeError(w, http.StatusNotFound, "not_found", "event not found", "")
			return
		}
		writeJSON(w, http.StatusOK, event)
	}
}
