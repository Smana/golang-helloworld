package queue

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	obs "image-gallery/internal/observability"
)

// Handler processes one job. Return nil (done), ErrSkip (nothing to do),
// Permanent(err) (dead-letter now) or any other error (retry).
type Handler func(ctx context.Context, job Job) error

// Consumer reads the stream in a consumer group.
type Consumer struct {
	rdb        redis.UniversalClient
	o          options
	tracer     trace.Tracer
	jobs       metric.Int64Counter
	processDur metric.Float64Histogram
}

// NewConsumer builds a consumer on rdb.
func NewConsumer(rdb redis.UniversalClient, opts ...Option) (*Consumer, error) {
	o := build(opts)
	m := o.mp.Meter("image-gallery/queue")
	jobs, err := m.Int64Counter(obs.MetricWorkerJobs, metric.WithUnit("{job}"),
		metric.WithDescription("Job outcomes: success, retry, dead_letter, skipped"))
	if err != nil {
		return nil, err
	}
	dur, err := m.Float64Histogram(obs.MetricMessagingProcess, metric.WithUnit("s"),
		metric.WithDescription("Time from delivery to ack, all attempts included"))
	if err != nil {
		return nil, err
	}
	return &Consumer{rdb: rdb, o: o, tracer: o.tp.Tracer("image-gallery/queue"), jobs: jobs, processDur: dur}, nil
}

// EnsureGroup creates the stream and the consumer group if missing.
func (c *Consumer) EnsureGroup(ctx context.Context) error {
	err := c.rdb.XGroupCreateMkStream(ctx, c.o.stream, c.o.group, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return err
	}
	return nil
}

// Ready is the worker's readiness: the stream answers and the group exists.
func (c *Consumer) Ready(ctx context.Context) error {
	groups, err := c.rdb.XInfoGroups(ctx, c.o.stream).Result()
	if err != nil {
		return err
	}
	for _, g := range groups {
		if g.Name == c.o.group {
			return nil
		}
	}
	return fmt.Errorf("consumer group %s missing on %s", c.o.group, c.o.stream)
}

// Run consumes until ctx is canceled, then returns once in-flight jobs are
// acked: it stops reading, it does not abandon work.
func (c *Consumer) Run(ctx context.Context, h Handler) error {
	if err := c.EnsureGroup(ctx); err != nil {
		return err
	}
	sem := make(chan struct{}, c.o.concurrency)
	var wg sync.WaitGroup
	defer wg.Wait()
	for ctx.Err() == nil {
		free := c.o.concurrency - len(sem)
		if free <= 0 {
			pause(ctx, 20*time.Millisecond)
			continue
		}
		msgs, err := c.fetch(ctx, int64(free))
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			pause(ctx, time.Second) // Valkey unavailable: readiness reports it; retry the read
			continue
		}
		for _, m := range msgs {
			sem <- struct{}{}
			wg.Add(1)
			go func(m redis.XMessage) {
				defer wg.Done()
				defer func() { <-sem }()
				c.handle(context.WithoutCancel(ctx), m, h) // shutdown must not cut a job in half
			}(m)
		}
	}
	return nil
}

// fetch reclaims entries idle past claimIdle first, then reads new ones.
func (c *Consumer) fetch(ctx context.Context, n int64) ([]redis.XMessage, error) {
	claimed, _, err := c.rdb.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream: c.o.stream, Group: c.o.group, Consumer: c.o.consumer, MinIdle: c.o.claimIdle, Start: "0-0", Count: n,
	}).Result()
	if err != nil {
		return nil, err
	}
	if len(claimed) > 0 {
		return claimed, nil
	}
	streams, err := c.rdb.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group: c.o.group, Consumer: c.o.consumer, Streams: []string{c.o.stream, ">"}, Count: n, Block: c.o.block,
	}).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []redis.XMessage
	for _, s := range streams {
		out = append(out, s.Messages...)
	}
	return out, nil
}

