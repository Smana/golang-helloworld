package loadgen

import (
	"errors"
	"sync"
)

// ErrBreakerTripped ends a run whose error rate stayed above the threshold.
var ErrBreakerTripped = errors.New("circuit breaker tripped: sustained error rate above 50 % — stopping so the load does not pile onto a failing app")

// breaker trips when more than threshold of the last `size` results failed,
// once at least `size/2` results are in.
type breaker struct {
	mu        sync.Mutex
	ring      []bool
	next      int
	filled    int
	failures  int
	threshold float64
}

func newBreaker(size int, threshold float64) *breaker {
	return &breaker{ring: make([]bool, size), threshold: threshold}
}

func (b *breaker) record(failed bool) (tripped bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.filled == len(b.ring) && b.ring[b.next] {
		b.failures--
	}
	b.ring[b.next] = failed
	if failed {
		b.failures++
	}
	b.next = (b.next + 1) % len(b.ring)
	if b.filled < len(b.ring) {
		b.filled++
	}
	return b.filled >= len(b.ring)/2 && float64(b.failures)/float64(b.filled) > b.threshold
}
