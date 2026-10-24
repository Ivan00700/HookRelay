package observability

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"

	"hookrelay/internal/postgres"

	"github.com/jackc/pgx/v5/pgconn"
)

type contextKey int

const (
	requestIDKey contextKey = iota
	loggerKey
)

func NewLogger(output io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(output, nil))
}

func Logger(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(loggerKey).(*slog.Logger); ok {
		return logger
	}
	return slog.Default()
}

func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// сырая ошибка может содержать URL и пароль - в лог попадает только её категория
func ErrorKind(err error) string {
	if errors.Is(err, postgres.ErrMissingURL) {
		return "missing_database_url"
	}
	if errors.Is(err, postgres.ErrInvalidConfig) {
		return "invalid_database_config"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) {
		return "postgres_" + databaseError.Code
	}
	var networkError net.Error
	if errors.As(err, &networkError) {
		return "network"
	}
	return "internal"
}
