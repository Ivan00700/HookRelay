package testutil

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hookrelay/internal/postgres"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Database creates an isolated, migrated schema and removes it after the test.
func Database(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	rawURL := os.Getenv("TEST_DATABASE_URL")
	if rawURL == "" {
		t.Skip("set TEST_DATABASE_URL to run PostgreSQL integration tests")
	}
	connectionURL, err := url.Parse(rawURL)
	if err != nil || (connectionURL.Scheme != "postgres" && connectionURL.Scheme != "postgresql") {
		t.Fatal("TEST_DATABASE_URL must be a PostgreSQL URL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	admin, err := postgres.Open(ctx, rawURL)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(admin.Close)

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("generate schema name: %v", err)
	}
	schemaName := fmt.Sprintf("hookrelay_test_%x", suffix)
	schema := pgx.Identifier{schemaName}.Sanitize()
	tx, err := admin.Begin(ctx)
	if err != nil {
		t.Fatalf("begin test migration: %v", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	if _, err := tx.Exec(ctx, "SET LOCAL search_path TO "+schema); err != nil {
		t.Fatalf("set test schema: %v", err)
	}
	migrationsDir := filepath.Join("..", "..", "migrations")
	files, err := os.ReadDir(migrationsDir)
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	applied := 0
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".sql") {
			continue
		}
		sql, err := os.ReadFile(filepath.Join(migrationsDir, file.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", file.Name(), err)
		}
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("apply %s: %v", file.Name(), err)
		}
		applied++
	}
	if applied == 0 {
		t.Fatal("no SQL migrations found")
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit test schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := admin.Exec(cleanupCtx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("remove test schema: %v", err)
		}
	})
	query := connectionURL.Query()
	query.Set("search_path", schemaName)
	connectionURL.RawQuery = query.Encode()
	pool, err := postgres.Open(ctx, connectionURL.String())
	if err != nil {
		t.Fatalf("open migrated test schema: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool, connectionURL.String()
}
