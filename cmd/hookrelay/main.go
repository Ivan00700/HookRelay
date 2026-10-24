package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"hookrelay/internal/observability"
	"hookrelay/internal/orchestrator"
)

func main() {
	logger := observability.NewLogger(os.Stderr)
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := orchestrator.Run(ctx)
	stop()
	if err != nil {
		logger.Error("server stopped", "error_kind", observability.ErrorKind(err))
		os.Exit(1)
	}
}
