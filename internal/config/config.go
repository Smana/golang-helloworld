// Package config provides application configuration management
// with validation, environment parsing, and Go 1.25 best practices
package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config represents the application configuration
type Config struct {
	Environment   string
	Port          string
	Host          string
	DatabaseURL   string
	Storage       StorageConfig
	Cache         CacheConfig
	Logging       *LoggingConfig
	Server        *ServerConfig
	Observability ObservabilityConfig
}

// StorageConfig holds object storage configuration
type StorageConfig struct {
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	BucketName      string
	UseSSL          bool
	Region          string
	MaxUploadSize   int64
	AllowedTypes    []string
	SyncOnStartup   bool // Sync existing S3 objects to database on startup
}

// CacheConfig holds Redis cache configuration
type CacheConfig struct {
	Enabled         bool
	Address         string
	Password        string
	Database        int
	MaxRetries      int
	MinRetryBackoff time.Duration
	MaxRetryBackoff time.Duration
	DialTimeout     time.Duration
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	PoolSize        int
	MinIdleConns    int
	MaxIdleConns    int
	MaxConnAge      time.Duration
	PoolTimeout     time.Duration
	IdleTimeout     time.Duration
	DefaultTTL      time.Duration
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	Level  string
	Format string
	Output string
}

// ServerConfig holds HTTP server configuration
type ServerConfig struct {
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
}

// ObservabilityConfig holds OpenTelemetry configuration
type ObservabilityConfig struct {
	ServiceName      string
	ServiceVersion   string
	Environment      string
	PodName          string
	PodNamespace     string
	TracesEndpoint   string
	TracesEnabled    bool
	TracesSampler    string
	TracesSamplerArg string
	MetricsEndpoint  string
	MetricsEnabled   bool
}

// Load creates a new configuration from environment variables with validation
func Load() (*Config, error) {
	useSSL := parseBoolOrDefault(getEnv("STORAGE_USE_SSL", "false"), false)
	maxUploadSize := parseSize(getEnv("MAX_UPLOAD_SIZE", "10MB"))
	allowedTypes := parseList(getEnv("ALLOWED_FILE_TYPES", "image/jpeg,image/png,image/gif,image/webp"))

	readTimeout := parseDurationOrDefault(getEnv("READ_TIMEOUT", "10s"), 10*time.Second)
	writeTimeout := parseDurationOrDefault(getEnv("WRITE_TIMEOUT", "10s"), 10*time.Second)
	idleTimeout := parseDurationOrDefault(getEnv("SERVER_TIMEOUT", "30s"), 30*time.Second)

	// Cache configuration
	cacheEnabled := parseBoolOrDefault(getEnv("CACHE_ENABLED", "true"), true)
	cacheDB := parseIntOrDefault(getEnv("CACHE_DATABASE", "0"), 0)
	cacheMaxRetries := parseIntOrDefault(getEnv("CACHE_MAX_RETRIES", "3"), 3)
	cachePoolSize := parseIntOrDefault(getEnv("CACHE_POOL_SIZE", "10"), 10)
	cacheMinIdleConns := parseIntOrDefault(getEnv("CACHE_MIN_IDLE_CONNS", "5"), 5)
	cacheMaxIdleConns := parseIntOrDefault(getEnv("CACHE_MAX_IDLE_CONNS", "10"), 10)

	cacheMinRetryBackoff := parseDurationOrDefault(getEnv("CACHE_MIN_RETRY_BACKOFF", "8ms"), 8*time.Millisecond)
	cacheMaxRetryBackoff := parseDurationOrDefault(getEnv("CACHE_MAX_RETRY_BACKOFF", "512ms"), 512*time.Millisecond)
	cacheDialTimeout := parseDurationOrDefault(getEnv("CACHE_DIAL_TIMEOUT", "5s"), 5*time.Second)
	cacheReadTimeout := parseDurationOrDefault(getEnv("CACHE_READ_TIMEOUT", "3s"), 3*time.Second)
	cacheWriteTimeout := parseDurationOrDefault(getEnv("CACHE_WRITE_TIMEOUT", "3s"), 3*time.Second)
	cacheMaxConnAge := parseDurationOrDefault(getEnv("CACHE_MAX_CONN_AGE", "30m"), 30*time.Minute)
	cachePoolTimeout := parseDurationOrDefault(getEnv("CACHE_POOL_TIMEOUT", "4s"), 4*time.Second)
	cacheIdleTimeout := parseDurationOrDefault(getEnv("CACHE_IDLE_TIMEOUT", "5m"), 5*time.Minute)
	cacheDefaultTTL := parseDurationOrDefault(getEnv("CACHE_DEFAULT_TTL", "1h"), 1*time.Hour)

	goEnv := getEnv("GO_ENV", "development")

	config := &Config{
		Environment: goEnv,
		Port:        getEnv("PORT", "8080"),
		Host:        getEnv("HOST", "localhost"),
		DatabaseURL: getEnv("DATABASE_URL", ""),
		Storage: StorageConfig{
			Endpoint:        getEnv("STORAGE_ENDPOINT", "localhost:9000"),
			AccessKeyID:     getEnv("STORAGE_ACCESS_KEY", ""),
			SecretAccessKey: getEnv("STORAGE_SECRET_KEY", ""),
			BucketName:      getEnv("STORAGE_BUCKET", "images"),
			UseSSL:          useSSL,
			Region:          getEnv("STORAGE_REGION", "us-east-1"),
			MaxUploadSize:   maxUploadSize,
			AllowedTypes:    allowedTypes,
			SyncOnStartup:   parseBoolOrDefault(getEnv("STORAGE_SYNC_ON_STARTUP", "false"), false),
		},
		Cache: CacheConfig{
			Enabled:         cacheEnabled,
			Address:         getEnv("CACHE_ADDRESS", "localhost:6379"),
			Password:        getEnv("CACHE_PASSWORD", ""),
			Database:        cacheDB,
			MaxRetries:      cacheMaxRetries,
			MinRetryBackoff: cacheMinRetryBackoff,
			MaxRetryBackoff: cacheMaxRetryBackoff,
			DialTimeout:     cacheDialTimeout,
			ReadTimeout:     cacheReadTimeout,
			WriteTimeout:    cacheWriteTimeout,
			PoolSize:        cachePoolSize,
			MinIdleConns:    cacheMinIdleConns,
			MaxIdleConns:    cacheMaxIdleConns,
			MaxConnAge:      cacheMaxConnAge,
			PoolTimeout:     cachePoolTimeout,
			IdleTimeout:     cacheIdleTimeout,
			DefaultTTL:      cacheDefaultTTL,
		},
		Logging: &LoggingConfig{
			Level:  getEnv("LOG_LEVEL", "info"),
			Format: getEnv("LOG_FORMAT", "json"),
			Output: getEnv("LOG_OUTPUT", "stdout"),
		},
		Server: &ServerConfig{
			ReadTimeout:  readTimeout,
			WriteTimeout: writeTimeout,
			IdleTimeout:  idleTimeout,
		},
		Observability: ObservabilityConfig{
			ServiceName:      getEnv("OTEL_SERVICE_NAME", "image-gallery"),
			ServiceVersion:   getEnv("OTEL_SERVICE_VERSION", "1.3.0"),
			Environment:      getEnv("OTEL_DEPLOYMENT_ENVIRONMENT", goEnv),
			PodName:          getEnv("POD_NAME", ""),
			PodNamespace:     getEnv("POD_NAMESPACE", ""),
			TracesEndpoint:   getEnv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "localhost:4318"),
			TracesEnabled:    parseBoolOrDefault(getEnv("OTEL_TRACES_ENABLED", "true"), true),
			TracesSampler:    getEnv("OTEL_TRACES_SAMPLER", "always_on"),
			TracesSamplerArg: getEnv("OTEL_TRACES_SAMPLER_ARG", "1.0"),
			MetricsEndpoint:  getEnv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "localhost:4318"),
			MetricsEnabled:   parseBoolOrDefault(getEnv("OTEL_METRICS_ENABLED", "true"), true),
		},
	}

	// Validate configuration before returning
	if err := config.Validate(); err != nil {
		return nil, err
	}

	return config, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// parseSize parses size strings like "10MB", "512KB" into bytes
