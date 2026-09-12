package observability

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// spanEndCounter counts every recording span that ends. With countingExporter
// it turns the BatchSpanProcessor's silent queue-full drops into a metric:
//
//	dropped ratio = 1 - telemetry.spans.exported{outcome="success"} / telemetry.spans.ended
type spanEndCounter struct{ ended metric.Int64Counter }

func (c spanEndCounter) OnStart(context.Context, sdktrace.ReadWriteSpan) {}
func (c spanEndCounter) OnEnd(s sdktrace.ReadOnlySpan) {
	if s.SpanContext().IsSampled() {
		// Background, not the span's context: counting must not attach exemplars.
		c.ended.Add(context.Background(), 1)
	}
}
func (c spanEndCounter) Shutdown(context.Context) error   { return nil }
func (c spanEndCounter) ForceFlush(context.Context) error { return nil }

// countingExporter counts the spans handed to the real exporter, by outcome.
type countingExporter struct {
	sdktrace.SpanExporter
	exported metric.Int64Counter
}

func (e countingExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	err := e.SpanExporter.ExportSpans(ctx, spans)
	outcome := OutcomeSuccess
	if err != nil {
		outcome = OutcomeFailure
	}
	e.exported.Add(context.Background(), int64(len(spans)), metric.WithAttributes(attribute.String(AttrOutcome, outcome)))
	return err
}
