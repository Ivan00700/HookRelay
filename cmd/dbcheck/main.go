package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"hookrelay/internal/postgres"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	err := run(ctx, os.Getenv("DATABASE_URL"), os.Stdout)
	cancel()
	if err != nil {
		log.Printf("database check failed: %v", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, databaseURL string, output io.Writer) error {
	pool, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	var database, user, version string
	err = pool.QueryRow(ctx,
		"SELECT current_database(), current_user, current_setting('server_version')",
	).Scan(&database, &user, &version)
	if err != nil {
		return fmt.Errorf("read PostgreSQL server information: %w", err)
	}
	_, err = fmt.Fprintf(output,
		"PostgreSQL connection OK: database=%s user=%s version=%s\n",
		database, user, version,
	)
	return err
}
