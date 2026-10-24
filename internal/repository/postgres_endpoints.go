package repository

import (
	"context"

	"hookrelay/internal/domain"
)

func (s *PostgresStore) CreateEndpoint(ctx context.Context, endpoint domain.Endpoint) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO endpoints (id, name, url, secret, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, endpoint.ID, endpoint.Name, endpoint.URL, endpoint.Secret, endpoint.CreatedAt)
	return databaseError("create endpoint", err, ErrConflict)
}

func (s *PostgresStore) GetEndpoint(ctx context.Context, id string) (domain.Endpoint, error) {
	endpoint, err := scanEndpoint(s.pool.QueryRow(ctx, `
		SELECT id, name, url, secret, created_at FROM endpoints WHERE id = $1
	`, id))
	return endpoint, databaseError("get endpoint", err, ErrConflict)
}

func (s *PostgresStore) ListEndpoints(ctx context.Context) ([]domain.Endpoint, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, url, secret, created_at FROM endpoints ORDER BY id`)
	if err != nil {
		return nil, databaseError("list endpoints", err, ErrConflict)
	}
	defer rows.Close()
	endpoints := make([]domain.Endpoint, 0)
	for rows.Next() {
		endpoint, err := scanEndpoint(rows)
		if err != nil {
			return nil, databaseError("scan endpoint", err, ErrConflict)
		}
		endpoints = append(endpoints, endpoint)
	}
	if err := rows.Err(); err != nil {
		return nil, databaseError("read endpoints", err, ErrConflict)
	}
	return endpoints, nil
}

func (s *PostgresStore) DeleteEndpoint(ctx context.Context, id string) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM endpoints WHERE id = $1`, id)
	if err != nil {
		return databaseError("delete endpoint", err, ErrConflict)
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
