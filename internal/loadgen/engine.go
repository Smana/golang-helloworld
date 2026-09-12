package loadgen

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// engine issues operations at a constant arrival rate (open loop). An arrival
// that finds `concurrency` requests in flight is SHED and counted, never
// delayed, so a slow server cannot quietly lower the offered load.
type engine struct {
	next        func() Op
	do          func(context.Context, Op) (int, error)
	rate        float64
	concurrency int
	stats       *stats
	breaker     *breaker // nil: no breaker (incident)
	out         io.Writer
}

func (e *engine) run(ctx context.Context, d time.Duration) error {
	tick := time.NewTicker(time.Duration(float64(time.Second) / e.rate))
	defer tick.Stop()
	progress := time.NewTicker(5 * time.Second)
	defer progress.Stop()
	deadline := time.NewTimer(d)
	defer deadline.Stop()
	sem := make(chan struct{}, e.concurrency)
	var wg sync.WaitGroup
	defer wg.Wait()
	var tripped atomic.Bool
	for {
		select {
		case <-ctx.Done():
			return nil // interrupted: the summary still prints
		case <-deadline.C:
			return nil
		case <-progress.C:
			fmt.Fprintln(e.out, e.stats.progressLine()) //nolint:errcheck // best-effort progress line
		case <-tick.C:
			if tripped.Load() {
				return ErrBreakerTripped
			}
			e.arrive(ctx, sem, &wg, &tripped)
		}
	}
}

// arrive admits one arrival if a concurrency slot is free, else sheds it: a
// slow server must never quietly throttle the offered load.
func (e *engine) arrive(ctx context.Context, sem chan struct{}, wg *sync.WaitGroup, tripped *atomic.Bool) {
	select {
	case sem <- struct{}{}:
		wg.Add(1)
		go e.attempt(ctx, sem, wg, tripped)
	default:
		e.stats.addShed()
	}
}

// attempt runs one operation, records its outcome and feeds the breaker; it
// always releases its concurrency slot and wg count on return.
func (e *engine) attempt(ctx context.Context, sem chan struct{}, wg *sync.WaitGroup, tripped *atomic.Bool) {
	defer wg.Done()
	defer func() { <-sem }()
	op := e.next()
	start := time.Now()
	_, err := e.do(ctx, op)
	if cutByInterrupt(ctx, err) {
		return // cut by the interrupt, not a server failure
	}
	e.stats.add(op, time.Since(start), err != nil)
	if e.breaker != nil && e.breaker.record(err != nil) {
		tripped.Store(true)
	}
}

// cutByInterrupt reports whether err is the context ending mid-request rather
// than the server failing.
func cutByInterrupt(ctx context.Context, err error) bool {
	return err != nil && ctx.Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded))
}
