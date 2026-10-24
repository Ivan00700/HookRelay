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

// Open создаёт пул и проверяет соединение. Пул закрывает вызывающий код.
func Open(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, ErrMissingURL
	}

	// 1) Разбираем настройки без вывода строки подключения в ошибку.
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		// Ошибка разбора может содержать пароль, поэтому возвращаем общую ошибку.
		return nil, ErrInvalidConfig
	}

	// 2) Создаём пул и проверяем реальное соединение с PostgreSQL.
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
