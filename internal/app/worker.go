package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"

	"image-gallery/internal/platform/queue"
	"image-gallery/internal/services"
	"image-gallery/internal/worker"
)

// RunWorker consumes image-gallery:jobs until ctx is canceled, finishing the
// jobs in flight before it returns. Unlike the web role (ruling R29: a
// missing Valkey must never be Fatal there, since uploads can still be
// served), a worker with no queue to read has nothing to do: it returns an
// error instead, but never calls Fatal, so the deferred Close still flushes
// telemetry.
func RunWorker(ctx context.Context) error {
	d, err := Bootstrap(ctx, "xplane-image-gallery-worker")
	if err != nil {
		return err
	}
	defer d.Close(ctx)
	if d.Redis == nil {
		return errors.New("the worker needs Valkey: set CACHE_ADDRESS and CACHE_ENABLED=true")
	}
	log := d.Logger.GetZerolog()

	container, err := services.NewContainerWithObservability(d.Cfg, d.DB, d.Store, d.Logger)
	if err != nil {
		return fmt.Errorf("services: %w", err)
	}
	proc := worker.NewProcessor(container.ImageRepository(), container.StorageService(), workerFaults(container), d.Logger)

	// The consumer gets the instrumented client (its reads/XACKs become db.client.* spans);
	// RegisterGauges gets a separate, uninstrumented one so its periodic polls never do.
	consumer, err := queue.NewConsumer(d.Redis,
		queue.WithConcurrency(envInt("WORKER_CONCURRENCY", 2)),
		queue.WithDeadLetterHook(proc.OnDeadLetter))
	if err != nil {
		return fmt.Errorf("consumer: %w", err)
	}
	if err := consumer.EnsureGroup(ctx); err != nil {
		return fmt.Errorf("consumer group: %w", err)
	}

	gaugeClient := redis.NewClient(&redis.Options{Addr: d.Cfg.Cache.Address, Password: d.Cfg.Cache.Password, DB: d.Cfg.Cache.Database})
	defer func() { _ = gaugeClient.Close() }() //nolint:errcheck // best-effort close on shutdown
	if err := queue.RegisterGauges(gaugeClient, otel.Meter("image-gallery/queue"), queue.DefaultStream, queue.DefaultGroup); err != nil {
		return fmt.Errorf("queue gauges: %w", err)
	}

	hs := &http.Server{
		Addr: envOr("WORKER_HEALTH_ADDR", ":8081"), Handler: worker.NewHealthHandler(consumer.Ready),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error().Err(err).Msg("worker health server failed")
		}
	}()

	log.Info().Str("stream", queue.DefaultStream).Str("group", queue.DefaultGroup).Msg("worker consuming")
	runErr := consumer.Run(ctx, proc.Handle)
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = hs.Shutdown(shutdownCtx) //nolint:errcheck // best-effort shutdown of the health server
	log.Info().Msg("worker stopped")
	return runErr
}

// workerFaults wires the demo-controls fault injector into the processor.
func workerFaults(c *services.Container) worker.Faults { return c.DemoInjector() }

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if n, err := strconv.Atoi(os.Getenv(key)); err == nil && n > 0 {
		return n
	}
	return def
}
