package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"hookrelay/internal/dashboard"
	"hookrelay/internal/delivery"
	"hookrelay/internal/httpapi"
	"hookrelay/internal/observability"
	"hookrelay/internal/postgres"
	"hookrelay/internal/repository"
)

// собирает зависимости - запускает HTTP-сервер и дожидается остановки
func Run(stopCtx context.Context) error {
	logger := observability.NewLogger(os.Stderr)
	// подключаем хранилище и проверяем схему очереди
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
	monitor := observability.NewMonitor(logger, store.EventCounts)

	// запускаем воркеры с общим HTTP-клиентом
	pool, err := delivery.NewPool(store, delivery.NewSender(&http.Client{}), 2, monitor)
	if err != nil {
		return fmt.Errorf("create delivery pool: %w", err)
	}

	// регистрируем маршруты и запускаем сервер
	mux := http.NewServeMux()
	handler := httpapi.NewHandler(store, monitor)
	handler.RegisterRoutes(mux)
	dashboard.RegisterRoutes(mux)
	port := httpPort()

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           monitor.HTTP(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}

	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- srv.ListenAndServe()
	}()

	logger.Info("server starting", "port", port)
	// после сигнала завершаем HTTP-запросы - затем активные доставки
	var runErr error
	select {
	case <-stopCtx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := srv.Shutdown(shutdownCtx); err != nil {
			runErr = fmt.Errorf("shutdown HTTP server: %w", err)
			logger.Error("http shutdown failed", "error_kind", observability.ErrorKind(err))
			if closeErr := srv.Close(); closeErr != nil {
				logger.Error("http close failed", "error_kind", observability.ErrorKind(closeErr))
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
	logger.Info("delivery workers stopped")
	return runErr
}
