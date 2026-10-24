package main

import (
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"hookrelay/internal/signature"
)

func main() {
	secret := os.Getenv("WEBHOOK_SECRET")
	if strings.TrimSpace(secret) == "" {
		log.Fatal("set WEBHOOK_SECRET to the secret of the sender endpoint")
	}
	server := &http.Server{
		Addr:              "127.0.0.1:9000",
		Handler:           newHandler(secret),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
	}
	log.Print("example receiver listening on http://127.0.0.1:9000/webhook")
	log.Fatal(server.ListenAndServe())
}

func newHandler(secret string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhook", func(w http.ResponseWriter, r *http.Request) {
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
		values := r.Header.Values(signature.Header)
		if len(values) != 1 || !signature.Verify(body, secret, values[0]) {
			http.Error(w, "invalid webhook signature", http.StatusUnauthorized)
			return
		}
		log.Printf("received signed event %q", r.Header.Get("X-HookRelay-Event-ID"))
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}
