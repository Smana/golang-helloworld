package faults

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"image-gallery/internal/domain/demo"
	"image-gallery/internal/observability"
)

type staticSource struct{ c demo.Controls }

func (s staticSource) Get(context.Context) (demo.Controls, error) { return s.c, nil }

type harness struct {
	inj    *Injector
	slept  []time.Duration
	logs   *bytes.Buffer
	spans  *tracetest.InMemoryExporter
	tracer *sdktrace.TracerProvider
}

func newHarness(c demo.Controls, roll float64) *harness {
	h := &harness{logs: &bytes.Buffer{}, spans: tracetest.NewInMemoryExporter()}
	h.tracer = sdktrace.NewTracerProvider(sdktrace.WithSyncer(h.spans))
	log := observability.NewLoggerTo(h.logs, observability.Config{ServiceName: "t", LogLevel: "info", LogFormat: "json"})
	h.inj = NewInjector(staticSource{c: c}, log,
		WithRand(func() float64 { return roll }), // deterministic: every draw returns roll
		WithSleep(func(_ context.Context, d time.Duration) error { h.slept = append(h.slept, d); return nil }))
	return h
}

func (h *harness) do(path string) *httptest.ResponseRecorder {
	ctx, span := h.tracer.Tracer("t").Start(context.Background(), "server")
	defer span.End()
	rec := httptest.NewRecorder()
	h.inj.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, http.NoBody).WithContext(ctx))
	return rec
}

func (h *harness) faultAttrs() []string {
	var out []string
	spans := h.spans.GetSpans()
	for i := range spans {
		set := attribute.NewSet(spans[i].Attributes...)
		if v, ok := set.Value("demo.fault"); ok {
			out = append(out, v.AsString())
		}
	}
	return out
}

func (h *harness) faultEvents() []string {
	var out []string
	spans := h.spans.GetSpans()
	for i := range spans {
		for _, e := range spans[i].Events {
			set := attribute.NewSet(e.Attributes...)
			if v, ok := set.Value("demo.fault"); ok {
				out = append(out, v.AsString())
			}
		}
	}
	return out
}

func TestLatencyThenErrorCarryAllThreeMarkers(t *testing.T) {
	h := newHarness(demo.Controls{LatencyMS: 800, LatencyProbability: 0.5, LatencyRoutes: []string{"/api/images"}, ErrorProbability: 0.5}, 0.3)
	rec := h.do("/api/images/7")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want 503", rec.Code)
	}
	if len(h.slept) != 1 || h.slept[0] != 800*time.Millisecond {
		t.Fatalf("slept = %v", h.slept)
	}
	if got := h.faultAttrs(); len(got) != 1 || got[0] != demo.FaultError { // the attribute holds the last fault
		t.Fatalf("span demo.fault = %v", got)
	}
	if got := h.faultEvents(); strings.Join(got, ",") != demo.FaultLatency+","+demo.FaultError {
		t.Fatalf("span demo.fault events = %v, want every fault in order", got)
	}
	if c := strings.Count(h.logs.String(), "demo fault injected"); c != 2 {
		t.Fatalf("log lines = %d, want 2:\n%s", c, h.logs.String())
	}
}

func TestProbabilityIsRespected(t *testing.T) {
	h := newHarness(demo.Controls{ErrorProbability: 0.2}, 0.3) // 0.3 >= 0.2: no fault
	if rec := h.do("/api/images"); rec.Code != 200 {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
}

func TestLatencyRouteFilterAndExemptPaths(t *testing.T) {
	h := newHarness(demo.Controls{LatencyMS: 100, LatencyProbability: 1, LatencyRoutes: []string{"/api/images"}, ErrorProbability: 1}, 0)
	for _, p := range []string{"/api/settings/demo", "/api/settings/demo/reset", "/healthz", "/readyz", "/gallery"} {
		if rec := h.do(p); rec.Code != 200 {
			t.Errorf("%s must never be faulted, got %d", p, rec.Code)
		}
	}
	h.do("/api/settings")
	if len(h.slept) != 0 {
		t.Fatalf("latency outside latency_routes: %v", h.slept)
	}
}

func TestSlowDBHookSkipsTheFaultPastTheLimit(t *testing.T) {
	h := newHarness(demo.Controls{SlowDBMS: 250}, 0)
	entered, release := make(chan struct{}, 3), make(chan struct{})
	hook := h.inj.SlowDBHook(2, func(context.Context, time.Duration) {
		entered <- struct{}{}
		<-release
	})

	var wg sync.WaitGroup
	for range 2 { // fill both slots, one at a time
		wg.Add(1)
		go func() { defer wg.Done(); hook(context.Background()) }()
		<-entered
	}
	done := make(chan struct{})
	go func() { hook(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the call past the limit waited for a slot instead of skipping the fault")
	}
	if n := strings.Count(h.logs.String(), "demo fault injected"); n != 2 {
		t.Fatalf("recorded %d faults, want 2: a skipped fault must not be reported", n)
	}

	close(release)
	wg.Wait()
	hook(context.Background()) // a freed slot is used again
	if len(entered) != 1 {
		t.Fatal("the hook did not sleep once a slot was free")
	}
}

func TestSlowDBAndWorkerFaults(t *testing.T) {
	h := newHarness(demo.Controls{SlowDBMS: 250, WorkerDelayMS: 100, WorkerFailureProbability: 1}, 0)
	if d := h.inj.SlowDB(context.Background()); d != 250*time.Millisecond {
		t.Fatalf("SlowDB = %v", d)
	}
	err := h.inj.WorkerFault(context.Background())
	if err == nil || len(h.slept) != 1 || h.slept[0] != 100*time.Millisecond {
		t.Fatalf("WorkerFault err=%v slept=%v", err, h.slept)
	}
	off := newHarness(demo.Controls{}, 0)
	if off.inj.SlowDB(context.Background()) != 0 || off.inj.WorkerFault(context.Background()) != nil || off.logs.Len() != 0 {
		t.Fatal("controls off must inject nothing")
	}
}
