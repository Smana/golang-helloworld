package loadgen

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// fakeApp mimics the image-gallery API surface the load generator uses.
type fakeApp struct {
	mu          sync.Mutex
	hits        map[string]int
	demoPuts    []map[string]any
	resets      atomic.Int32
	fail        atomic.Bool
	traceparent atomic.Int32
	nextID      atomic.Int32
}

func newFakeApp(t *testing.T) (*fakeApp, *httptest.Server) {
	f := &fakeApp{hits: map[string]int{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("traceparent") != "" {
			f.traceparent.Add(1)
		}
		key := r.Method + " " + route(r.URL.Path)
		f.mu.Lock()
		f.hits[key]++
		f.mu.Unlock()
		if f.fail.Load() && !strings.HasPrefix(r.URL.Path, "/api/settings/demo") {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		switch key {
		case "GET /api/images":
			_, _ = io.WriteString(w, `{"images":[{"id":"1"},{"id":"2"}],"total_count":2}`)
		case "POST /api/images":
			_ = r.ParseMultipartForm(32 << 20)
			w.WriteHeader(http.StatusCreated)
			fmt.Fprintf(w, `{"images":[{"id":%d}],"count":1}`, 100+f.nextID.Add(1))
		case "PUT /api/settings/demo":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.mu.Lock()
			f.demoPuts = append(f.demoPuts, body)
			f.mu.Unlock()
			_, _ = io.WriteString(w, `{}`)
		case "POST /api/settings/demo/reset":
			f.resets.Add(1)
			_, _ = io.WriteString(w, `{}`)
		case "DELETE /api/images/{id}":
			w.WriteHeader(http.StatusNoContent)
		default:
			_, _ = io.WriteString(w, "ok")
		}
	}))
	t.Cleanup(srv.Close)
	return f, srv
}

func route(p string) string {
	parts := strings.Split(p, "/")
	if len(parts) >= 4 && parts[1] == "api" && parts[2] == "images" {
		parts[3] = "{id}"
	}
	return strings.Join(parts, "/")
}

func TestOptionsValidation(t *testing.T) {
	cases := []struct {
		args    []string
		wantErr string
	}{
		{[]string{}, "--target is required"},
		{[]string{"--target", "http://x", "--rate", "30"}, "--force"},
		{[]string{"--target", "http://x", "--scenario", "nope"}, "unknown scenario"},
	}
	for _, c := range cases {
		if _, err := parseOptions(c.args, io.Discard); err == nil || !strings.Contains(err.Error(), c.wantErr) {
			t.Errorf("%v: err = %v, want %q", c.args, err, c.wantErr)
		}
	}
	o, err := parseOptions([]string{"--target", "http://x/", "--rate", "30", "--force"}, io.Discard)
	if err != nil || o.Rate != 30 || o.Target != "http://x" {
		t.Fatalf("forced rate: %+v %v", o, err)
	}
	o, _ = parseOptions([]string{"--target", "http://x", "--scenario", "steady"}, io.Discard)
	if o.Rate != 1 {
		t.Fatalf("steady default rate = %v, want 1", o.Rate)
	}
}

func TestMixedScenarioIsOpenLoopAndCoversOps(t *testing.T) {
	f, srv := newFakeApp(t)
	sum, err := Run(context.Background(), Options{Target: srv.URL, Scenario: "mixed", Rate: 25, Duration: 2 * time.Second, Concurrency: 10, Seed: 7}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Requests < 35 || sum.Errors != 0 || sum.Shed != 0 {
		t.Fatalf("summary = %+v", sum)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, k := range []string{"GET /api/images", "POST /api/images", "GET /api/images/{id}/thumbnail"} {
		if f.hits[k] == 0 {
			t.Errorf("never called %s (hits %v)", k, f.hits)
		}
	}
}

func TestClientInjectsTraceparent(t *testing.T) {
	otel.SetTextMapPropagator(propagation.TraceContext{})
	otel.SetTracerProvider(sdktrace.NewTracerProvider())
	f, srv := newFakeApp(t)
	if _, err := Run(context.Background(), Options{Target: srv.URL, Scenario: "browse", Rate: 10, Duration: 500 * time.Millisecond, Concurrency: 2, Seed: 1}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if f.traceparent.Load() == 0 {
		t.Fatal("requests carried no traceparent: traces would not start at the client")
	}
}

func TestBreakerStopsASustainedErrorRate(t *testing.T) {
	f, srv := newFakeApp(t)
	f.fail.Store(true)
	start := time.Now()
	_, err := Run(context.Background(), Options{Target: srv.URL, Scenario: "browse", Rate: 25, Duration: time.Minute, Concurrency: 10, Seed: 1}, io.Discard)
	if !errors.Is(err, ErrBreakerTripped) || time.Since(start) > 15*time.Second {
		t.Fatalf("err = %v after %s", err, time.Since(start))
	}
}

func TestIncidentRunsEveryPhaseThenResets(t *testing.T) {
	f, srv := newFakeApp(t)
	var out strings.Builder
	if _, err := Run(context.Background(), Options{Target: srv.URL, Scenario: "incident", Rate: 20, Concurrency: 5, Seed: 1, PhaseScale: 0.002}, &out); err != nil {
		t.Fatal(err)
	}
	if f.resets.Load() != 1 {
		t.Fatalf("resets = %d, want 1", f.resets.Load())
	}
	var sawLatency, sawErrors, sawSlowWorker bool
	for _, p := range f.demoPuts {
		sawLatency = sawLatency || p["latency_ms"] == float64(800)
		sawErrors = sawErrors || p["error_probability"] == 0.2
		sawSlowWorker = sawSlowWorker || p["worker_delay_ms"] == float64(4000)
	}
	if !sawLatency || !sawErrors || !sawSlowWorker || !strings.Contains(out.String(), "phase recovery") {
		t.Fatalf("puts = %v\noutput:\n%s", f.demoPuts, out.String())
	}
}

func TestIncidentResetsOnInterrupt(t *testing.T) {
	f, srv := newFakeApp(t)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, _ = Run(ctx, Options{Target: srv.URL, Scenario: "incident", Rate: 10, Concurrency: 2, Seed: 1, PhaseScale: 1}, io.Discard)
	if f.resets.Load() != 1 {
		t.Fatalf("an interrupted incident must still reset the controls, resets = %d", f.resets.Load())
	}
}

func TestPercentile(t *testing.T) {
	d := []time.Duration{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	if p := percentile(d, 0.5); p != 5 {
		t.Errorf("p50 = %v", p)
	}
	if p := percentile(d, 0.99); p != 10 {
		t.Errorf("p99 = %v", p)
	}
}
