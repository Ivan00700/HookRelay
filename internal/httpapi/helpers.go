package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
)

const maxRequestBodyBytes = 1 << 20

type apiError struct {
	status  int
	code    string
	message string
}

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Field   string `json:"field,omitempty"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) *apiError {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		return classifyDecodeError(err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return classifyDecodeError(err)
		}
		return &apiError{http.StatusBadRequest, "invalid_json", "request body must contain exactly one JSON object"}
	}
	return nil
}

func classifyDecodeError(err error) *apiError {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return &apiError{http.StatusRequestEntityTooLarge, "invalid_json", "request body exceeds 1 MiB"}
	}
	return &apiError{http.StatusBadRequest, "invalid_json", "request body must be a valid JSON object"}
}

func newID() (string, error) {
	var randomBytes [16]byte
	if _, err := rand.Read(randomBytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(randomBytes[:]), nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		log.Printf("encode JSON response: %v", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", "")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(append(body, '\n')); err != nil {
		log.Printf("write JSON response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, code, message, field string) {
	response := errorBody{}
	response.Error.Code = code
	response.Error.Field = field
	response.Error.Message = message
	writeJSON(w, status, response)
}
