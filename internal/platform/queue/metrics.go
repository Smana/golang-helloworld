package queue

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"

	obs "image-gallery/internal/observability"
)

// RegisterGauges reports the consumer group's backlog on each collection.
// Pass an UNINSTRUMENTED client: these polls must not show up as traces.
//
//	queue.depth   entries not yet delivered to the group (XINFO GROUPS lag; XLEN also counts acked entries)
//	queue.pending delivered but not acked
//	queue.lag     age of the oldest pending entry, in seconds
func RegisterGauges(rdb redis.UniversalClient, meter metric.Meter, stream, group string) error {
	depth, err := meter.Int64ObservableGauge(obs.MetricQueueDepth, metric.WithUnit("{message}"),
		metric.WithDescription("Entries not yet delivered to the consumer group"))
	if err != nil {
		return err
	}
	pending, err := meter.Int64ObservableGauge(obs.MetricQueuePending, metric.WithUnit("{message}"),
		metric.WithDescription("Entries delivered but not acknowledged"))
	if err != nil {
		return err
	}
	lag, err := meter.Float64ObservableGauge(obs.MetricQueueLag, metric.WithUnit("s"),
		metric.WithDescription("Age of the oldest pending entry"))
	if err != nil {
		return err
	}
	attrs := metric.WithAttributes(semconv.MessagingDestinationName(stream), semconv.MessagingConsumerGroupName(group))
	// The returned metric.Registration is intentionally discarded: RegisterGauges'
	// signature (the Task 10 contract other tasks build on) has no way to hand it
	// back, and callers register gauges once for the worker process's entire
	// lifetime with no call to Unregister, so there is nothing to do with it.
	_, err = meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		groups, err := rdb.XInfoGroups(ctx, stream).Result()
		if err != nil {
			return err
		}
		for _, g := range groups {
			if g.Name != group {
				continue
			}
			if g.Lag >= 0 {
				o.ObserveInt64(depth, g.Lag, attrs)
			}
			o.ObserveInt64(pending, g.Pending, attrs)
		}
		sum, err := rdb.XPending(ctx, stream, group).Result()
		if err != nil {
			return err
		}
		age := 0.0
		if sum.Count > 0 {
			age = entryAge(sum.Lower, time.Now())
		}
		o.ObserveFloat64(lag, age, attrs)
		return nil
	}, depth, pending, lag)
	return err
}
