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
	mux.HandleFunc("POST /api/v1/endpoints", withStorageTimeout(handleCreateEndpoint(store)))
	mux.HandleFunc("GET /api/v1/endpoints", withStorageTimeout(handleListEndpoints(store)))
	mux.HandleFunc("GET /api/v1/endpoints/{id}", withStorageTimeout(handleGetEndpoint(store)))
	mux.HandleFunc("DELETE /api/v1/endpoints/{id}", withStorageTimeout(handleDeleteEndpoint(store)))
}

func handleListEndpoints(store repository.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		endpoints, err := store.ListEndpoints(r.Context())
		if err != nil {
			log.Printf("list endpoints: %v", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", "")
			return
		}
		writeJSON(w, http.StatusOK, endpoints)
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
			CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
		}
		if err := store.CreateEndpoint(r.Context(), endpoint); err != nil {
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
		endpoint, err := store.GetEndpoint(r.Context(), r.PathValue("id"))
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
}

func handleDeleteEndpoint(store repository.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := store.DeleteEndpoint(r.Context(), r.PathValue("id"))
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
