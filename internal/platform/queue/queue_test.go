package queue

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	obs "image-gallery/internal/observability"
)

type harness struct {
	rdb    *redis.Client
	spans  *tracetest.InMemoryExporter
	reader *sdkmetric.ManualReader
	opts   []Option
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx := context.Background()
	c, err := tcredis.Run(ctx, "valkey/valkey:7-alpine")
	testcontainers.CleanupContainer(t, c)
	if err != nil {
		t.Fatal(err)
	}
	uri, err := c.ConnectionString(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: strings.TrimPrefix(uri, "redis://")})
	t.Cleanup(func() { _ = rdb.Close() })
	exp := tracetest.NewInMemoryExporter()
	reader := sdkmetric.NewManualReader()
	stream := "t:" + strings.ReplaceAll(t.Name(), "/", "_")
	return &harness{rdb: rdb, spans: exp, reader: reader, opts: []Option{
		WithTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))),
		WithMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))),
		WithPropagator(propagation.TraceContext{}),
		WithStream(stream), WithDeadLetter(stream + ":dead"), WithGroup("workers"),
		WithConsumerName("test-worker"), WithBlock(100 * time.Millisecond),
		WithBackoff(func(int) time.Duration { return time.Millisecond }),
	}}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func runConsumer(t *testing.T, h *harness, handler Handler, extra ...Option) (stop func()) {
	t.Helper()
	c, err := NewConsumer(h.rdb, append(h.opts, extra...)...)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = c.Run(ctx, handler); close(done) }()
	return func() { cancel(); <-done }
}

func spansNamed(h *harness, name string) []tracetest.SpanStub {
	var out []tracetest.SpanStub
	spans := h.spans.GetSpans()
	for i := range spans {
		if spans[i].Name == name {
			out = append(out, spans[i])
		}
	}
	return out
}

func counterByOutcome(t *testing.T, h *harness, name string) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := h.reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	out := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				v, _ := dp.Attributes.Value(obs.AttrOutcome)
				out[v.AsString()] += dp.Value
			}
		}
	}
	return out
}

func TestTraceContinuesThroughQueueWithLink(t *testing.T) {
	h := newHarness(t)
	p, err := NewProducer(h.rdb, h.opts...)
	if err != nil {
		t.Fatal(err)
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(h.spans))
	ctx, root := tp.Tracer("t").Start(context.Background(), "upload")
	if _, err := p.Publish(ctx, Job{Type: JobTypeProcessImage, ImageID: 7, ObjectKey: "k"}); err != nil {
		t.Fatal(err)
	}
	root.End()

	var handled atomic.Int32
	stop := runConsumer(t, h, func(context.Context, Job) error { handled.Add(1); return nil })
	waitFor(t, "job handled", func() bool { return handled.Load() == 1 })
	stop()

	prod := spansNamed(h, "send "+h.streamName())
	cons := spansNamed(h, "process "+h.streamName())
	if len(prod) != 1 || len(cons) != 1 {
		t.Fatalf("producer spans %d, consumer spans %d", len(prod), len(cons))
	}
	if prod[0].SpanKind != trace.SpanKindProducer || cons[0].SpanKind != trace.SpanKindConsumer {
		t.Errorf("kinds: %v / %v", prod[0].SpanKind, cons[0].SpanKind)
	}
	if cons[0].SpanContext.TraceID() != root.SpanContext().TraceID() {
		t.Errorf("consumer is not in the upload's trace")
	}
	if cons[0].Parent.SpanID() != prod[0].SpanContext.SpanID() {
		t.Errorf("consumer parent = %s, want producer %s", cons[0].Parent.SpanID(), prod[0].SpanContext.SpanID())
	}
	if len(cons[0].Links) != 1 || cons[0].Links[0].SpanContext.SpanID() != prod[0].SpanContext.SpanID() {
		t.Errorf("consumer must link the producer span, links = %+v", cons[0].Links)
	}
}

func TestRetryThenDeadLetter(t *testing.T) {
	h := newHarness(t)
	p, _ := NewProducer(h.rdb, h.opts...)
	if _, err := p.Publish(context.Background(), Job{Type: JobTypeProcessImage, ImageID: 9, ObjectKey: "k"}); err != nil {
		t.Fatal(err)
	}
	var hooked atomic.Int32
	stop := runConsumer(t, h, func(context.Context, Job) error { return errors.New("boom") },
		WithMaxAttempts(3), WithDeadLetterHook(func(_ context.Context, j Job, _ error) {
			if j.ImageID == 9 {
				hooked.Add(1)
			}
		}))
	waitFor(t, "dead-letter hook", func() bool { return hooked.Load() == 1 })
	stop()

	ctx := context.Background()
	dead, err := h.rdb.XRange(ctx, h.streamName()+":dead", "-", "+").Result()
	if err != nil || len(dead) != 1 || dead[0].Values["error"] != "boom" || dead[0].Values["original_id"] == nil {
		t.Fatalf("dead-letter stream = %+v, %v", dead, err)
	}
	pend, _ := h.rdb.XPending(ctx, h.streamName(), "workers").Result()
	if pend.Count != 0 {
		t.Fatalf("original must be acked, pending = %d", pend.Count)
	}
	if n := len(spansNamed(h, "job.attempt")); n != 3 {
		t.Errorf("job.attempt spans = %d, want 3", n)
	}
	if got := counterByOutcome(t, h, obs.MetricWorkerJobs); got[obs.OutcomeRetry] != 2 || got[obs.OutcomeDeadLetter] != 1 {
		t.Errorf("worker.jobs = %v, want retry:2 dead_letter:1", got)
	}
}

