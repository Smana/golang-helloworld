package observability

import (
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	instrumentationName = "github.com/smana/image-gallery/http"
	healthzPath         = "/healthz"
	readyzPath          = "/readyz"
)

// HTTPMetrics holds HTTP-related metrics instruments
type HTTPMetrics struct {
	requestCount    metric.Int64Counter
	requestDuration metric.Float64Histogram
	responseSize    metric.Int64Histogram
	activeRequests  metric.Int64UpDownCounter
}

// NewHTTPMetrics creates and registers HTTP metrics
func NewHTTPMetrics(meter metric.Meter) (*HTTPMetrics, error) {
	requestCount, err := meter.Int64Counter(
		"http.server.request.count",
		metric.WithDescription("Total number of HTTP requests"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return nil, err
	}

	requestDuration, err := meter.Float64Histogram(
		"http.server.request.duration",
		metric.WithDescription("Duration of HTTP requests"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	responseSize, err := meter.Int64Histogram(
		"http.server.response.size",
		metric.WithDescription("Size of HTTP response bodies"),
		metric.WithUnit("By"),
	)
	if err != nil {
		return nil, err
	}

	activeRequests, err := meter.Int64UpDownCounter(
		"http.server.active_requests",
		metric.WithDescription("Number of active HTTP requests"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return nil, err
	}

	return &HTTPMetrics{
		requestCount:    requestCount,
		requestDuration: requestDuration,
		responseSize:    responseSize,
		activeRequests:  activeRequests,
	}, nil
}

// responseWriter wraps http.ResponseWriter to capture status code and response size
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

// MetricsMiddleware returns a middleware that records HTTP metrics
func MetricsMiddleware(metrics *HTTPMetrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip health check endpoints
			if r.URL.Path == healthzPath || r.URL.Path == readyzPath {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			ctx := r.Context()

			// Increment active requests
			metrics.activeRequests.Add(ctx, 1)
			defer metrics.activeRequests.Add(ctx, -1)

			// Wrap response writer to capture status code and size
			rw := &responseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			// Process request
			next.ServeHTTP(rw, r)

			// Calculate duration
			duration := time.Since(start).Seconds()

			// Common attributes
			attrs := []attribute.KeyValue{
				semconv.HTTPRequestMethodKey.String(r.Method),
				semconv.HTTPRoute(r.URL.Path),
				semconv.HTTPResponseStatusCode(rw.statusCode),
				attribute.String("http.scheme", r.URL.Scheme),
			}

			// Record metrics
			metrics.requestCount.Add(ctx, 1, metric.WithAttributes(attrs...))
			metrics.requestDuration.Record(ctx, duration, metric.WithAttributes(attrs...))
			metrics.responseSize.Record(ctx, rw.bytesWritten, metric.WithAttributes(attrs...))
		})
	}
}

// TracingMiddleware returns a middleware that creates spans for HTTP requests
func TracingMiddleware(tracer trace.Tracer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip health check endpoints
			if r.URL.Path == healthzPath || r.URL.Path == readyzPath {
				next.ServeHTTP(w, r)
				return
			}

			// Start span
			ctx, span := tracer.Start(r.Context(), r.Method+" "+r.URL.Path,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					semconv.HTTPRequestMethodKey.String(r.Method),
					semconv.HTTPRoute(r.URL.Path),
					attribute.String("http.scheme", r.URL.Scheme),
					semconv.URLFull(r.URL.String()),
					semconv.UserAgentOriginal(r.UserAgent()),
					semconv.HTTPRequestBodySize(int(r.ContentLength)),
					semconv.ClientAddress(r.RemoteAddr),
				),
			)
			defer span.End()

			// Wrap response writer to capture status code
			rw := &responseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			// Process request with trace context
			next.ServeHTTP(rw, r.WithContext(ctx))

			// Add response attributes to span
			span.SetAttributes(
				semconv.HTTPResponseStatusCode(rw.statusCode),
				attribute.Int64("http.response.body.size", rw.bytesWritten),
			)

			// Set span status based on HTTP status code
			if rw.statusCode >= 400 {
				span.SetStatus(codes.Error, http.StatusText(rw.statusCode))
			}
		})
	}
}

// GetTracer returns a tracer for HTTP instrumentation
func GetTracer() trace.Tracer {
	return otel.Tracer(instrumentationName)
}

// GetMeter returns a meter for HTTP metrics
func GetMeter() metric.Meter {
	return otel.Meter(instrumentationName)
}
