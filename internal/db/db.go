package db

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
)

// Open connects to Postgres via lib/pq (pure Go, the project's only
// external dependency).
func Open(dsn string) (*sql.DB, error) {
	conn, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening db: %w", err)
	}
	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("pinging db: %w", err)
	}
	// Conservative defaults for a single-instance, ~10-user deployment.
	// Each idle connection is a Postgres backend holding a few MB on a 2 GB
	// server shared with other services: keep only two warm, and close
	// them after a few quiet minutes.
	conn.SetMaxOpenConns(10)
	conn.SetMaxIdleConns(2)
	conn.SetConnMaxIdleTime(5 * time.Minute)
	return conn, nil
}
