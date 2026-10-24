package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"hookrelay/internal/delivery"
	"hookrelay/internal/httpapi"
	"hookrelay/internal/postgres"
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
	stopCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(stopCtx)
	stopSignals()
	if err != nil {
		log.Printf("server stopped: %v", err)
		os.Exit(1)
	}
}

func run(stopCtx context.Context) error {
	startupCtx, cancel := context.WithTimeout(stopCtx, 5*time.Second)
	database, err := postgres.Open(startupCtx, os.Getenv("DATABASE_URL"))
	if err != nil {
		cancel()
		return fmt.Errorf("open storage: %w", err)
	}
	defer database.Close()
	store := repository.NewPostgresStore(database)
	err = store.CheckQueueSchema(startupCtx)
	cancel()
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)

	port := os.Getenv("HTTP_PORT")
	if port == "" {
		port = "8080"
	}

	pool, err := delivery.NewPool(store, delivery.NewSender(&http.Client{}), 2)
	if err != nil {
		return fmt.Errorf("create delivery pool: %w", err)
	}
	httpapi.RegisterEndpoints(mux, store)
	httpapi.RegisterEvents(mux, store)

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- srv.ListenAndServe()
	}()

	log.Printf("starting server on http://localhost:%s", port)
	var runErr error
	select {
	case <-stopCtx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := srv.Shutdown(shutdownCtx); err != nil {
			runErr = fmt.Errorf("shutdown HTTP server: %w", err)
			log.Printf("graceful HTTP shutdown: %v", err)
			if closeErr := srv.Close(); closeErr != nil {
				log.Printf("close HTTP server: %v", closeErr)
			}
		}
		cancel()
		<-serveErrors
	case err := <-serveErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			runErr = err
		}
	}
	pool.Close()
	log.Print("delivery workers stopped")
	return runErr
}
