package cache

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"image-gallery/internal/config"
	"image-gallery/internal/domain/image"
)

func TestNewRedisClient(t *testing.T) {
	tests := []struct {
		name        string
		config      config.CacheConfig
		expectError bool
	}{
		{
			name: "cache disabled",
			config: config.CacheConfig{
				Enabled: false,
			},
			expectError: true,
		},
		{
			name: "invalid redis address",
			config: config.CacheConfig{
				Enabled:     true,
				Address:     "invalid:address:123",
				DialTimeout: 1 * time.Second,
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewRedisClient(tt.config)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, client)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, client)
				if client != nil {
					_ = client.Close() //nolint:errcheck // Connection cleanup in test
				}
			}
		})
	}
}

func TestRedisClient_ImageOperations(t *testing.T) {
	// Skip if Redis is not available
	client := getTestRedisClient(t)
	if client == nil {
		t.Skip("Redis not available for testing")
		return
	}
	defer func() { _ = client.Close() }() //nolint:errcheck // Resource cleanup

	ctx := context.Background()

	// Test data
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

	t.Run("SetImage and GetImage", func(t *testing.T) {
		// Set image in cache
		err := client.SetImage(ctx, testImage, 3600)
		require.NoError(t, err)

		// Get image from cache
		cachedImage, err := client.GetImage(ctx, testImage.ID)
		require.NoError(t, err)
		assert.Equal(t, testImage.ID, cachedImage.ID)
		assert.Equal(t, testImage.Filename, cachedImage.Filename)
		assert.Equal(t, testImage.ContentType, cachedImage.ContentType)
	})

	t.Run("GetImage not found", func(t *testing.T) {
		// Try to get non-existent image
		_, err := client.GetImage(ctx, 99999)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found in cache")
	})

	t.Run("DeleteImage", func(t *testing.T) {
		// First set an image
		err := client.SetImage(ctx, testImage, 3600)
		require.NoError(t, err)

		// Verify it exists
		_, err = client.GetImage(ctx, testImage.ID)
		require.NoError(t, err)

		// Delete the image
		err = client.DeleteImage(ctx, testImage.ID)
		require.NoError(t, err)

		// Verify it's gone
		_, err = client.GetImage(ctx, testImage.ID)
		assert.Error(t, err)
	})
}

func TestRedisClient_ImageListOperations(t *testing.T) {
	client := getTestRedisClient(t)
	if client == nil {
		t.Skip("Redis not available for testing")
		return
	}
	defer func() { _ = client.Close() }() //nolint:errcheck // Resource cleanup

	ctx := context.Background()

	// Test data
	testResponse := &image.ListImagesResponse{
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

	cacheKey := "test_list_key"

	t.Run("SetImageList and GetImageList", func(t *testing.T) {
		// Set image list in cache
		err := client.SetImageList(ctx, cacheKey, testResponse, 1800)
		require.NoError(t, err)

		// Get image list from cache
		cachedResponse, err := client.GetImageList(ctx, cacheKey)
		require.NoError(t, err)
		assert.Equal(t, testResponse.TotalCount, cachedResponse.TotalCount)
		assert.Equal(t, len(testResponse.Images), len(cachedResponse.Images))
		assert.Equal(t, testResponse.Images[0].Filename, cachedResponse.Images[0].Filename)
	})

	t.Run("InvalidateImageLists", func(t *testing.T) {
		// Set multiple image lists
		err := client.SetImageList(ctx, "list1", testResponse, 1800)
		require.NoError(t, err)
		err = client.SetImageList(ctx, "list2", testResponse, 1800)
		require.NoError(t, err)

		// Verify they exist
		_, err = client.GetImageList(ctx, "list1")
		require.NoError(t, err)
		_, err = client.GetImageList(ctx, "list2")
		require.NoError(t, err)

		// Invalidate all lists
		err = client.InvalidateImageLists(ctx)
		require.NoError(t, err)

		// Verify they're gone
		_, err = client.GetImageList(ctx, "list1")
		assert.Error(t, err)
		_, err = client.GetImageList(ctx, "list2")
		assert.Error(t, err)
	})
}

func TestRedisClient_StatsOperations(t *testing.T) {
	client := getTestRedisClient(t)
	if client == nil {
		t.Skip("Redis not available for testing")
		return
	}
	defer func() { _ = client.Close() }() //nolint:errcheck // Resource cleanup

	ctx := context.Background()

	// Test data
	testStats := map[string]interface{}{
		"total_images": 100,
		"total_size":   1024000,
		"categories": map[string]int{
			"small":  20,
			"medium": 50,
			"large":  30,
		},
	}

	statsKey := "test_stats"

	t.Run("SetStats and GetStats", func(t *testing.T) {
		// Set stats in cache
		err := client.SetStats(ctx, statsKey, testStats, 3600)
		require.NoError(t, err)

		// Get stats from cache
		cachedStats, err := client.GetStats(ctx, statsKey)
		require.NoError(t, err)
		assert.NotNil(t, cachedStats)
		// Note: JSON marshaling/unmarshaling may change types, so we just check it's not nil
	})
}

func TestRedisClient_HealthAndInfo(t *testing.T) {
	client := getTestRedisClient(t)
	if client == nil {
		t.Skip("Redis not available for testing")
		return
	}
	defer func() { _ = client.Close() }() //nolint:errcheck // Resource cleanup

	ctx := context.Background()

	t.Run("Health", func(t *testing.T) {
		err := client.Health(ctx)
		assert.NoError(t, err)
	})

	t.Run("GetCacheInfo", func(t *testing.T) {
		info, err := client.GetCacheInfo(ctx)
		assert.NoError(t, err)
		assert.NotNil(t, info)
		assert.Contains(t, info, "info")
		assert.Contains(t, info, "db_size")
	})
}

func TestGenerateListKey(t *testing.T) {
	tests := []struct {
		name     string
		req      *image.ListImagesRequest
		expected string
	}{
		{
			name: "basic request",
			req: &image.ListImagesRequest{
				Page:     1,
				PageSize: 10,
				Tag:      "landscape",
			},
			expected: "page_1_pagesize_10_tag_landscape",
		},
		{
			name: "request with different tag",
			req: &image.ListImagesRequest{
				Page:     2,
				PageSize: 20,
				Tag:      "nature",
			},
			expected: "page_2_pagesize_20_tag_nature",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateListKey(tt.req)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGenerateStatsKey(t *testing.T) {
	tests := []struct {
		name      string
		statsType string
		params    []string
		expected  string
	}{
		{
			name:      "simple stats key",
			statsType: "image_stats",
			params:    nil,
			expected:  "image_stats",
		},
		{
			name:      "stats with parameters",
			statsType: "user_stats",
			params:    []string{"user123", "monthly"},
			expected:  "user_stats_user123_monthly",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateStatsKey(tt.statsType, tt.params...)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// getTestRedisClient creates a Redis client for testing
// Returns nil if Redis is not available
func getTestRedisClient(t *testing.T) *RedisClient {
	cfg := config.CacheConfig{
		Enabled:     true,
		Address:     "localhost:6379",
		Password:    "",
		Database:    1, // Use database 1 for tests
		DialTimeout: 5 * time.Second,
		DefaultTTL:  1 * time.Hour,
	}

	client, err := NewRedisClient(cfg)
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
