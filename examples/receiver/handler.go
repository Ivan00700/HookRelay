package main

import (
	"errors"
	"io"
	"log"
	"net/http"

	"hookrelay/internal/signature"
)

func newHandler(secret string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhook", func(w http.ResponseWriter, r *http.Request) {
		// 1) Читаем тело с ограничением размера.
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			var limitErr *http.MaxBytesError
			if errors.As(err, &limitErr) {
				http.Error(w, "webhook body is too large", http.StatusRequestEntityTooLarge)
			} else {
				http.Error(w, "cannot read webhook body", http.StatusBadRequest)
			}
			return
		}

		// 2) Проверяем подпись до обработки события.
		values := r.Header.Values(signature.Header)
		if len(values) != 1 || !signature.Verify(body, secret, values[0]) {
			http.Error(w, "invalid webhook signature", http.StatusUnauthorized)
			return
		}

		// 3) Принимаем событие только после успешной проверки.
		log.Printf("received signed event %q", r.Header.Get("X-HookRelay-Event-ID"))
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}
