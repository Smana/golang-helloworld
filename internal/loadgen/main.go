package loadgen

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"image-gallery/internal/observability"
)

// Main is `image-gallery loadgen`; it returns the exit code.
func Main(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	o, err := parseOptions(args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(stderr, "loadgen:", err) //nolint:errcheck // best-effort diagnostic on the way out
		return 2
	}
	defer setupTelemetry(ctx, stderr)()
	//nolint:errcheck // best-effort status line
	fmt.Fprintf(stdout, "loadgen: %s -> %s at %.1f req/s (concurrency %d, seed %d)\n", o.Scenario, o.Target, o.Rate, o.Concurrency, o.Seed)
	sum, err := Run(ctx, o, stdout)
	fmt.Fprint(stdout, sum) //nolint:errcheck // best-effort summary output
	if err != nil {
		fmt.Fprintln(stderr, "loadgen:", err) //nolint:errcheck // best-effort diagnostic on the way out
		return 1
	}
	return 0
}

// Run executes one scenario and returns its summary.
func Run(ctx context.Context, o Options, out io.Writer) (Summary, error) {
	if err := o.validate(); err != nil {
		return Summary{}, err
	}
	rnd := newLockedRand(o.Seed)
	c := newClient(o.Target, o.Scenario, rnd)
	st := newStats()
	var err error
	if o.Scenario == scenarioIncident {
		err = runIncident(ctx, o, c, st, rnd, out)
	} else {
		e := &engine{next: picker(scenarios[o.Scenario], rnd), do: c.do, rate: o.Rate, concurrency: o.Concurrency,
			stats: st, breaker: newBreaker(100, 0.5), out: out}
		err = e.run(ctx, o.Duration)
	}
	return st.summary(), err
}

// setupTelemetry exports spans and client metrics when OTEL_* endpoints are
// set; otherwise it only installs the W3C propagator (summary-only mode).
func setupTelemetry(ctx context.Context, stderr io.Writer) func() {
	otel.SetTextMapPropagator(propagation.TraceContext{})
	tracesEP, metricsEP := os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"), os.Getenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT")
	if tracesEP == "" && metricsEP == "" {
		return func() {}
	}
	cfg := observability.LoadConfig()
	if os.Getenv("OTEL_SERVICE_NAME") == "" {
		cfg.ServiceName = "image-gallery-loadgen"
	}
	cfg.TracesEnabled, cfg.MetricsEnabled = tracesEP != "", metricsEP != ""
	p, err := observability.NewProvider(ctx, cfg, nil)
	if err != nil {
		fmt.Fprintln(stderr, "loadgen: telemetry disabled:", err) //nolint:errcheck // best-effort diagnostic
		return func() {}
	}
	return func() {
		fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_ = p.ForceFlush(fctx) //nolint:errcheck // best-effort flush before shutdown
		_ = p.Shutdown(fctx)   //nolint:errcheck // best-effort shutdown
	}
}