func parseSize(sizeStr string) int64 {
	sizeStr = strings.ToUpper(strings.TrimSpace(sizeStr))

	if strings.HasSuffix(sizeStr, "MB") {
		numStr := strings.TrimSuffix(sizeStr, "MB")
		if num, err := strconv.ParseInt(numStr, 10, 64); err == nil {
			return num * 1024 * 1024
		}
	}

	if strings.HasSuffix(sizeStr, "KB") {
		numStr := strings.TrimSuffix(sizeStr, "KB")
		if num, err := strconv.ParseInt(numStr, 10, 64); err == nil {
			return num * 1024
		}
	}

	// Default to 10MB if parsing fails
	return 10 * 1024 * 1024
}

// parseList parses comma-separated strings into slices
func parseList(listStr string) []string {
	if listStr == "" {
		return []string{}
	}

	items := strings.Split(listStr, ",")
	result := make([]string, 0, len(items))

	for _, item := range items {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			result = append(result, trimmed)
		}
	}

	return result
}

// MustLoad loads configuration and panics on error
// Useful for startup scenarios where invalid config should crash the application
func MustLoad() *Config {
	config, err := Load()
	if err != nil {
		panic(err)
	}
	return config
}

// parseBoolOrDefault parses a boolean string, returning the default value on error
func parseBoolOrDefault(value string, defaultValue bool) bool {
	if result, err := strconv.ParseBool(value); err == nil {
		return result
	}
	return defaultValue
}

// parseIntOrDefault parses an integer string, returning the default value on error
func parseIntOrDefault(value string, defaultValue int) int {
	if result, err := strconv.Atoi(value); err == nil {
		return result
	}
	return defaultValue
}

// parseDurationOrDefault parses a duration string, returning the default value on error
func parseDurationOrDefault(value string, defaultValue time.Duration) time.Duration {
	if result, err := time.ParseDuration(value); err == nil {
		return result
	}
	return defaultValue
}
