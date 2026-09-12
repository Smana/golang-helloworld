package queue

import (
	"context"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	obs "image-gallery/internal/observability"
)

// Producer appends jobs to the stream.
type Producer struct {
	rdb    redis.UniversalClient
	o      options
	tracer trace.Tracer
	sent   metric.Int64Counter
}

// NewProducer builds a producer on rdb.
func NewProducer(rdb redis.UniversalClient, opts ...Option) (*Producer, error) {
	o := build(opts)
	sent, err := o.mp.Meter("image-gallery/queue").Int64Counter(obs.MetricMessagingSent,
		metric.WithUnit("{message}"), metric.WithDescription("Jobs appended to the stream"))
	if err != nil {
		return nil, err
	}
	return &Producer{rdb: rdb, o: o, tracer: o.tp.Tracer("image-gallery/queue"), sent: sent}, nil
}

// Publish appends job under a PRODUCER span whose context travels in the entry,
// so the consumer continues the same trace. Returns the stream entry ID.
func (p *Producer) Publish(ctx context.Context, job Job) (string, error) {
	ctx, span := p.tracer.Start(ctx, "send "+p.o.stream, trace.WithSpanKind(trace.SpanKindProducer), trace.WithAttributes(
		semconv.MessagingSystemKey.String(messagingSystem),
		semconv.MessagingDestinationName(p.o.stream),
		semconv.MessagingOperationTypeKey.String("send"),
		attribute.Int(obs.AttrImageID, job.ImageID),
		attribute.String(obs.AttrJobType, job.Type)))
	defer span.End()

	carrier := propagation.MapCarrier{}
	p.o.prop.Inject(ctx, carrier)
	job.Carrier = carrier

	attrs := []attribute.KeyValue{semconv.MessagingSystemKey.String(messagingSystem), semconv.MessagingDestinationName(p.o.stream)}
	id, err := p.rdb.XAdd(ctx, &redis.XAddArgs{Stream: p.o.stream, MaxLen: p.o.maxLen, Approx: true, Values: job.values()}).Result()
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "xadd failed")
		p.sent.Add(ctx, 1, metric.WithAttributes(append(attrs, semconv.ErrorTypeKey.String("xadd"))...))
		return "", err
	}
	span.SetAttributes(semconv.MessagingMessageID(id))
	p.sent.Add(ctx, 1, metric.WithAttributes(attrs...))
	return id, nil
}
