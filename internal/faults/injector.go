package faults

import (
	"context"
	"errors"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"image-gallery/internal/domain/demo"
	obs "image-gallery/internal/observability"
)

// Source provides the current controls (the cached Service in production).
type Source interface {
	Get(ctx context.Context) (demo.Controls, error)
}

// Injector turns the controls into faults. Every fault it injects is visible
// three ways: the span attribute demo.fault, a warn log line, and the counter
// demo.faults.injected.
type Injector struct {
	src    Source
	rnd    func() float64
	sleep  func(context.Context, time.Duration) error
	faults metric.Int64Counter
	log    *obs.Logger
}

// InjectorOption customizes an Injector (tests make it deterministic).
type InjectorOption func(*Injector)

// WithRand replaces the random source.
func WithRand(f func() float64) InjectorOption { return func(i *Injector) { i.rnd = f } }

// WithSleep replaces the sleep function.
func WithSleep(f func(context.Context, time.Duration) error) InjectorOption {
	return func(i *Injector) { i.sleep = f }
}

// NewInjector builds an Injector; log may be nil.
//
//nolint:errcheck // instrument name is a constant; the SDK returns a usable instrument alongside any error
func NewInjector(src Source, log *obs.Logger, opts ...InjectorOption) *Injector {
	faults, _ := otel.Meter("image-gallery/demo").Int64Counter(obs.MetricDemoFaults, metric.WithUnit("{fault}"),
		metric.WithDescription("Faults injected by the demo controls"))
	i := &Injector{src: src, rnd: rand.Float64, sleep: sleepCtx, faults: faults, log: log}
	for _, o := range opts {
		o(i)
	}
	return i
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (i *Injector) record(ctx context.Context, fault string) {
	trace.SpanFromContext(ctx).SetAttributes(attribute.String(obs.AttrDemoFault, fault))
	i.faults.Add(ctx, 1, metric.WithAttributes(attribute.String(obs.AttrDemoFault, fault)))
	if i.log != nil {
		i.log.Warn(ctx).Str(obs.AttrDemoFault, fault).Msg("demo fault injected")
	}
}

// exempt: only /api is faulted, never the demo endpoints themselves, so a
// fault can always be switched off. Probes and pages are not under /api.
func exempt(path string) bool {
	return !strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/api/settings/demo")
}

// Middleware injects latency and 5xx errors into /api requests. Mount it
// inside the observability middleware so the fault lands on the server span.
func (i *Injector) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if exempt(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		ctx := r.Context()
		c, err := i.src.Get(ctx)
		if err != nil || !c.Active() {
			next.ServeHTTP(w, r)
			return
		}
		if c.LatencyMS > 0 && c.MatchesLatencyRoute(r.URL.Path) && i.rnd() < c.LatencyProbability {
			i.record(ctx, demo.FaultLatency)
			if err := i.sleep(ctx, time.Duration(c.LatencyMS)*time.Millisecond); err != nil {
				return // client went away
			}
		}
		if c.ErrorProbability > 0 && i.rnd() < c.ErrorProbability {
			i.record(ctx, demo.FaultError)
			http.Error(w, "demo fault: injected error", http.StatusServiceUnavailable)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// SlowDB returns how long the list query should sleep in the database (0 = no fault).
func (i *Injector) SlowDB(ctx context.Context) time.Duration {
	c, err := i.src.Get(ctx)
	if err != nil || c.SlowDBMS <= 0 {
		return 0
	}
	i.record(ctx, demo.FaultSlowDB)
	return time.Duration(c.SlowDBMS) * time.Millisecond
}

// WorkerFault delays the job and/or fails it (retryable), per the controls.
func (i *Injector) WorkerFault(ctx context.Context) error {
	c, err := i.src.Get(ctx)
	if err != nil {
		return nil // the demo must not break processing when its own row is unreadable
	}
	if c.WorkerDelayMS > 0 {
		i.record(ctx, demo.FaultWorkerSlowdown)
		if err := i.sleep(ctx, time.Duration(c.WorkerDelayMS)*time.Millisecond); err != nil {
			return err
		}
	}
	if c.WorkerFailureProbability > 0 && i.rnd() < c.WorkerFailureProbability {
		i.record(ctx, demo.FaultWorkerFailure)
		return errors.New("demo fault: injected worker failure")
	}
	return nil
}
