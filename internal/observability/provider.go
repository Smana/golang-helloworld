package observability

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/exemplar"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
)

// Provider manages OpenTelemetry providers lifecycle
type Provider struct {
	tracerProvider *sdktrace.TracerProvider
	meterProvider  *sdkmetric.MeterProvider
	config         Config
}

// telemetryScope is the meter scope for the SDK self-observation counters.
const telemetryScope = "image-gallery/telemetry"

// NewProvider builds OTLP/HTTP exporters from config and wires the SDK.
func NewProvider(ctx context.Context, config Config, logger *Logger) (*Provider, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	var traceExp sdktrace.SpanExporter
	if config.TracesEnabled {
		exp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(config.TracesEndpoint))
		if err != nil {
			return nil, fmt.Errorf("failed to create trace exporter: %w", err)
		}
		traceExp = exp
	}
	var reader sdkmetric.Reader
	if config.MetricsEnabled {
		exp, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(config.MetricsEndpoint))
		if err != nil {
			return nil, fmt.Errorf("failed to create metric exporter: %w", err)
		}
		reader = sdkmetric.NewPeriodicReader(exp, sdkmetric.WithInterval(15*time.Second))
	}
	return NewProviderWith(ctx, config, logger, traceExp, reader)
}

// NewProviderWith wires the SDK around a span exporter and a metric reader; a nil
// one disables that signal. Metrics come first so the span counters exist
// before the first span ends.
func NewProviderWith(ctx context.Context, config Config, logger *Logger, traceExp sdktrace.SpanExporter, reader sdkmetric.Reader) (*Provider, error) {
	if logger != nil {
		otel.SetErrorHandler(otel.ErrorHandlerFunc(logger.OTELErrorHandler()))
	}
	res, err := buildResource(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}
	p := &Provider{config: config}

	if reader != nil {
		p.meterProvider = sdkmetric.NewMeterProvider(
			sdkmetric.WithResource(res),
			sdkmetric.WithReader(reader),
			sdkmetric.WithExemplarFilter(exemplar.TraceBasedFilter),
			sdkmetric.WithView(createExponentialHistogramView()),
		)
		otel.SetMeterProvider(p.meterProvider)
		// Go runtime metrics (go.memory.used, go.memory.limit, go.goroutine.count, …): the saturation/OOM story.
		if err := runtime.Start(runtime.WithMeterProvider(p.meterProvider), runtime.WithMinimumReadMemStatsInterval(15*time.Second)); err != nil {
			return nil, fmt.Errorf("failed to start runtime metrics: %w", err)
		}
	}

	if traceExp != nil {
		meter := otel.Meter(telemetryScope) // no-op when metrics are disabled
		ended, err := meter.Int64Counter(MetricSpansEnded, metric.WithUnit("{span}"), metric.WithDescription("Sampled spans that ended"))
		if err != nil {
			return nil, err
		}
		exported, err := meter.Int64Counter(MetricSpansExported, metric.WithUnit("{span}"), metric.WithDescription("Spans handed to the exporter, by outcome"))
		if err != nil {
			return nil, err
		}
		sampler, err := createSampler(config)
		if err != nil {
			return nil, fmt.Errorf("failed to create sampler: %w", err)
		}
		// Queue sized for 100 % sampling at 25 req/s (~20 spans/request => ~500 spans/s):
		// 8192 spans is ~16 s of headroom, a bounded few MiB; overflow is counted, not hidden.
		p.tracerProvider = sdktrace.NewTracerProvider(
			sdktrace.WithResource(res),
			sdktrace.WithSampler(sampler),
			sdktrace.WithSpanProcessor(spanEndCounter{ended: ended}),
			sdktrace.WithBatcher(countingExporter{SpanExporter: traceExp, exported: exported},
				sdktrace.WithBatchTimeout(time.Second),
				sdktrace.WithMaxExportBatchSize(1024),
				sdktrace.WithMaxQueueSize(8192),
				sdktrace.WithExportTimeout(10*time.Second),
			),
		)
		otel.SetTracerProvider(p.tracerProvider)
	}

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	return p, nil
}

