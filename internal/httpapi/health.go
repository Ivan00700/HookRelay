package httpapi

import (
	"log"
	"net/http"
)

// Проверка здоровья не обращается к БД и сохраняет прежний формат ответа.
func healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if _, err := w.Write([]byte(`{"status":"ok"}`)); err != nil {
		log.Printf("write health response: %v", err)
	}
}
