package repository

import (
	"context"

	"hookrelay/internal/domain"
)

// считаем текущее состояние в БД - в память приложения копия очереди не переносится
func (s *PostgresStore) EventCounts(ctx context.Context) (map[domain.DeliveryStatus]int64, error) {
	rows, err := s.pool.Query(ctx, `SELECT status, COUNT(*) FROM events GROUP BY status`)
	if err != nil {
		return nil, databaseError("count events", err, ErrConflict)
	}
	defer rows.Close()
	counts := make(map[domain.DeliveryStatus]int64)
	for rows.Next() {
		var status domain.DeliveryStatus
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			return nil, databaseError("scan event count", err, ErrConflict)
		}
		counts[status] = count
	}
	if err := rows.Err(); err != nil {
		return nil, databaseError("read event counts", err, ErrConflict)
	}
	return counts, nil
}

func (s *PostgresStore) CheckReady(ctx context.Context) error {
	if err := s.pool.Ping(ctx); err != nil {
		return databaseError("check database readiness", err, ErrConflict)
	}
	return s.CheckQueueSchema(ctx)
}
