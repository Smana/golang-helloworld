package observability

import (
	"fmt"
	"os"
	"strconv"
)

// Sampler type constants
const (
	SamplerAlwaysOn                = "always_on"
	SamplerAlwaysOff               = "always_off"
	SamplerTraceIDRatio            = "traceidratio"
	SamplerParentBasedAlwaysOn     = "parentbased_always_on"
	SamplerParentBasedAlwaysOff    = "parentbased_always_off"
	SamplerParentBasedTraceIDRatio = "parentbased_traceidratio"
)

// Config holds configuration for OpenTelemetry instrumentation
type Config struct {
	// Service identification
	ServiceName    string
	ServiceVersion string
	Environment    string
	PodName        string
	PodNamespace   string

	// OTLP Trace Exporter configuration
	TracesEndpoint   string
	TracesEnabled    bool
	TracesSampler    string // Sampler type: "always_on", "always_off", "traceidratio", "parentbased_always_on", "parentbased_traceidratio"
	TracesSamplerArg string // Sampler argument (e.g., "0.1" for 10% sampling with traceidratio)

	// OTLP Metrics Exporter configuration
	MetricsEndpoint string
	MetricsEnabled  bool

	// Logging configuration
	LogLevel  string
	LogFormat string // json or console
}

// LoadConfig loads observability configuration from environment variables
func LoadConfig() Config {
	return Config{
		// Service identification
		ServiceName:    getEnv("OTEL_SERVICE_NAME", "image-gallery"),
		ServiceVersion: getEnv("OTEL_SERVICE_VERSION", "1.3.0"),
		Environment:    getEnv("OTEL_DEPLOYMENT_ENVIRONMENT", getEnv("GO_ENV", "development")),
		PodName:        os.Getenv("POD_NAME"),
		PodNamespace:   os.Getenv("POD_NAMESPACE"),

		// Traces configuration
		TracesEndpoint:   getEnv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://localhost:4318/v1/traces"),
		TracesEnabled:    getEnvBool("OTEL_TRACES_ENABLED", true),
		TracesSampler:    getEnv("OTEL_TRACES_SAMPLER", SamplerAlwaysOn),
		TracesSamplerArg: getEnv("OTEL_TRACES_SAMPLER_ARG", "1.0"),

		// Metrics configuration
		MetricsEndpoint: getEnv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "http://localhost:4318/v1/metrics"),
		MetricsEnabled:  getEnvBool("OTEL_METRICS_ENABLED", true),

		// Logging configuration
		LogLevel:  getEnv("LOG_LEVEL", "info"),
		LogFormat: getEnv("LOG_FORMAT", "json"),
	}
}

// Validate checks if the configuration is valid
func (c Config) Validate() error {
	if c.ServiceName == "" {
		return fmt.Errorf("service name is required")
	}

	if c.TracesEnabled && c.TracesEndpoint == "" {
		return fmt.Errorf("traces endpoint is required when traces are enabled")
	}

	if c.MetricsEnabled && c.MetricsEndpoint == "" {
		return fmt.Errorf("metrics endpoint is required when metrics are enabled")
	}

	// Validate sampler configuration
	if err := validateSampler(c.TracesSampler, c.TracesSamplerArg); err != nil {
		return err
	}

	return nil
}

// validateSampler validates the sampler type and argument
func validateSampler(samplerType, samplerArg string) error {
	validSamplers := map[string]bool{
		SamplerAlwaysOn:                true,
		SamplerAlwaysOff:               true,
		SamplerTraceIDRatio:            true,
		SamplerParentBasedAlwaysOn:     true,
		SamplerParentBasedAlwaysOff:    true,
		SamplerParentBasedTraceIDRatio: true,
	}

	if !validSamplers[samplerType] {
		return fmt.Errorf("invalid traces sampler: %s (valid options: always_on, always_off, traceidratio, parentbased_always_on, parentbased_always_off, parentbased_traceidratio)", samplerType)
	}

	// Validate sampler argument for ratio-based samplers
	if samplerType == SamplerTraceIDRatio || samplerType == SamplerParentBasedTraceIDRatio {
		ratio, err := strconv.ParseFloat(samplerArg, 64)
		if err != nil {
			return fmt.Errorf("invalid traces sampler arg (must be a float between 0.0 and 1.0): %s", samplerArg)
		}
		if ratio < 0.0 || ratio > 1.0 {
			return fmt.Errorf("traces sampler arg must be between 0.0 and 1.0, got: %f", ratio)
		}
	}

	return nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value == "true" || value == "1" || value == "yes"
}
