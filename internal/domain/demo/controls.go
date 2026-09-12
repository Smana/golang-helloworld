// Package demo holds the fault-injection controls of the observability demo.
package demo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Fault types: the demo.fault attribute and counter label.
const (
	FaultLatency        = "latency"
	FaultError          = "error"
	FaultSlowDB         = "slow_db"
	FaultWorkerFailure  = "worker_failure"
	FaultWorkerSlowdown = "worker_slowdown"
)

// ErrInvalidControls wraps every validation failure.
var ErrInvalidControls = errors.New("invalid demo controls")

// Controls is the single demo_controls row. The zero value is "everything off".
type Controls struct {
	LatencyMS                int       `json:"latency_ms"`
	LatencyProbability       float64   `json:"latency_probability"`
	LatencyRoutes            []string  `json:"latency_routes"`
	ErrorProbability         float64   `json:"error_probability"`
	SlowDBMS                 int       `json:"slow_db_ms"`
	WorkerFailureProbability float64   `json:"worker_failure_probability"`
	WorkerDelayMS            int       `json:"worker_delay_ms"`
	UpdatedAt                time.Time `json:"updated_at"`
}

// Validate mirrors the table's CHECK constraints.
func (c Controls) Validate() error {
	prob := func(name string, p float64) error {
		if p < 0 || p > 1 {
			return fmt.Errorf("%w: %s must be within [0,1], got %v", ErrInvalidControls, name, p)
		}
		return nil
	}
	ms := func(name string, v, max int) error {
		if v < 0 || v > max {
			return fmt.Errorf("%w: %s must be within [0,%d], got %d", ErrInvalidControls, name, max, v)
		}
		return nil
	}
	return errors.Join(
		ms("latency_ms", c.LatencyMS, 30000), prob("latency_probability", c.LatencyProbability),
		prob("error_probability", c.ErrorProbability), ms("slow_db_ms", c.SlowDBMS, 30000),
		prob("worker_failure_probability", c.WorkerFailureProbability), ms("worker_delay_ms", c.WorkerDelayMS, 60000))
}

// Active reports whether any control can inject a fault.
func (c Controls) Active() bool {
	return (c.LatencyMS > 0 && c.LatencyProbability > 0) || c.ErrorProbability > 0 || c.SlowDBMS > 0 ||
		c.WorkerFailureProbability > 0 || c.WorkerDelayMS > 0
}

// MatchesLatencyRoute reports whether path is subject to injected latency.
func (c Controls) MatchesLatencyRoute(path string) bool {
	if len(c.LatencyRoutes) == 0 {
		return true
	}
	for _, p := range c.LatencyRoutes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// Repository persists the controls row.
type Repository interface {
	Get(ctx context.Context) (Controls, error)
	Save(ctx context.Context, c Controls) (Controls, error)
}