func (c *Consumer) handle(ctx context.Context, m redis.XMessage, h Handler) {
	start := time.Now()
	job, parseErr := jobFromMessage(m)
	parent := c.o.prop.Extract(ctx, propagation.MapCarrier(job.Carrier))
	ctx, span := c.tracer.Start(parent, "process "+c.o.stream,
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithLinks(trace.LinkFromContext(parent)), // explicit producer relationship, in addition to parenthood
		trace.WithAttributes(
			semconv.MessagingSystemKey.String(messagingSystem),
			semconv.MessagingDestinationName(c.o.stream),
			semconv.MessagingConsumerGroupName(c.o.group),
			semconv.MessagingOperationTypeKey.String("process"),
			semconv.MessagingMessageID(m.ID),
			attribute.Int(obs.AttrImageID, job.ImageID),
			attribute.String(obs.AttrJobType, job.Type)))
	defer span.End()

	outcome, err := obs.OutcomeDeadLetter, parseErr
	if parseErr == nil {
		outcome, err = c.attempts(ctx, job, h)
	}
	if outcome == obs.OutcomeDeadLetter {
		span.RecordError(err)
		span.SetStatus(codes.Error, "dead-lettered")
		c.deadLetter(ctx, m, job, err)
	}
	if ackErr := c.rdb.XAck(ctx, c.o.stream, c.o.group, m.ID).Err(); ackErr != nil {
		span.RecordError(ackErr)
	}
	attrs := metric.WithAttributes(attribute.String(obs.AttrJobType, job.Type), attribute.String(obs.AttrOutcome, outcome))
	c.jobs.Add(ctx, 1, attrs)
	c.processDur.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(
		semconv.MessagingSystemKey.String(messagingSystem), semconv.MessagingDestinationName(c.o.stream),
		attribute.String(obs.AttrJobType, job.Type), attribute.String(obs.AttrOutcome, outcome)))
}

// attempts runs h up to maxAttempts times with exponential backoff.
//
// The attempt budget (n) lives only in this call's stack: it is not persisted
// to the stream entry. If the worker process dies mid-backoff, the entry sits
// unacked in the PEL until some consumer's XAutoClaim picks it back up, and
// that next handle() call starts counting from attempt 1 again — a worker
// that crash-loops on the same poison job can therefore retry it more than
// maxAttempts times overall. Redis does track delivery count durably per
// pending entry (XPendingExt's RetryCount, incremented by XAutoClaim); this
// consumer does not consult it (ruling R28: not worth the hot-path complexity
// and flakiness risk for a failure mode no acceptance criterion exercises).
func (c *Consumer) attempts(ctx context.Context, job Job, h Handler) (string, error) {
	var err error
	for n := 1; n <= c.o.maxAttempts; n++ {
		err = c.attempt(ctx, job, h, n)
		switch {
		case err == nil:
			return obs.OutcomeSuccess, nil
		case errors.Is(err, ErrSkip):
			return obs.OutcomeSkipped, nil
		case IsPermanent(err) || n == c.o.maxAttempts:
			return obs.OutcomeDeadLetter, err
		}
		c.jobs.Add(ctx, 1, metric.WithAttributes(attribute.String(obs.AttrJobType, job.Type), attribute.String(obs.AttrOutcome, obs.OutcomeRetry)))
		pause(ctx, c.o.backoff(n))
	}
	return obs.OutcomeDeadLetter, err
}

func (c *Consumer) attempt(ctx context.Context, job Job, h Handler, n int) (err error) {
	ctx, span := c.tracer.Start(ctx, "job.attempt", trace.WithAttributes(attribute.Int(obs.AttrJobAttempt, n)))
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic in handler: %v", r)
		}
		if err != nil && !errors.Is(err, ErrSkip) {
			kind := "transient"
			if IsPermanent(err) {
				kind = "permanent"
			}
			span.RecordError(err)
			span.SetStatus(codes.Error, "attempt failed")
			span.SetAttributes(semconv.ErrorTypeKey.String(kind))
		}
		span.End()
	}()
	return h(ctx, job)
}

func (c *Consumer) deadLetter(ctx context.Context, m redis.XMessage, job Job, cause error) {
	vals := map[string]any{"original_id": m.ID, "error": fmt.Sprint(cause), "failed_at": time.Now().UTC().Format(time.RFC3339Nano)}
	for k, v := range m.Values {
		vals[k] = v
	}
	if err := c.rdb.XAdd(ctx, &redis.XAddArgs{Stream: c.o.deadLetter, MaxLen: c.o.maxLen, Approx: true, Values: vals}).Err(); err != nil {
		trace.SpanFromContext(ctx).RecordError(err)
	}
	if c.o.onDeadLetter == nil {
		return
	}
	// The hook runs on ctx == context.WithoutCancel(<Run's ctx>) (see Run), so
	// shutdown alone never stops it: bound it ourselves (ruling R27) so a slow
	// or hanging hook cannot pin this goroutine's sem slot and, through it,
	// keep Run's deferred wg.Wait() from ever returning. A well-behaved hook
	// that honors ctx cancellation stops on its own when hookCtx expires; one
	// that ignores it is simply abandoned running in the background — that
	// leaked goroutine is an accepted cost, not tracked further.
	hookCtx, cancel := context.WithTimeout(ctx, deadLetterHookTimeout)
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.o.onDeadLetter(hookCtx, job, cause)
	}()
	select {
	case <-done:
	case <-hookCtx.Done():
		trace.SpanFromContext(ctx).RecordError(
			fmt.Errorf("dead-letter hook exceeded %s, abandoning it", deadLetterHookTimeout))
	}
	cancel()
}

// pause sleeps for d or until ctx is done.
func pause(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}
