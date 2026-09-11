package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestLoggerCarriesServiceNameAndTraceIDs(t *testing.T) {
	var buf bytes.Buffer
	l := NewLoggerTo(&buf, Config{ServiceName: "xplane-image-gallery-worker", LogLevel: "info", LogFormat: "json"})
	tp := sdktrace.NewTracerProvider()
	ctx, span := tp.Tracer("t").Start(context.Background(), "op")
	l.Info(ctx).Msg("hello")
	span.End()

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("not JSON: %q", buf.String())
	}
	if line["service.name"] != "xplane-image-gallery-worker" {
		t.Errorf("service.name = %v", line["service.name"])
	}
	if line["trace_id"] != span.SpanContext().TraceID().String() || line["span_id"] == nil {
		t.Errorf("trace correlation missing: %v", line)
	}
}
