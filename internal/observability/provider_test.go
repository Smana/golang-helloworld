package observability

import (
	"context"
	"runtime/debug"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func testConfig() Config {
	return Config{
		ServiceName: "xplane-image-gallery", ServiceVersion: "2.0.0", Environment: "test",
		PodName: "web-0", PodNamespace: "apps",
		TracesEnabled: true, TracesEndpoint: "http://unused", TracesSampler: SamplerAlwaysOn, TracesSamplerArg: "1.0",
		MetricsEnabled: true, MetricsEndpoint: "http://unused",
	}
}

// resetOTelGlobals undoes what NewProviderWith installs globally, so the providers a test
// shuts down do not leak into later tests. It resets to no-op implementations rather than to
// the previous values: the default globals delegate to the first provider ever set, so
// putting them back would still route through the test's providers.
func resetOTelGlobals(t *testing.T) {
	t.Cleanup(func() {
		otel.SetTracerProvider(tracenoop.NewTracerProvider())
		otel.SetMeterProvider(metricnoop.NewMeterProvider())
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator())
	})
}

func sumCounter(t *testing.T, rm metricdata.ResourceMetrics, name string, match attribute.KeyValue) int64 {
	t.Helper()
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			matchSet := attribute.NewSet(match)
			for _, dp := range m.Data.(metricdata.Sum[int64]).DataPoints {
				if match.Key == "" || dp.Attributes.HasValue(match.Key) && dp.Attributes.Equivalent() == matchSet.Equivalent() {
					total += dp.Value
				}
			}
		}
	}
	return total
}

func metricNames(rm metricdata.ResourceMetrics) map[string]bool {
	names := map[string]bool{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			names[m.Name] = true
		}
	}
	return names
}

func TestProviderCountsEndedAndExportedSpans(t *testing.T) {
	// go.memory.limit is only emitted by the runtime instrumentation when a
	// memory limit is configured; the host running this test may have none
	// (no GOMEMLIMIT, no cgroup limit), so set one for the duration of the test.
	prevLimit := debug.SetMemoryLimit(512 << 20)
	defer debug.SetMemoryLimit(prevLimit)

	ctx := context.Background()
	exp := tracetest.NewInMemoryExporter()
	reader := sdkmetric.NewManualReader()
	resetOTelGlobals(t)
	p, err := NewProviderWith(ctx, testConfig(), nil, exp, reader)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Shutdown(ctx) }()

	tr := otel.Tracer("test")
	for i := 0; i < 3; i++ {
		_, span := tr.Start(ctx, "op")
		span.End()
	}
	if err := p.ForceFlush(ctx); err != nil {
		t.Fatal(err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatal(err)
	}
	if got := sumCounter(t, rm, MetricSpansEnded, attribute.KeyValue{}); got != 3 {
		t.Fatalf("%s = %d, want 3", MetricSpansEnded, got)
	}
	if got := sumCounter(t, rm, MetricSpansExported, attribute.String(AttrOutcome, OutcomeSuccess)); got != 3 {
		t.Fatalf("%s{outcome=success} = %d, want 3", MetricSpansExported, got)
	}
	if n := len(exp.GetSpans()); n != 3 {
		t.Fatalf("exported spans = %d, want 3", n)
	}
	for _, name := range RuntimeInstruments {
		if !metricNames(rm)[name] {
			t.Errorf("runtime metric %q missing", name)
		}
	}
	res := exp.GetSpans()[0].Resource
	for _, want := range []attribute.KeyValue{
		attribute.String("k8s.pod.name", "web-0"),
		attribute.String("k8s.namespace.name", "apps"),
		attribute.String("deployment.environment.name", "test"),
	} {
		if v, ok := res.Set().Value(want.Key); !ok || v != want.Value {
			t.Errorf("resource %s = %v, want %v", want.Key, v, want.Value)
		}
	}
}
