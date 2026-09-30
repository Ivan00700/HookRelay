package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"hookrelay/internal/delivery"
	"hookrelay/internal/httpapi"
	"hookrelay/internal/repository"
)

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

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)

	port := os.Getenv("HTTP_PORT")
	if port == "" {
		port = "8080"
	}

	store := repository.NewMemoryStore()
	pool, err := delivery.NewPool(store, delivery.NewSender(&http.Client{}), 2, 64)
	if err != nil {
		log.Fatalf("create delivery pool: %v", err)
	}
	httpapi.RegisterEndpoints(mux, store)
	httpapi.RegisterEvents(mux, store, pool)

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	stopCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- srv.ListenAndServe()
	}()

	log.Printf("starting server on http://localhost:%s", port)
	select {
	case <-stopCtx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("graceful HTTP shutdown: %v", err)
			if closeErr := srv.Close(); closeErr != nil {
				log.Printf("close HTTP server: %v", closeErr)
			}
		}
		cancel()
		<-serveErrors
	case err := <-serveErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Printf("HTTP server stopped: %v", err)
		}
	}
	pool.Close()
	log.Print("delivery workers stopped")
}