func buildResource(ctx context.Context, c Config) (*resource.Resource, error) {
	attrs := []attribute.KeyValue{
		semconv.ServiceName(c.ServiceName),
		semconv.ServiceVersion(c.ServiceVersion),
		semconv.DeploymentEnvironmentNameKey.String(c.Environment),
	}
	if c.PodName != "" {
		attrs = append(attrs, semconv.K8SPodName(c.PodName))
	}
	if c.PodNamespace != "" {
		attrs = append(attrs, semconv.K8SNamespaceName(c.PodNamespace))
	}
	return resource.New(ctx,
		resource.WithAttributes(attrs...),
		resource.WithFromEnv(), // OTEL_RESOURCE_ATTRIBUTES, e.g. cloud.provider=gcp from the cluster patch
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithProcess(),
	)
}

// createSampler creates a trace sampler based on configuration
func createSampler(config Config) (sdktrace.Sampler, error) {
	switch config.TracesSampler {
	case SamplerAlwaysOn:
		return sdktrace.AlwaysSample(), nil
	case SamplerAlwaysOff:
		return sdktrace.NeverSample(), nil
	case SamplerTraceIDRatio:
		ratio, err := strconv.ParseFloat(config.TracesSamplerArg, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid sampler arg: %w", err)
		}
		return sdktrace.TraceIDRatioBased(ratio), nil
	case SamplerParentBasedAlwaysOn:
		return sdktrace.ParentBased(sdktrace.AlwaysSample()), nil
	case SamplerParentBasedAlwaysOff:
		return sdktrace.ParentBased(sdktrace.NeverSample()), nil
	case SamplerParentBasedTraceIDRatio:
		ratio, err := strconv.ParseFloat(config.TracesSamplerArg, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid sampler arg: %w", err)
		}
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio)), nil
	default:
		return nil, fmt.Errorf("unknown sampler type: %s", config.TracesSampler)
	}
}

// createExponentialHistogramView creates a view that converts all histograms to exponential histograms with exemplars
func createExponentialHistogramView() sdkmetric.View {
	return sdkmetric.NewView(
		// Match all histogram instruments (ending with .duration, .size, etc.)
		sdkmetric.Instrument{Kind: sdkmetric.InstrumentKindHistogram},
		// Convert to exponential histogram with bucket-based exemplar reservoir
		sdkmetric.Stream{
			Aggregation: sdkmetric.AggregationBase2ExponentialHistogram{
				MaxSize:  160, // Maximum number of buckets (default: 160)
				MaxScale: 20,  // Maximum scale factor (default: 20, range: -10 to 20)
			},
		},
	)
}

// Tracer returns a tracer for the given instrumentation scope
func (p *Provider) Tracer(name string, opts ...trace.TracerOption) trace.Tracer {
	if p.tracerProvider == nil {
		return otel.Tracer(name, opts...)
	}
	return p.tracerProvider.Tracer(name, opts...)
}

// Meter returns a meter for the given instrumentation scope
func (p *Provider) Meter(name string, opts ...metric.MeterOption) metric.Meter {
	if p.meterProvider == nil {
		return otel.Meter(name, opts...)
	}
	return p.meterProvider.Meter(name, opts...)
}

// Shutdown gracefully shuts down the provider, flushing any remaining telemetry
func (p *Provider) Shutdown(ctx context.Context) error {
	var err error

	if p.tracerProvider != nil {
		if shutdownErr := p.tracerProvider.Shutdown(ctx); shutdownErr != nil {
			err = fmt.Errorf("failed to shutdown tracer provider: %w", shutdownErr)
		}
	}

	if p.meterProvider != nil {
		if shutdownErr := p.meterProvider.Shutdown(ctx); shutdownErr != nil {
			if err != nil {
				err = fmt.Errorf("%w; failed to shutdown meter provider: %w", err, shutdownErr)
			} else {
				err = fmt.Errorf("failed to shutdown meter provider: %w", shutdownErr)
			}
		}
	}

	return err
}

// ForceFlush flushes any pending telemetry
func (p *Provider) ForceFlush(ctx context.Context) error {
	var err error

	if p.tracerProvider != nil {
		if flushErr := p.tracerProvider.ForceFlush(ctx); flushErr != nil {
			err = fmt.Errorf("failed to flush tracer provider: %w", flushErr)
		}
	}

	if p.meterProvider != nil {
		if flushErr := p.meterProvider.ForceFlush(ctx); flushErr != nil {
			if err != nil {
				err = fmt.Errorf("%w; failed to flush meter provider: %w", err, flushErr)
			} else {
				err = fmt.Errorf("failed to flush meter provider: %w", flushErr)
			}
		}
	}

	return err
}
