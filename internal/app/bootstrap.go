// Package app holds what the long-running roles (serve, worker) share.
package app

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"

	"image-gallery/internal/config"
	"image-gallery/internal/observability"
	"image-gallery/internal/platform/cache"
	"image-gallery/internal/platform/database"
	"image-gallery/internal/platform/storage"
)

// Deps are the connections every role needs.
type Deps struct {
	Cfg    *config.Config
	Logger *observability.Logger
	OTel   *observability.Provider
	DB     *sql.DB
	Store  storage.ObjectStore
	Redis  *redis.Client // instrumented; nil when CACHE_ADDRESS is empty
}

// Bootstrap loads configuration and connects telemetry, Postgres, object
// storage and Valkey. defaultService names the role when OTEL_SERVICE_NAME is
// unset (local runs); in the cluster the composition or the sidecar env sets it.
func Bootstrap(ctx context.Context, defaultService string) (*Deps, error) {
	_ = godotenv.Load() //nolint:errcheck // .env is optional
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if os.Getenv("OTEL_SERVICE_NAME") == "" {
		cfg.Observability.ServiceName = defaultService
	}
	oc := observability.Config{
		ServiceName: cfg.Observability.ServiceName, ServiceVersion: cfg.Observability.ServiceVersion,
		Environment: cfg.Observability.Environment, PodName: cfg.Observability.PodName, PodNamespace: cfg.Observability.PodNamespace,
		TracesEndpoint: cfg.Observability.TracesEndpoint, TracesEnabled: cfg.Observability.TracesEnabled,
		TracesSampler: cfg.Observability.TracesSampler, TracesSamplerArg: cfg.Observability.TracesSamplerArg,
		MetricsEndpoint: cfg.Observability.MetricsEndpoint, MetricsEnabled: cfg.Observability.MetricsEnabled,
		LogLevel: cfg.Logging.Level, LogFormat: cfg.Logging.Format,
	}
	d := &Deps{Cfg: cfg, Logger: observability.NewLogger(oc)}
	if d.OTel, err = observability.NewProvider(ctx, oc, d.Logger); err != nil {
		return nil, fmt.Errorf("opentelemetry: %w", err)
	}
	if d.DB, err = database.NewConnection(cfg.DatabaseURL); err != nil {
		d.Close(ctx)
		return nil, fmt.Errorf("database: %w", err)
	}
	if d.Store, err = storage.NewObjectStore(ctx, cfg.Storage); err != nil {
		d.Close(ctx)
		return nil, fmt.Errorf("object storage (%s): %w", cfg.Storage.Provider, err)
	}
	if cfg.Cache.Address != "" {
		if d.Redis, err = cache.NewInstrumentedClient(cfg.Cache); err != nil {
			d.Close(ctx)
			return nil, fmt.Errorf("valkey: %w", err)
		}
	}
	d.Logger.GetZerolog().Info().Str("storage.provider", d.Store.Provider()).Msg("bootstrap complete")
	return d, nil
}

// Close flushes telemetry and releases connections, whatever was opened.
func (d *Deps) Close(ctx context.Context) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if d.Redis != nil {
		_ = d.Redis.Close() //nolint:errcheck // best-effort close on shutdown
	}
	if d.DB != nil {
		_ = d.DB.Close() //nolint:errcheck // best-effort close on shutdown
	}
	if d.OTel != nil {
		_ = d.OTel.ForceFlush(ctx) //nolint:errcheck // best-effort flush before shutdown
		_ = d.OTel.Shutdown(ctx)   //nolint:errcheck // best-effort shutdown
	}
}
