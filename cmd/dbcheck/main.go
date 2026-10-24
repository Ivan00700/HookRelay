package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"hookrelay/internal/observability"
	"hookrelay/internal/postgres"
)

func main() {
	logger := observability.NewLogger(os.Stderr)
	slog.SetDefault(logger)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err := postgres.Check(ctx, os.Getenv("DATABASE_URL"), os.Stdout)
	cancel()
	if err != nil {
		logger.Error("database check failed", "error_kind", observability.ErrorKind(err))
		os.Exit(1)
	}
}
