package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"image-gallery/internal/config"
	"image-gallery/internal/observability"
	"image-gallery/internal/platform/database"
	"image-gallery/internal/platform/server"
	"image-gallery/internal/platform/storage"
	"image-gallery/internal/services"
	"image-gallery/internal/web/handlers"

	"github.com/KimMachineGun/automemlimit/memlimit"
	"github.com/joho/godotenv"
)

func init() {
	// Automatically set GOMEMLIMIT to 90% of cgroup's memory limit
	// This helps prevent OOM kills in containerized environments
	if _, err := memlimit.Set(
		memlimit.WithRatio(0.9),
		memlimit.WithProvider(memlimit.FromCgroup),
		memlimit.WithLogger(slog.Default()),
	); err != nil {
		slog.Warn("Failed to set automatic memory limit", "error", err)
	}
}

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, using environment variables")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize OpenTelemetry configuration
	otelConfig := observability.Config{
		ServiceName:      cfg.Observability.ServiceName,
		ServiceVersion:   cfg.Observability.ServiceVersion,
		Environment:      cfg.Observability.Environment,
		PodName:          cfg.Observability.PodName,
		PodNamespace:     cfg.Observability.PodNamespace,
		TracesEndpoint:   cfg.Observability.TracesEndpoint,
		TracesEnabled:    cfg.Observability.TracesEnabled,
		TracesSampler:    cfg.Observability.TracesSampler,
		TracesSamplerArg: cfg.Observability.TracesSamplerArg,
		MetricsEndpoint:  cfg.Observability.MetricsEndpoint,
		MetricsEnabled:   cfg.Observability.MetricsEnabled,
		LogLevel:         cfg.Logging.Level,
		LogFormat:        cfg.Logging.Format,
	}

	// Initialize structured logger first (before OTEL provider)
	logger := observability.NewLogger(otelConfig)
	logger.GetZerolog().Info().
		Str("service", cfg.Observability.ServiceName).
		Str("version", cfg.Observability.ServiceVersion).
		Str("environment", cfg.Environment).
		Msg("Initializing image-gallery service")

	// Initialize OpenTelemetry provider with error handling
	otelProvider, err := observability.NewProvider(context.Background(), otelConfig, logger)
	if err != nil {
		logger.GetZerolog().Fatal().Err(err).Msg("Failed to initialize OpenTelemetry provider")
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := otelProvider.Shutdown(shutdownCtx); err != nil {
			logger.GetZerolog().Error().Err(err).Msg("Error shutting down OpenTelemetry provider")
		}
	}()
	logger.GetZerolog().Info().Msg("OpenTelemetry provider initialized")

	db, err := database.NewConnection(cfg.DatabaseURL)
	if err != nil {
		logger.GetZerolog().Fatal().Err(err).Msg("Failed to connect to database")
	}
	defer func() {
		if err := db.Close(); err != nil {
			logger.GetZerolog().Error().Err(err).Msg("Error closing database connection")
		}
	}()

	// Migrations are handled by Atlas - application never runs migrations:
	// - Local development: Use `make migrate` (Atlas CLI)
	// - Kubernetes: Atlas Operator handles migrations automatically
	logger.GetZerolog().Info().Msg("Database connected - migrations are handled by Atlas")

	storageClient, err := storage.NewMinIOClient(cfg.Storage)
	if err != nil {
		if closeErr := db.Close(); closeErr != nil {
			logger.GetZerolog().Error().Err(closeErr).Msg("Error closing database connection")
		}
		logger.GetZerolog().Fatal().Err(err).Msg("Failed to connect to storage")
	}
	logger.GetZerolog().Info().Msg("Storage client initialized")

	// Initialize dependency injection container with observability
	container, err := services.NewContainerWithObservability(cfg, db, storageClient, logger)
	if err != nil {
		if closeErr := db.Close(); closeErr != nil {
			logger.GetZerolog().Error().Err(closeErr).Msg("Error closing database connection")
		}
		logger.GetZerolog().Fatal().Err(err).Msg("Failed to initialize services container")
	}
	defer func() {
		if err := container.Close(); err != nil {
			logger.GetZerolog().Error().Err(err).Msg("Error closing services container")
		}
	}()
	logger.GetZerolog().Info().Msg("Services container initialized")

	// Sync existing S3 images to database if configured
	if cfg.Storage.SyncOnStartup {
		logger.GetZerolog().Info().Msg("Starting S3 bucket synchronization...")
		if err := syncExistingImages(context.Background(), container, logger); err != nil {
			logger.GetZerolog().Error().Err(err).Msg("Failed to sync existing images, continuing startup...")
		} else {
			logger.GetZerolog().Info().Msg("S3 bucket synchronization completed")
		}
	}

	handler := handlers.NewWithContainer(container)

	srv := server.New(cfg.Port, handler.Routes())

	go func() {
		logger.GetZerolog().Info().Str("port", cfg.Port).Msg("Server starting")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.GetZerolog().Fatal().Err(err).Msg("Server failed to start")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.GetZerolog().Info().Msg("Server shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Force flush telemetry before shutdown
	if flushErr := otelProvider.ForceFlush(ctx); flushErr != nil {
		logger.GetZerolog().Error().Err(flushErr).Msg("Failed to flush telemetry")
	}

	if err := srv.Shutdown(ctx); err != nil {
		logger.GetZerolog().Error().Err(err).Msg("Server forced to shutdown")
	}

	logger.GetZerolog().Info().Msg("Server exited gracefully")
	fmt.Println("Server exited")
}

// syncExistingImages synchronizes existing S3 objects to the database
func syncExistingImages(ctx context.Context, container *services.Container, logger *observability.Logger) error {
	// Get database connection
	dbRepo := container.DB()

	// Create a storage service wrapper to list objects
	storageSvc, err := storage.NewService(&container.Config().Storage)
	if err != nil {
		return fmt.Errorf("failed to create storage service: %w", err)
	}

	// List all objects in the bucket
	objects, err := storageSvc.ListObjects(ctx, "", 10000) // List up to 10k objects
	if err != nil {
		return fmt.Errorf("failed to list objects: %w", err)
	}

	logger.GetZerolog().Info().Int("count", len(objects)).Msg("Found objects in S3 bucket")

	synced := 0
	skipped := 0

	for _, obj := range objects {
		// Check if image already exists in database by storage path
		var existingImage struct{ ID int }
		err := dbRepo.QueryRowContext(ctx, "SELECT id FROM images WHERE storage_path = $1 LIMIT 1", obj.Key).Scan(&existingImage.ID)
		if err == nil {
			// Image already exists
			skipped++
			continue
		}

		// Extract original filename from metadata or use key
		originalFilename := obj.Key
		if metaFilename, ok := obj.UserMetadata["original-filename"]; ok {
			originalFilename = metaFilename
		}

		// Create image record in database
		// Note: We don't have the actual file data, so dimensions will be NULL
		// This is acceptable for existing images as they can be updated later
		_, err = dbRepo.ExecContext(ctx, `
			INSERT INTO images (original_filename, storage_path, content_type, file_size, uploaded_at, created_at, updated_at)
			VALUES ($1, $2, $3, $4, NOW(), NOW(), NOW())
			ON CONFLICT (storage_path) DO NOTHING
		`, originalFilename, obj.Key, obj.ContentType, obj.Size)

		if err != nil {
			logger.GetZerolog().Error().Err(err).Str("path", obj.Key).Msg("Failed to create image record")
			continue
		}

		synced++
		logger.GetZerolog().Debug().Str("path", obj.Key).Str("filename", originalFilename).Msg("Synced image to database")
	}

	logger.GetZerolog().Info().
		Int("synced", synced).
		Int("skipped", skipped).
		Int("total", len(objects)).
		Msg("S3 synchronization summary")

	return nil
}
