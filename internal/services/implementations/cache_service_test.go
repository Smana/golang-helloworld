package implementations

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"image-gallery/internal/config"
	"image-gallery/internal/domain/image"
	"image-gallery/internal/platform/cache"
)

func TestNewCacheService(t *testing.T) {
	// Test with nil client
	service := NewCacheService(nil)
	assert.NotNil(t, service)

	// Test with real client (if Redis is available)
	if client := getTestRedisClient(t); client != nil {
		defer func() { _ = client.Close() }() //nolint:errcheck // Resource cleanup
		service = NewCacheService(client)
		assert.NotNil(t, service)
	}
}

func TestCacheService_WithNilClient(t *testing.T) {
	service := NewCacheService(nil)
	ctx := context.Background()

	testImage := &image.Image{
		ID:          1,
		Filename:    "test.jpg",
		ContentType: "image/jpeg",
	}

	t.Run("GetImage returns cache unavailable", func(t *testing.T) {
		_, err := service.GetImage(ctx, 1)
		assert.Equal(t, image.ErrCacheUnavailable, err)
	})

	t.Run("SetImage succeeds silently", func(t *testing.T) {
		err := service.SetImage(ctx, testImage, 3600)
		assert.NoError(t, err)
	})

	t.Run("DeleteImage succeeds silently", func(t *testing.T) {
		err := service.DeleteImage(ctx, 1)
		assert.NoError(t, err)
	})

	t.Run("GetImageList returns cache unavailable", func(t *testing.T) {
		_, err := service.GetImageList(ctx, "test")
		assert.Equal(t, image.ErrCacheUnavailable, err)
	})

	t.Run("SetImageList succeeds silently", func(t *testing.T) {
		response := &image.ListImagesResponse{
			Images:     []image.Image{*testImage},
			TotalCount: 1,
		}
		err := service.SetImageList(ctx, "test", response, 1800)
		assert.NoError(t, err)
	})

	t.Run("InvalidateImageLists succeeds silently", func(t *testing.T) {
		err := service.InvalidateImageLists(ctx)
		assert.NoError(t, err)
	})

	t.Run("GetStats returns cache unavailable", func(t *testing.T) {
		_, err := service.GetStats(ctx, "test")
		assert.Equal(t, image.ErrCacheUnavailable, err)
	})

	t.Run("SetStats succeeds silently", func(t *testing.T) {
		stats := map[string]int{"total": 100}
		err := service.SetStats(ctx, "test", stats, 3600)
		assert.NoError(t, err)
	})

	t.Run("Health returns cache unavailable", func(t *testing.T) {
		err := service.Health(ctx)
		assert.Equal(t, image.ErrCacheUnavailable, err)
	})
}

func TestCacheService_WithRedisClient(t *testing.T) {
	client := getTestRedisClient(t)
	if client == nil {
		t.Skip("Redis not available for testing")
		return
	}
	defer func() { _ = client.Close() }() //nolint:errcheck // Resource cleanup

	service := NewCacheService(client)
	ctx := context.Background()

	testImage := &image.Image{
		ID:               1,
		Filename:         "test.jpg",
		OriginalFilename: "original_test.jpg",
		ContentType:      "image/jpeg",
		FileSize:         1024,
		StoragePath:      "/storage/test.jpg",
		UploadedAt:       time.Now(),
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	t.Run("Image operations", func(t *testing.T) {
		// Set image
		err := service.SetImage(ctx, testImage, 3600)
		require.NoError(t, err)

		// Get image
		cachedImage, err := service.GetImage(ctx, testImage.ID)
		require.NoError(t, err)
		assert.Equal(t, testImage.ID, cachedImage.ID)
		assert.Equal(t, testImage.Filename, cachedImage.Filename)

		// Delete image
		err = service.DeleteImage(ctx, testImage.ID)
		require.NoError(t, err)

		// Verify deleted
		_, err = service.GetImage(ctx, testImage.ID)
		assert.Error(t, err)
	})

	t.Run("Image list operations", func(t *testing.T) {
		response := &image.ListImagesResponse{
			Images: []image.Image{
				{
					ID:          1,
					Filename:    "test1.jpg",
					ContentType: "image/jpeg",
				},
				{
					ID:          2,
					Filename:    "test2.png",
					ContentType: "image/png",
				},
			},
			TotalCount: 2,
			Page:       1,
			PageSize:   10,
		}

		cacheKey := "test_list"

		// Set image list
		err := service.SetImageList(ctx, cacheKey, response, 1800)
		require.NoError(t, err)

		// Get image list
		cachedResponse, err := service.GetImageList(ctx, cacheKey)
		require.NoError(t, err)
		assert.Equal(t, response.TotalCount, cachedResponse.TotalCount)
		assert.Equal(t, len(response.Images), len(cachedResponse.Images))

		// Invalidate lists
		err = service.InvalidateImageLists(ctx)
		require.NoError(t, err)

		// Verify invalidated
		_, err = service.GetImageList(ctx, cacheKey)
		assert.Error(t, err)
	})

	t.Run("Stats operations", func(t *testing.T) {
		stats := map[string]interface{}{
			"total_images": 100,
			"total_size":   1024000,
		}

		statsKey := "test_stats"

		// Set stats
		err := service.SetStats(ctx, statsKey, stats, 3600)
		require.NoError(t, err)

		// Get stats
		cachedStats, err := service.GetStats(ctx, statsKey)
		require.NoError(t, err)
		assert.NotNil(t, cachedStats)
	})

	t.Run("Health check", func(t *testing.T) {
		err := service.Health(ctx)
		assert.NoError(t, err)
	})
}

// getTestRedisClient creates a Redis client for testing
// Returns nil if Redis is not available
func getTestRedisClient(t *testing.T) *cache.RedisClient {
	cfg := config.CacheConfig{
		Enabled:     true,
		Address:     "localhost:6379",
		Password:    "",
		Database:    1, // Use database 1 for tests
		DialTimeout: 5 * time.Second,
		DefaultTTL:  1 * time.Hour,
	}

	client, err := cache.NewRedisClient(cfg)
	if err != nil {
		t.Logf("Redis not available for testing: %v", err)
		return nil
	}

	// Clean up test database before tests
	ctx := context.Background()
	if err := client.FlushCache(ctx); err != nil {
		t.Logf("Failed to clean test database: %v", err)
	}

	return client
}
