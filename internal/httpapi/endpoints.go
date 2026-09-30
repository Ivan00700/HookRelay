package httpapi

import (
	"errors"
	"log"
	"net/http"
	"time"

	"hookrelay/internal/domain"
	"hookrelay/internal/repository"
)

func RegisterEndpoints(mux *http.ServeMux, store repository.Store) {
	mux.HandleFunc("POST /api/v1/endpoints", handleCreateEndpoint(store))
	mux.HandleFunc("GET /api/v1/endpoints", handleListEndpoints(store))
	mux.HandleFunc("GET /api/v1/endpoints/{id}", handleGetEndpoint(store))
	mux.HandleFunc("DELETE /api/v1/endpoints/{id}", handleDeleteEndpoint(store))
}

func handleListEndpoints(store repository.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, store.ListEndpoints())
	}
}

func handleCreateEndpoint(store repository.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
			CreatedAt: time.Now().UTC(),
		}
		if err := store.CreateEndpoint(endpoint); err != nil {
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
}

func handleGetEndpoint(store repository.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		endpoint, ok := store.GetEndpoint(r.PathValue("id"))
		if !ok {
			writeError(w, http.StatusNotFound, "not_found", "endpoint not found", "")
			return
		}
		writeJSON(w, http.StatusOK, endpoint)
	}
}

func handleDeleteEndpoint(store repository.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := store.DeleteEndpoint(r.PathValue("id"))
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
}
