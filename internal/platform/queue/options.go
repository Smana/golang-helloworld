package queue

import (
	"context"
	"os"
	"strconv"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

type options struct {
	tp           trace.TracerProvider
	mp           metric.MeterProvider
	prop         propagation.TextMapPropagator
	stream       string
	group        string
	deadLetter   string
	consumer     string
	maxLen       int64
	claimIdle    time.Duration
	maxAttempts  int
	backoff      func(attempt int) time.Duration
	concurrency  int
	block        time.Duration
	onDeadLetter func(context.Context, Job, error)
}

// Option configures a Producer or Consumer.
type Option func(*options)

func defaultOptions() options {
	host, err := os.Hostname() // the pod name in Kubernetes: one consumer per pod
	if err != nil || host == "" {
		host = "worker-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return options{
		tp: otel.GetTracerProvider(), mp: otel.GetMeterProvider(), prop: otel.GetTextMapPropagator(),
		stream: DefaultStream, group: DefaultGroup, deadLetter: DefaultDeadLetter, consumer: host,
		maxLen: 10000, claimIdle: 60 * time.Second, maxAttempts: 4, concurrency: 2, block: 2 * time.Second,
		backoff: func(attempt int) time.Duration { return 500 * time.Millisecond << (attempt - 1) },
	}
}

func build(opts []Option) options {
	o := defaultOptions()
	for _, fn := range opts {
		fn(&o)
	}
	return o
}

func WithTracerProvider(tp trace.TracerProvider) Option     { return func(o *options) { o.tp = tp } }
func WithMeterProvider(mp metric.MeterProvider) Option      { return func(o *options) { o.mp = mp } }
func WithPropagator(p propagation.TextMapPropagator) Option { return func(o *options) { o.prop = p } }
func WithStream(s string) Option                            { return func(o *options) { o.stream = s } }
func WithGroup(g string) Option                             { return func(o *options) { o.group = g } }
func WithDeadLetter(s string) Option                        { return func(o *options) { o.deadLetter = s } }
func WithConsumerName(n string) Option                      { return func(o *options) { o.consumer = n } }
func WithClaimIdle(d time.Duration) Option                  { return func(o *options) { o.claimIdle = d } }
func WithMaxAttempts(n int) Option                          { return func(o *options) { o.maxAttempts = n } }
func WithBackoff(f func(attempt int) time.Duration) Option  { return func(o *options) { o.backoff = f } }
func WithConcurrency(n int) Option                          { return func(o *options) { o.concurrency = n } }
func WithBlock(d time.Duration) Option                      { return func(o *options) { o.block = d } }

// WithDeadLetterHook registers a callback invoked after a job is dead-lettered.
func WithDeadLetterHook(f func(context.Context, Job, error)) Option {
	return func(o *options) { o.onDeadLetter = f }
}
