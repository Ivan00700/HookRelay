package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"hookrelay/internal/delivery"
	"hookrelay/internal/httpapi"
	"hookrelay/internal/postgres"
	"hookrelay/internal/repository"
)

// Run собирает зависимости, запускает HTTP-сервер и дожидается остановки.
func Run(stopCtx context.Context) error {
	// 1) Подключаем хранилище и проверяем схему очереди.
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

	// 2) Запускаем воркеры с общим HTTP-клиентом.
	pool, err := delivery.NewPool(store, delivery.NewSender(&http.Client{}), 2)
	if err != nil {
		return fmt.Errorf("create delivery pool: %w", err)
	}

	// 3) Регистрируем маршруты и запускаем сервер.
	mux := http.NewServeMux()
	handler := httpapi.NewHandler(store)
	handler.RegisterRoutes(mux)
	port := httpPort()

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
	// 4) После сигнала завершаем HTTP-запросы, затем активные доставки.
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
