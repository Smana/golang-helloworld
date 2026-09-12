package database

import (
	"database/sql"
	"time"

	"github.com/XSAM/otelsql"
	_ "github.com/lib/pq"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

func NewConnection(dbURL string) (*sql.DB, error) {
	if dbURL == "" {
		return nil, ErrMissingDatabaseURL
	}

	// Open database connection with OpenTelemetry instrumentation
	// This automatically traces all SQL queries with proper semantic conventions
	db, err := otelsql.Open("postgres", dbURL,
		otelsql.WithAttributes(
			semconv.DBSystemNamePostgreSQL,
		),
		otelsql.WithSpanOptions(otelsql.SpanOptions{
			OmitConnResetSession: true,
			OmitConnPrepare:      true,
			OmitRows:             true,
			OmitConnectorConnect: true,
		}),
	)
	if err != nil {
		return nil, err
	}

	// Register DB stats metrics for connection pool monitoring
	if _, err := otelsql.RegisterDBStatsMetrics(db, otelsql.WithAttributes(
		semconv.DBSystemNamePostgreSQL,
	)); err != nil {
		_ = db.Close() //nolint:errcheck // Connection cleanup in error path
		return nil, err
	}

	if err := db.Ping(); err != nil {
		_ = db.Close() //nolint:errcheck // Connection cleanup in error path
		return nil, err
	}

	// Configure connection pool limits to prevent resource exhaustion
	// Tuned for high-concurrency workloads with occasional slow queries (e.g., benchmarks)
	db.SetMaxOpenConns(25)                 // Limit total open connections (supports ~20 concurrent requests)
	db.SetMaxIdleConns(5)                  // Idle connections for quick reuse (reduced from 10 to save memory)
	db.SetConnMaxLifetime(5 * time.Minute) // Recycle connections periodically
	db.SetConnMaxIdleTime(2 * time.Minute) // Close idle connections after 2min

	return db, nil
}