func TestPermanentAndSkip(t *testing.T) {
	h := newHarness(t)
	p, _ := NewProducer(h.rdb, h.opts...)
	ctx := context.Background()
	_, _ = p.Publish(ctx, Job{Type: JobTypeProcessImage, ImageID: 1, ObjectKey: "perm"})
	_, _ = p.Publish(ctx, Job{Type: JobTypeProcessImage, ImageID: 2, ObjectKey: "skip"})
	var calls atomic.Int32
	stop := runConsumer(t, h, func(_ context.Context, j Job) error {
		calls.Add(1)
		if j.ObjectKey == "perm" {
			return Permanent(errors.New("corrupt image"))
		}
		return ErrSkip
	})
	waitFor(t, "both handled", func() bool {
		pend, _ := h.rdb.XPending(ctx, h.streamName(), "workers").Result()
		n, _ := h.rdb.XLen(ctx, h.streamName()+":dead").Result()
		return calls.Load() == 2 && pend.Count == 0 && n == 1
	})
	stop()
	if got := counterByOutcome(t, h, obs.MetricWorkerJobs); got[obs.OutcomeSkipped] != 1 || got[obs.OutcomeDeadLetter] != 1 || got[obs.OutcomeRetry] != 0 {
		t.Errorf("worker.jobs = %v", got)
	}
}

func TestAutoClaimRecoversAbandonedEntry(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	c0, _ := NewConsumer(h.rdb, h.opts...)
	if err := c0.EnsureGroup(ctx); err != nil {
		t.Fatal(err)
	}
	p, _ := NewProducer(h.rdb, h.opts...)
	_, _ = p.Publish(ctx, Job{Type: JobTypeProcessImage, ImageID: 5, ObjectKey: "k"})
	// A worker that read the entry and died before acking it.
	if _, err := h.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{Group: "workers", Consumer: "dead-worker",
		Streams: []string{h.streamName(), ">"}, Count: 1}).Result(); err != nil {
		t.Fatal(err)
	}
	var got atomic.Int32
	stop := runConsumer(t, h, func(_ context.Context, j Job) error { got.Store(int32(j.ImageID)); return nil },
		WithClaimIdle(100*time.Millisecond))
	waitFor(t, "reclaimed job", func() bool { return got.Load() == 5 })
	stop()
}

func TestShutdownFinishesInFlightJob(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	p, _ := NewProducer(h.rdb, h.opts...)
	_, _ = p.Publish(ctx, Job{Type: JobTypeProcessImage, ImageID: 3, ObjectKey: "k"})
	started, release := make(chan struct{}), make(chan struct{})
	var finished atomic.Bool
	stop := runConsumer(t, h, func(context.Context, Job) error {
		close(started)
		<-release
		finished.Store(true)
		return nil
	})
	<-started
	go func() { time.Sleep(200 * time.Millisecond); close(release) }()
	stop() // cancels, then must wait for the in-flight job
	if !finished.Load() {
		t.Fatal("Run returned before the in-flight job finished")
	}
	pend, _ := h.rdb.XPending(ctx, h.streamName(), "workers").Result()
	if pend.Count != 0 {
		t.Fatalf("in-flight job must be acked on shutdown, pending = %d", pend.Count)
	}
}

func TestGaugesAndReady(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	c, _ := NewConsumer(h.rdb, h.opts...)
	if err := c.Ready(ctx); err == nil {
		t.Fatal("Ready must fail before the group exists")
	}
	if err := c.EnsureGroup(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Ready(ctx); err != nil {
		t.Fatal(err)
	}
	gaugeReader := sdkmetric.NewManualReader() // a reader registers with ONE provider; the harness's is taken
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(gaugeReader))
	if err := RegisterGauges(h.rdb, mp.Meter("q"), h.streamName(), "workers"); err != nil {
		t.Fatal(err)
	}
	p, _ := NewProducer(h.rdb, h.opts...)
	for i := 0; i < 3; i++ {
		_, _ = p.Publish(ctx, Job{Type: JobTypeProcessImage, ImageID: i, ObjectKey: "k"})
	}
	// XReadGroup is synchronous: by the time Result() returns, the entry is
	// already in the group's PEL, so no extra wait is needed before collecting.
	_, _ = h.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{Group: "workers", Consumer: "x", Streams: []string{h.streamName(), ">"}, Count: 1}).Result()

	var rm metricdata.ResourceMetrics
	if err := gaugeReader.Collect(ctx, &rm); err != nil {
		t.Fatal(err)
	}
	vals := map[string]float64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch d := m.Data.(type) {
			case metricdata.Gauge[int64]:
				for _, dp := range d.DataPoints {
					vals[m.Name] = float64(dp.Value)
				}
			case metricdata.Gauge[float64]:
				for _, dp := range d.DataPoints {
					vals[m.Name] = dp.Value
				}
			}
		}
	}
	if vals[obs.MetricQueueDepth] != 2 || vals[obs.MetricQueuePending] != 1 || vals[obs.MetricQueueLag] <= 0 {
		t.Fatalf("gauges = %v, want depth 2, pending 1, lag > 0", vals)
	}
	_ = attribute.Key("unused") // keep the import list stable if assertions change
}

func (h *harness) streamName() string {
	for _, o := range h.opts {
		var x options
		o(&x)
		if x.stream != "" {
			return x.stream
		}
	}
	return DefaultStream
}
