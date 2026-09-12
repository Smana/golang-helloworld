package observability

import (
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestCreateSampler(t *testing.T) {
	for sampler, want := range map[string]sdktrace.Sampler{
		SamplerAlwaysOn:                sdktrace.AlwaysSample(),
		SamplerAlwaysOff:               sdktrace.NeverSample(),
		SamplerTraceIDRatio:            sdktrace.TraceIDRatioBased(0.25),
		SamplerParentBasedAlwaysOn:     sdktrace.ParentBased(sdktrace.AlwaysSample()),
		SamplerParentBasedAlwaysOff:    sdktrace.ParentBased(sdktrace.NeverSample()),
		SamplerParentBasedTraceIDRatio: sdktrace.ParentBased(sdktrace.TraceIDRatioBased(0.25)),
	} {
		got, err := createSampler(Config{TracesSampler: sampler, TracesSamplerArg: "0.25"})
		if err != nil || got.Description() != want.Description() {
			t.Errorf("%s: got %v, %v; want %s", sampler, got, err, want.Description())
		}
	}
	for _, sampler := range []string{SamplerTraceIDRatio, SamplerParentBasedTraceIDRatio} {
		if _, err := createSampler(Config{TracesSampler: sampler, TracesSamplerArg: "half"}); err == nil {
			t.Errorf("%s accepted a non-numeric ratio", sampler)
		}
	}
}
