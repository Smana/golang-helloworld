package observability

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"
)

// Logger wraps zerolog with OpenTelemetry trace correlation
type Logger struct {
	logger zerolog.Logger
}

// NewLogger writes JSON (or console) logs to stdout.
func NewLogger(config Config) *Logger { return NewLoggerTo(os.Stdout, config) }

// NewLoggerTo writes to w; tests pass a buffer.
func NewLoggerTo(w io.Writer, config Config) *Logger {
	output := w
	if config.LogFormat == "console" {
		output = zerolog.ConsoleWriter{Out: w, TimeFormat: time.RFC3339}
	}
	baseLogger := zerolog.New(output).
		Level(parseLogLevel(config.LogLevel)).
		With().
		Timestamp().
		Str("service.name", config.ServiceName).
		Str("service.version", config.ServiceVersion).
		Str("deployment.environment.name", config.Environment).
		Logger()
	return &Logger{logger: baseLogger}
}

// parseLogLevel converts string log level to zerolog.Level
func parseLogLevel(level string) zerolog.Level {
	switch level {
	case "debug":
		return zerolog.DebugLevel
	case "info":
		return zerolog.InfoLevel
	case "warn", "warning":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	case "fatal":
		return zerolog.FatalLevel
	case "panic":
		return zerolog.PanicLevel
	default:
		return zerolog.InfoLevel
	}
}

// WithContext returns a logger with trace context information
func (l *Logger) WithContext(ctx context.Context) *zerolog.Logger {
	logger := l.logger

	// Extract trace context
	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.IsValid() {
		logger = logger.With().
			Str("trace_id", spanCtx.TraceID().String()).
			Str("span_id", spanCtx.SpanID().String()).
			Bool("trace_sampled", spanCtx.IsSampled()).
			Logger()
	}

	return &logger
}

// Info returns an info level event with trace context
func (l *Logger) Info(ctx context.Context) *zerolog.Event {
	return l.WithContext(ctx).Info()
}

// Debug returns a debug level event with trace context
func (l *Logger) Debug(ctx context.Context) *zerolog.Event {
	return l.WithContext(ctx).Debug()
}

// Warn returns a warn level event with trace context
func (l *Logger) Warn(ctx context.Context) *zerolog.Event {
	return l.WithContext(ctx).Warn()
}

// Error returns an error level event with trace context
func (l *Logger) Error(ctx context.Context) *zerolog.Event {
	return l.WithContext(ctx).Error()
}

// Fatal returns a fatal level event with trace context
func (l *Logger) Fatal(ctx context.Context) *zerolog.Event {
	return l.WithContext(ctx).Fatal()
}

// GetZerolog returns the underlying zerolog.Logger for direct access
func (l *Logger) GetZerolog() *zerolog.Logger {
	return &l.logger
}

// WithFields returns a logger with additional fields
func (l *Logger) WithFields(fields map[string]interface{}) *Logger {
	ctx := l.logger.With()
	for k, v := range fields {
		ctx = ctx.Interface(k, v)
	}
	return &Logger{
		logger: ctx.Logger(),
	}
}

// OTELErrorHandler returns a function that handles OTEL errors using structured logging
func (l *Logger) OTELErrorHandler() func(error) {
	return func(err error) {
		l.logger.Error().
			Err(err).
			Str("source", "otel_sdk").
			Msg("OpenTelemetry SDK error")
	}
}
