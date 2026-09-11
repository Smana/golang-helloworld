package observability

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	instrumentationName = "image-gallery/http"
	healthzPath         = "/healthz"
	readyzPath          = "/readyz"
	unmatchedRoute      = "unmatched"
)

// HTTPMetrics holds the semconv HTTP server instruments.
type HTTPMetrics struct {
	duration metric.Float64Histogram
	active   metric.Int64UpDownCounter
	respSize metric.Int64Histogram
}

// NewHTTPMetrics registers the HTTP server instruments on meter.
func NewHTTPMetrics(meter metric.Meter) (*HTTPMetrics, error) {
	duration, err := meter.Float64Histogram(MetricHTTPServerDuration,
		metric.WithDescription("Duration of HTTP server requests"), metric.WithUnit("s"))
	if err != nil {
		return nil, err
	}
	active, err := meter.Int64UpDownCounter(MetricHTTPServerActive,
		metric.WithDescription("Number of in-flight HTTP server requests"), metric.WithUnit("{request}"))
	if err != nil {
		return nil, err
	}
	respSize, err := meter.Int64Histogram(MetricHTTPServerRespSize,
		metric.WithDescription("Size of HTTP server response bodies"), metric.WithUnit("By"))
	if err != nil {
		return nil, err
	}
	return &HTTPMetrics{duration: duration, active: active, respSize: respSize}, nil
}

// responseWriter captures the status code and body size.
type responseWriter struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int64
}

func (rw *responseWriter) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
	rw.ResponseWriter.WriteHeader(statusCode)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.bytesWritten += int64(n)
	return n, err
}

// Middleware is the only HTTP server instrumentation. It continues the caller's
// trace (W3C traceparent), names the span after the chi route pattern once
// routing is done, and records RED metrics keyed by that pattern. It never keys
// by the raw path: per-image IDs made every URL its own span name and series.
// metrics may be nil (metrics disabled).
func Middleware(tracer trace.Tracer, metrics *HTTPMetrics) func(http.Handler) http.Handler {
	// Fallback to GetTracer() when nil tracer passed (legacy constructor compatibility)
	if tracer == nil {
		tracer = GetTracer()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == healthzPath || r.URL.Path == readyzPath {
				next.ServeHTTP(w, r)
				return
			}
			ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
			method := semconv.HTTPRequestMethodKey.String(r.Method)
			ctx, span := tracer.Start(ctx, r.Method,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(method,
					semconv.URLPath(r.URL.Path),
					semconv.UserAgentOriginal(r.UserAgent()),
					semconv.ClientAddress(r.RemoteAddr)),
			)
			defer span.End()
			if metrics != nil {
				metrics.active.Add(ctx, 1, metric.WithAttributes(method))
				defer metrics.active.Add(ctx, -1, metric.WithAttributes(method))
			}

			rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			start := time.Now()
			next.ServeHTTP(rw, r.WithContext(ctx))

			route := routePattern(r)
			attrs := []attribute.KeyValue{method, semconv.HTTPRoute(route), semconv.HTTPResponseStatusCode(rw.statusCode)}
			span.SetName(r.Method + " " + route)
			span.SetAttributes(semconv.HTTPRoute(route), semconv.HTTPResponseStatusCode(rw.statusCode),
				semconv.HTTPResponseBodySize(int(rw.bytesWritten)))
			if rw.statusCode >= http.StatusInternalServerError {
				span.SetStatus(codes.Error, http.StatusText(rw.statusCode))
			}
			if metrics != nil {
				metrics.duration.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(attrs...))
				metrics.respSize.Record(ctx, rw.bytesWritten, metric.WithAttributes(attrs...))
			}
		})
	}
}

// routePattern is the matched chi pattern (complete once routing returned), or
// "unmatched" so that 404s cannot mint one series per path. The route context
// is shared by pointer with the request chi routed, so it is filled here.
func routePattern(r *http.Request) string {
	if rc := chi.RouteContext(r.Context()); rc != nil {
		if p := rc.RoutePattern(); p != "" {
			return p
		}
	}
	return unmatchedRoute
}

// GetTracer returns the HTTP instrumentation tracer.
func GetTracer() trace.Tracer { return otel.Tracer(instrumentationName) }

// GetMeter returns the HTTP instrumentation meter.
func GetMeter() metric.Meter { return otel.Meter(instrumentationName) }
