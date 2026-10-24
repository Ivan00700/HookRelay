package postgres

import (
	"context"
	"fmt"
	"io"
)

// Check проверяет соединение и выводит сведения о PostgreSQL.
func Check(ctx context.Context, databaseURL string, output io.Writer) error {
	pool, err := Open(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	var database, user, version string
	err = pool.QueryRow(ctx,
		"SELECT current_database(), current_user, current_setting('server_version')",
	).Scan(&database, &user, &version)
	if err != nil {
		return fmt.Errorf("read PostgreSQL server information: %w", err)
	}
	_, err = fmt.Fprintf(output,
		"PostgreSQL connection OK: database=%s user=%s version=%s\n",
		database, user, version,
	)
	return err
}
