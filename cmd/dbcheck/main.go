package main

import (
	"context"
	"log"
	"os"
	"time"

	"hookrelay/internal/postgres"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err := postgres.Check(ctx, os.Getenv("DATABASE_URL"), os.Stdout)
	cancel()
	if err != nil {
		log.Printf("database check failed: %v", err)
		os.Exit(1)
	}
}
