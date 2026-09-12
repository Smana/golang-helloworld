package observability

import (
	"testing"
)

func TestSamplerConstants(t *testing.T) {
	tests := []struct {
		name     string
		constant string
		expected string
	}{
		{"AlwaysOn", SamplerAlwaysOn, "always_on"},
		{"AlwaysOff", SamplerAlwaysOff, "always_off"},
		{"TraceIDRatio", SamplerTraceIDRatio, "traceidratio"},
		{"ParentBasedAlwaysOn", SamplerParentBasedAlwaysOn, "parentbased_always_on"},
		{"ParentBasedAlwaysOff", SamplerParentBasedAlwaysOff, "parentbased_always_off"},
		{"ParentBasedTraceIDRatio", SamplerParentBasedTraceIDRatio, "parentbased_traceidratio"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constant != tt.expected {
				t.Errorf("sampler constant %s = %s, want %s", tt.name, tt.constant, tt.expected)
			}
		})
	}
}

func TestValidateSampler(t *testing.T) {
	tests := []struct {
		name        string
		samplerType string
		samplerArg  string
		wantErr     bool
	}{
		{
			name:        "valid always_on",
			samplerType: SamplerAlwaysOn,
			samplerArg:  "1.0",
			wantErr:     false,
		},
		{
			name:        "valid parentbased_traceidratio",
			samplerType: SamplerParentBasedTraceIDRatio,
			samplerArg:  "0.1",
			wantErr:     false,
		},
		{
			name:        "valid traceidratio with 0.5",
			samplerType: SamplerTraceIDRatio,
			samplerArg:  "0.5",
			wantErr:     false,
		},
		{
			name:        "invalid sampler type",
			samplerType: "invalid_sampler",
			samplerArg:  "0.1",
			wantErr:     true,
		},
		{
			name:        "invalid ratio - too high",
			samplerType: SamplerTraceIDRatio,
			samplerArg:  "1.5",
			wantErr:     true,
		},
		{
			name:        "invalid ratio - negative",
			samplerType: SamplerTraceIDRatio,
			samplerArg:  "-0.1",
			wantErr:     true,
		},
		{
			name:        "invalid ratio - not a number",
			samplerType: SamplerTraceIDRatio,
			samplerArg:  "invalid",
			wantErr:     true,
		},
		{
			name:        "valid always_off doesn't need ratio",
			samplerType: SamplerAlwaysOff,
			samplerArg:  "ignored",
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSampler(tt.samplerType, tt.samplerArg)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateSampler() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config with always_on",
			config: Config{
				ServiceName:      "test-service",
				TracesEnabled:    true,
				TracesEndpoint:   "http://localhost:4318",
				TracesSampler:    SamplerAlwaysOn,
				TracesSamplerArg: "1.0",
				MetricsEnabled:   true,
				MetricsEndpoint:  "http://localhost:4318",
			},
			wantErr: false,
		},
		{
			name: "valid config with parentbased sampling",
			config: Config{
				ServiceName:      "test-service",
				TracesEnabled:    true,
				TracesEndpoint:   "http://localhost:4318",
				TracesSampler:    SamplerParentBasedTraceIDRatio,
				TracesSamplerArg: "0.1",
				MetricsEnabled:   true,
				MetricsEndpoint:  "http://localhost:4318",
			},
			wantErr: false,
		},
		{
			name: "missing service name",
			config: Config{
				TracesEnabled:  true,
				TracesEndpoint: "http://localhost:4318",
				TracesSampler:  SamplerAlwaysOn,
			},
			wantErr: true,
		},
		{
			name: "traces enabled but no endpoint",
			config: Config{
				ServiceName:   "test-service",
				TracesEnabled: true,
				TracesSampler: SamplerAlwaysOn,
			},
			wantErr: true,
		},
		{
			name: "invalid sampler configuration",
			config: Config{
				ServiceName:      "test-service",
				TracesEnabled:    true,
				TracesEndpoint:   "http://localhost:4318",
				TracesSampler:    "invalid",
				TracesSamplerArg: "0.1",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Config.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoadConfigDefaultsToBuiltVersion(t *testing.T) {
	defer func(v string) { DefaultServiceVersion = v }(DefaultServiceVersion)
	DefaultServiceVersion = "9.9.9-built"

	t.Setenv("OTEL_SERVICE_VERSION", "")
	if got := LoadConfig().ServiceVersion; got != "9.9.9-built" {
		t.Errorf("unset OTEL_SERVICE_VERSION: service version = %q, want the built version %q", got, "9.9.9-built")
	}
	t.Setenv("OTEL_SERVICE_VERSION", "1.2.3")
	if got := LoadConfig().ServiceVersion; got != "1.2.3" {
		t.Errorf("OTEL_SERVICE_VERSION=1.2.3: service version = %q, want the override", got)
	}
}

func TestLoadConfig(t *testing.T) {
	// Save original env vars
	originalVars := map[string]string{
		"OTEL_SERVICE_NAME":       getEnv("OTEL_SERVICE_NAME", ""),
		"OTEL_TRACES_SAMPLER":     getEnv("OTEL_TRACES_SAMPLER", ""),
		"OTEL_TRACES_SAMPLER_ARG": getEnv("OTEL_TRACES_SAMPLER_ARG", ""),
	}

	// Restore after test
	defer func() {
		for k, v := range originalVars {
			if v != "" {
				t.Setenv(k, v)
			}
		}
	}()

	// Test default configuration
	t.Run("default configuration", func(t *testing.T) {
		config := LoadConfig()

		if config.ServiceName != "image-gallery" {
			t.Errorf("Expected default service name 'image-gallery', got %s", config.ServiceName)
		}

		if config.TracesSampler != SamplerAlwaysOn {
			t.Errorf("Expected default sampler 'always_on', got %s", config.TracesSampler)
		}

		if config.TracesSamplerArg != "1.0" {
			t.Errorf("Expected default sampler arg '1.0', got %s", config.TracesSamplerArg)
		}
	})
}
