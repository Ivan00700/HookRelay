package main

import (
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"hookrelay/internal/observability"
)

func main() {
	logger := observability.NewLogger(os.Stderr)
	slog.SetDefault(logger)
	secret := os.Getenv("WEBHOOK_SECRET")
	if strings.TrimSpace(secret) == "" {
		logger.Error("example receiver requires WEBHOOK_SECRET")
		os.Exit(1)
	}
	address := os.Getenv("RECEIVER_ADDR")
	if address == "" {
		address = "127.0.0.1:9000"
	}
	server := &http.Server{
		Addr:              address,
		Handler:           newHandler(secret),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
	}
	logger.Info("example receiver starting", "address", address)
	if err := server.ListenAndServe(); err != nil {
		logger.Error("example receiver stopped", "error_kind", observability.ErrorKind(err))
		os.Exit(1)
	}
}
