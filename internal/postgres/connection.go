package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrMissingURL    = errors.New("DATABASE_URL is required")
	ErrInvalidConfig = errors.New("invalid PostgreSQL connection configuration")
)

// Open creates a pool and verifies a connection. The caller must close the pool.
func Open(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, ErrMissingURL
	}

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		// Parse errors may include the connection string and its password.
		return nil, ErrInvalidConfig
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create PostgreSQL pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	return pool, nil
}
