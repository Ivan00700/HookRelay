package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"hookrelay/internal/orchestrator"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := orchestrator.Run(ctx)
	stop()
	if err != nil {
		log.Printf("server stopped: %v", err)
		os.Exit(1)
	}
}
