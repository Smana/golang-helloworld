package loadgen

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// demoControls mirrors the app's /api/settings/demo document (internal/domain/demo.Controls).
type demoControls struct {
	LatencyMS                int      `json:"latency_ms"`
	LatencyProbability       float64  `json:"latency_probability"`
	LatencyRoutes            []string `json:"latency_routes"`
	ErrorProbability         float64  `json:"error_probability"`
	SlowDBMS                 int      `json:"slow_db_ms"`
	WorkerFailureProbability float64  `json:"worker_failure_probability"`
	WorkerDelayMS            int      `json:"worker_delay_ms"`
}

type phase struct {
	name     string
	length   time.Duration
	controls demoControls
	ops      []weighted
	note     string
}

var uploadHeavy = []weighted{{OpUpload, 60}, {OpList, 20}, {OpThumbnail, 20}}

// incidentPhases is the scripted ~10 minute story told on the dashboards.
var incidentPhases = []phase{
	{"baseline", 2 * time.Minute, demoControls{}, mixedOps, "healthy traffic — note p95, error rate and queue depth"},
	{"latency", 2 * time.Minute, demoControls{LatencyMS: 800, LatencyProbability: 0.5, LatencyRoutes: []string{imagesPath}}, mixedOps,
		"p95 on the gallery API climbs; exemplars lead to slow traces tagged demo.fault=latency"},
	{"errors", 2 * time.Minute, demoControls{ErrorProbability: 0.2}, mixedOps, "a 5xx burst — error ratio, error spans, logs by trace_id"},
	{"worker-slowdown", 3 * time.Minute, demoControls{WorkerDelayMS: 4000}, uploadHeavy,
		"uploads outpace the worker — queue.depth and queue.lag grow, thumbnails lag"},
	{"recovery", time.Minute, demoControls{}, mixedOps, "controls off — the queue drains, latency and errors return to baseline"},
}

func (c *client) setControls(ctx context.Context, dc demoControls) error {
	if dc.LatencyRoutes == nil {
		dc.LatencyRoutes = []string{}
	}
	body, err := json.Marshal(dc)
	if err != nil {
		return err
	}
	status, err := c.send(ctx, http.MethodPut, "/api/settings/demo", body, "application/json")
	if err == nil && status >= 300 {
		err = fmt.Errorf("PUT /api/settings/demo: HTTP %d", status)
	}
	return err
}

func (c *client) resetControls(ctx context.Context) error {
	status, err := c.send(ctx, http.MethodPost, "/api/settings/demo/reset", nil, "")
	if err == nil && status >= 300 {
		err = fmt.Errorf("POST /api/settings/demo/reset: HTTP %d", status)
	}
	return err
}

// runIncident plays the phases and ALWAYS resets the controls on the way out,
// including on Ctrl-C, so a presenter can never leave the app broken.
func runIncident(ctx context.Context, o Options, c *client, st *stats, rnd *lockedRand, out io.Writer) (err error) {
	defer func() {
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if rerr := c.resetControls(rctx); rerr != nil {
			//nolint:errcheck // best-effort diagnostic to the operator; nothing actionable on write failure
			fmt.Fprintf(out, "WARNING: demo controls NOT reset: %v — run: curl -X POST %s/api/settings/demo/reset\n", rerr, o.Target)
			if err == nil {
				err = rerr
			}
			return
		}
		fmt.Fprintln(out, "demo controls reset (all off)") //nolint:errcheck // best-effort status line
	}()
	start := time.Now()
	for i := range incidentPhases {
		p := &incidentPhases[i]
		if ctx.Err() != nil {
			return nil
		}
		if err := c.setControls(ctx, p.controls); err != nil {
			return fmt.Errorf("phase %s: %w", p.name, err)
		}
		//nolint:errcheck // best-effort status line
		fmt.Fprintf(out, "[%s +%-5s] phase %-16s %s\n", time.Now().Format("15:04:05"), time.Since(start).Round(time.Second), p.name, p.note)
		e := &engine{next: picker(p.ops, rnd), do: c.do, rate: o.Rate, concurrency: o.Concurrency, stats: st, out: out}
		if err := e.run(ctx, time.Duration(float64(p.length)*o.PhaseScale)); err != nil {
			return err
		}
	}
	return nil
}
