package loadgen

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

// Summary is printed at the end of a run.
type Summary struct {
	Requests, Errors, Shed int
	Elapsed                time.Duration
	Rate, ErrorPct         float64
	P50, P95, P99          time.Duration
	ByOp                   map[Op]int
}

func (s Summary) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n== loadgen summary ==\nrequests %d in %s (%.1f req/s)  errors %d (%.2f %%)  shed %d\n",
		s.Requests, s.Elapsed.Round(time.Second), s.Rate, s.Errors, s.ErrorPct, s.Shed)
	fmt.Fprintf(&b, "client latency  p50 %s  p95 %s  p99 %s\n", s.P50.Round(time.Millisecond), s.P95.Round(time.Millisecond), s.P99.Round(time.Millisecond))
	ops := make([]string, 0, len(s.ByOp))
	for op, n := range s.ByOp {
		ops = append(ops, fmt.Sprintf("%s=%d", op, n))
	}
	sort.Strings(ops)
	fmt.Fprintf(&b, "by op  %s\n", strings.Join(ops, "  "))
	return b.String()
}

type stats struct {
	mu     sync.Mutex
	start  time.Time
	durs   []time.Duration
	errors int
	shed   int
	byOp   map[Op]int
}

func newStats() *stats { return &stats{start: time.Now(), byOp: map[Op]int{}} }

func (s *stats) add(op Op, d time.Duration, failed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.durs = append(s.durs, d)
	s.byOp[op]++
	if failed {
		s.errors++
	}
}

func (s *stats) addShed() { s.mu.Lock(); s.shed++; s.mu.Unlock() }

func (s *stats) summary() Summary {
	s.mu.Lock()
	defer s.mu.Unlock()
	sorted := append([]time.Duration(nil), s.durs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	el := time.Since(s.start)
	sum := Summary{Requests: len(sorted), Errors: s.errors, Shed: s.shed, Elapsed: el, ByOp: map[Op]int{},
		P50: percentile(sorted, 0.50), P95: percentile(sorted, 0.95), P99: percentile(sorted, 0.99)}
	for k, v := range s.byOp {
		sum.ByOp[k] = v
	}
	if el > 0 {
		sum.Rate = float64(len(sorted)) / el.Seconds()
	}
	if len(sorted) > 0 {
		sum.ErrorPct = 100 * float64(s.errors) / float64(len(sorted))
	}
	return sum
}

func (s *stats) progressLine() string {
	sum := s.summary()
	return fmt.Sprintf("t=%-6s req=%-6d rate=%5.1f/s err=%5.2f%% p95=%-8s shed=%d",
		sum.Elapsed.Round(time.Second), sum.Requests, sum.Rate, sum.ErrorPct, sum.P95.Round(time.Millisecond), sum.Shed)
}

// percentile of an ascending slice (nearest-rank).
func percentile(sorted []time.Duration, q float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	i := int(math.Ceil(q*float64(len(sorted)))) - 1
	return sorted[max(0, min(i, len(sorted)-1))]
}
