package repository

import "github.com/jackc/pgx/v5/pgxpool"

// PostgresStore выполняет операции хранилища через общий пул соединений.
type PostgresStore struct {
	pool *pgxpool.Pool
}

var _ Store = (*PostgresStore)(nil)

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{
		pool: pool,
	}
}
