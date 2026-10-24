package postgres

import (
	"context"
	"errors"
	"net"
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

func TestOpenDeadline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for connection: %v", err)
	}
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// Принимаем TCP-соединение, но не отвечаем на приветствие PostgreSQL.
		<-release
	}()
	t.Cleanup(func() {
		close(release)
		listener.Close()
		<-done
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	pool, err := Open(ctx, "postgres://hookrelay@"+listener.Addr().String()+"/hookrelay?sslmode=disable")
	if pool != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline error and nil pool, got pool=%v err=%v", pool, err)
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
