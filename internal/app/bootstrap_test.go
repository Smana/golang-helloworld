package app

import (
	"testing"

	"image-gallery/internal/config"
	"image-gallery/internal/observability"
)

func TestTelemetryConfigDefaultsToBuiltVersion(t *testing.T) {
	defer func(v string) { observability.DefaultServiceVersion = v }(observability.DefaultServiceVersion)
	observability.DefaultServiceVersion = "9.9.9-built"
	t.Setenv("GO_ENV", "test")

	for env, want := range map[string]string{"": "9.9.9-built", "1.2.3": "1.2.3"} {
		t.Setenv("OTEL_SERVICE_VERSION", env)
		cfg, err := config.Load()
		if err != nil {
			t.Fatalf("config.Load: %v", err)
		}
		if got := telemetryConfig(cfg, "xplane-image-gallery").ServiceVersion; got != want {
			t.Errorf("OTEL_SERVICE_VERSION=%q: service version = %q, want %q", env, got, want)
		}
	}
}
