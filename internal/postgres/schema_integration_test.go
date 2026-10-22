package postgres

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestSchemaIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set TEST_DATABASE_URL to run the PostgreSQL integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	defer pool.Close()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin schema transaction: %v", err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cleanupCancel()
		if err := tx.Rollback(cleanupCtx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Errorf("rollback schema transaction: %v", err)
		}
	}()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("generate schema name: %v", err)
	}
	schema := pgx.Identifier{fmt.Sprintf("hookrelay_test_%x", suffix)}.Sanitize()
	if _, err := tx.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL search_path TO "+schema); err != nil {
		t.Fatalf("set test schema: %v", err)
	}

	migrationsDir := filepath.Join("..", "..", "migrations")
	files, err := os.ReadDir(migrationsDir)
	if err != nil {
		t.Fatalf("read migrations directory: %v", err)
	}
	applied := 0
	// ReadDir returns entries sorted by name, matching the migration numbering.
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".sql") {
			continue
		}
		sql, err := os.ReadFile(filepath.Join(migrationsDir, file.Name()))
		if err != nil {
			t.Fatalf("read migration %s: %v", file.Name(), err)
		}
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("apply migration %s: %v", file.Name(), err)
		}
		applied++
	}
	if applied == 0 {
		t.Fatal("no SQL migrations found")
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO endpoints (id, name, url, secret, created_at)
		VALUES ('endpoint-1', 'receiver', 'http://example.test/webhook', 'test-only', CURRENT_TIMESTAMP);
		INSERT INTO events (id, endpoint_id, type, payload, status, created_at)
		VALUES ('event-1', 'endpoint-1', 'order.created', '{}', 'pending', CURRENT_TIMESTAMP),
		       ('event-2', 'endpoint-1', 'order.created', '{}', 'pending', CURRENT_TIMESTAMP);
	`)
	if err != nil {
		t.Fatalf("insert test endpoint and events: %v", err)
	}
	var attemptID int64
	var resultIsEmpty bool
	err = tx.QueryRow(ctx, `
		INSERT INTO delivery_attempts (event_id, attempt_number, started_at)
		VALUES ('event-1', 1, CURRENT_TIMESTAMP)
		RETURNING id, finished_at IS NULL AND http_status IS NULL AND error_message IS NULL
	`).Scan(&attemptID, &resultIsEmpty)
	if err != nil || attemptID <= 0 || !resultIsEmpty {
		t.Fatalf("create unfinished attempt: id=%d resultIsEmpty=%t err=%v", attemptID, resultIsEmpty, err)
	}

	cases := []struct {
		name string
		sql  string
		code string
	}{
		{"duplicate endpoint", `INSERT INTO endpoints VALUES ('endpoint-1', 'receiver', 'http://example.test', 'test-only', CURRENT_TIMESTAMP)`, "23505"},
		{"missing endpoint name", `INSERT INTO endpoints VALUES ('endpoint-2', NULL, 'http://example.test', 'test-only', CURRENT_TIMESTAMP)`, "23502"},
		{"event without endpoint", `INSERT INTO events VALUES ('event-3', 'missing', 'order.created', '{}', 'pending', CURRENT_TIMESTAMP)`, "23503"},
		{"invalid event status", `INSERT INTO events VALUES ('event-3', 'endpoint-1', 'order.created', '{}', 'unknown', CURRENT_TIMESTAMP)`, "23514"},
		{"invalid JSON payload", `INSERT INTO events VALUES ('event-3', 'endpoint-1', 'order.created', '{broken', 'pending', CURRENT_TIMESTAMP)`, "22P02"},
		{"delete referenced endpoint", `DELETE FROM endpoints WHERE id = 'endpoint-1'`, "23503"},
		{"duplicate attempt number", `INSERT INTO delivery_attempts (event_id, attempt_number, started_at) VALUES ('event-1', 1, CURRENT_TIMESTAMP)`, "23505"},
		{"zero attempt number", `INSERT INTO delivery_attempts (event_id, attempt_number, started_at) VALUES ('event-1', 0, CURRENT_TIMESTAMP)`, "23514"},
		{"attempt without event", `INSERT INTO delivery_attempts (event_id, attempt_number, started_at) VALUES ('missing', 1, CURRENT_TIMESTAMP)`, "23503"},
		{"invalid HTTP status", `INSERT INTO delivery_attempts (event_id, attempt_number, started_at, http_status) VALUES ('event-1', 2, CURRENT_TIMESTAMP, 999)`, "23514"},
		{"finish before start", `INSERT INTO delivery_attempts (event_id, attempt_number, started_at, finished_at) VALUES ('event-1', 2, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP - INTERVAL '1 second')`, "23514"},
		{"delete event with history", `DELETE FROM events WHERE id = 'event-1'`, "23503"},
		{"attempt number per event", `INSERT INTO delivery_attempts (event_id, attempt_number, started_at) VALUES ('event-2', 1, CURRENT_TIMESTAMP)`, ""},
		{"successful delivery result", `INSERT INTO delivery_attempts (event_id, attempt_number, started_at, finished_at, http_status) VALUES ('event-1', 2, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 204)`, ""},
		{"network error without HTTP status", `INSERT INTO delivery_attempts (event_id, attempt_number, started_at, finished_at, error_message) VALUES ('event-1', 2, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, 'timeout')`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// A nested transaction uses a savepoint, so expected SQL errors are isolated.
			checkTx, err := tx.Begin(ctx)
			if err != nil {
				t.Fatalf("begin constraint check: %v", err)
			}
			defer func() {
				if err := checkTx.Rollback(ctx); err != nil {
					t.Errorf("rollback constraint check: %v", err)
				}
			}()
			_, err = checkTx.Exec(ctx, tc.sql)
			if tc.code == "" {
				if err != nil {
					t.Fatalf("expected valid data to be accepted: %v", err)
				}
				return
			}
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != tc.code {
				t.Fatalf("expected PostgreSQL error %s, got %v", tc.code, err)
			}
		})
	}
}
