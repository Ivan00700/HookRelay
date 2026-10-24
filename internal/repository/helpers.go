package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"hookrelay/internal/domain"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func scanEndpoint(row pgx.Row) (domain.Endpoint, error) {
	var endpoint domain.Endpoint
	if err := row.Scan(&endpoint.ID, &endpoint.Name, &endpoint.URL, &endpoint.Secret, &endpoint.CreatedAt); err != nil {
		return domain.Endpoint{}, err
	}
	endpoint.CreatedAt = endpoint.CreatedAt.UTC()
	return endpoint, nil
}

func scanEvent(row pgx.Row) (domain.Event, error) {
	var event domain.Event
	if err := row.Scan(&event.ID, &event.EndpointID, &event.Type, &event.Payload, &event.Status, &event.CreatedAt); err != nil {
		return domain.Event{}, err
	}
	event.CreatedAt = event.CreatedAt.UTC()
	return event, nil
}

func databaseError(operation string, err, foreignKeyError error) error {
	// приводим ошибки PostgreSQL к общим ошибкам репозитория
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	} else {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case "23505":
				err = ErrConflict
			case "23503":
				err = foreignKeyError
			}
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}

// откат получает свой контекст - контекст запроса уже мог завершиться
func rollback(tx pgx.Tx) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}
