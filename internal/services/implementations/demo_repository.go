package implementations

import (
	"context"
	"database/sql"

	"github.com/lib/pq"

	"image-gallery/internal/domain/demo"
)

// DemoRepository persists the single demo_controls row (id = 1).
type DemoRepository struct{ db *sql.DB }

// NewDemoRepository builds a repository on db.
func NewDemoRepository(db *sql.DB) *DemoRepository { return &DemoRepository{db: db} }

// Get reads the demo_controls row.
func (r *DemoRepository) Get(ctx context.Context) (demo.Controls, error) {
	var c demo.Controls
	err := r.db.QueryRowContext(ctx, `
		SELECT latency_ms, latency_probability, latency_routes, error_probability, slow_db_ms,
		       worker_failure_probability, worker_delay_ms, updated_at
		  FROM demo_controls WHERE id = 1`).Scan(&c.LatencyMS, &c.LatencyProbability, pq.Array(&c.LatencyRoutes),
		&c.ErrorProbability, &c.SlowDBMS, &c.WorkerFailureProbability, &c.WorkerDelayMS, &c.UpdatedAt)
	return c, err
}

// Save writes c to the demo_controls row.
func (r *DemoRepository) Save(ctx context.Context, c demo.Controls) (demo.Controls, error) {
	if c.LatencyRoutes == nil {
		c.LatencyRoutes = []string{}
	}
	err := r.db.QueryRowContext(ctx, `
		UPDATE demo_controls SET latency_ms = $1, latency_probability = $2, latency_routes = $3,
		       error_probability = $4, slow_db_ms = $5, worker_failure_probability = $6,
		       worker_delay_ms = $7, updated_at = NOW()
		 WHERE id = 1 RETURNING updated_at`,
		c.LatencyMS, c.LatencyProbability, pq.Array(c.LatencyRoutes), c.ErrorProbability, c.SlowDBMS,
		c.WorkerFailureProbability, c.WorkerDelayMS).Scan(&c.UpdatedAt)
	return c, err
}

// SleepInDB runs pg_sleep so an injected slow query shows up as a real, slow DB span.
func SleepInDB(ctx context.Context, db *sql.DB, seconds float64) error {
	_, err := db.ExecContext(ctx, "SELECT pg_sleep($1)", seconds)
	return err
}
