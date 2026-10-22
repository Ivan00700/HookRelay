package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestOpenMissingURL(t *testing.T) {
	pool, err := Open(context.Background(), "  ")
	if pool != nil || !errors.Is(err, ErrMissingURL) {
		t.Fatalf("expected missing URL error and nil pool, got pool=%v err=%v", pool, err)
	}
}

func TestOpenInvalidConfigDoesNotExposePassword(t *testing.T) {
	const password = "private-test-password"
	pool, err := Open(context.Background(),
		"postgres://hookrelay:"+password+"@127.0.0.1:invalid/hookrelay",
	)
	if pool != nil || !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected invalid config error and nil pool, got pool=%v err=%v", pool, err)
	}
	if strings.Contains(err.Error(), password) {
		t.Fatal("connection error exposed the password")
	}
}

func TestOpenCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pool, err := Open(ctx, "postgres://hookrelay@127.0.0.1:1/hookrelay?sslmode=disable")
	if pool != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled context and nil pool, got pool=%v err=%v", pool, err)
	}
}

func TestOpenIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set TEST_DATABASE_URL to run the PostgreSQL integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	defer pool.Close()

	var value int
	if err := pool.QueryRow(ctx, "SELECT 1").Scan(&value); err != nil {
		t.Fatalf("query PostgreSQL: %v", err)
	}
	if value != 1 {
		t.Fatalf("expected 1, got %d", value)
	}
}
