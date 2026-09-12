package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/rs/zerolog"

	"image-gallery/internal/observability"
	"image-gallery/internal/platform/queue"
	"image-gallery/internal/platform/server"
	"image-gallery/internal/platform/storage"
	"image-gallery/internal/services"
	"image-gallery/internal/services/implementations"
	"image-gallery/internal/web/handlers"
)

// RunServe runs the web role until ctx is canceled.
func RunServe(ctx context.Context) error {
	d, err := Bootstrap(ctx, "xplane-image-gallery")
	if err != nil {
		return err
	}
	defer d.Close(ctx)
	log := d.Logger.GetZerolog()

	container, err := services.NewContainerWithObservability(d.Cfg, d.DB, d.Store, d.Logger)
	if err != nil {
		return fmt.Errorf("services: %w", err)
	}
	if err := wireJobPublisher(d, container); err != nil {
		return err
	}
	if d.Cfg.Storage.SyncOnStartup {
		if err := syncExistingImages(ctx, container, d.Logger); err != nil {
			log.Error().Err(err).Msg("Failed to sync existing images, continuing startup")
		}
	}

	srv := server.New(d.Cfg.Port, handlers.NewWithContainer(container).Routes())
	return serveUntilShutdown(ctx, srv, d.Cfg.Port, log)
}

// wireJobPublisher attaches the Valkey-backed job publisher when a Redis
// connection is configured. Without it, uploads stay pending for a worker
// that can never pick them up (local/no-queue runs).
func wireJobPublisher(d *Deps, container *services.Container) error {
	if d.Redis == nil {
		return nil
	}
	producer, err := queue.NewProducer(d.Redis)
	if err != nil {
		return fmt.Errorf("job producer: %w", err)
	}
	container.UseJobPublisher(implementations.NewQueueJobPublisher(producer))
	return nil
}

// serveUntilShutdown runs srv until ctx is canceled or it fails, then drains
// in-flight requests with a bounded timeout.
func serveUntilShutdown(ctx context.Context, srv *http.Server, port string, log *zerolog.Logger) error {
	errCh := make(chan error, 1)
	go func() {
		log.Info().Str("port", port).Msg("web role listening")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()
	select {
	case <-ctx.Done():
	case err := <-errCh:
		return err
	}
	log.Info().Msg("web role shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// syncExistingImages synchronizes existing S3 objects to the database
func syncExistingImages(ctx context.Context, container *services.Container, logger *observability.Logger) error {
	// Get database connection
	dbRepo := container.DB()

	// Create a storage service wrapper to list objects
	storageSvc, err := storage.NewService(&container.Config().Storage, container.ObjectStore())
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
