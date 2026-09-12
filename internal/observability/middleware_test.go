package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func newTestRouter(t *testing.T) (http.Handler, *tracetest.InMemoryExporter, *sdkmetric.ManualReader) {
	t.Helper()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	metrics, err := NewHTTPMetrics(mp.Meter("test"))
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.Use(Middleware(tp.Tracer("test"), metrics))
	r.Route("/api", func(r chi.Router) {
		r.Get("/images/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	})
	return r, exp, reader
}

func TestMiddlewareContinuesIncomingTrace(t *testing.T) {
	h, exp, _ := newTestRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/images/42", http.NoBody)
	req.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01") // pragma: allowlist secret
	h.ServeHTTP(httptest.NewRecorder(), req)

	spans := exp.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	s := spans[0]
	if s.SpanContext.TraceID().String() != "4bf92f3577b34da6a3ce929d0e0e4736" { // pragma: allowlist secret
		t.Errorf("trace not continued: %s", s.SpanContext.TraceID())
	}
	if s.Parent.SpanID().String() != "00f067aa0ba902b7" {
		t.Errorf("parent = %s", s.Parent.SpanID())
	}
	if s.Name != "GET /api/images/{id}" || s.SpanKind != trace.SpanKindServer {
		t.Errorf("name=%q kind=%v", s.Name, s.SpanKind)
	}
}

func TestMiddlewareMetricsUseRoutePatternNotPath(t *testing.T) {
	h, _, reader := newTestRouter(t)
	for _, p := range []string{"/api/images/1", "/api/images/2", "/nope"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, p, http.NoBody))
	}
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatal(err)
	}
	routes := map[string]uint64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "http.server.request.count" {
				t.Errorf("non-semconv http.server.request.count must be gone")
			}
			if m.Name != MetricHTTPServerDuration {
				continue
			}
			for _, dp := range m.Data.(metricdata.Histogram[float64]).DataPoints {
				v, _ := dp.Attributes.Value("http.route")
				routes[v.AsString()] += dp.Count
			}
		}
	}
	if routes["/api/images/{id}"] != 2 || routes["unmatched"] != 1 || len(routes) != 2 {
		t.Errorf("routes = %v, want {/api/images/{id}:2, unmatched:1}", routes)
	}
}
