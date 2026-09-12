package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"

	"image-gallery/internal/config"
	"image-gallery/internal/domain/image"
)

// RedisClient wraps the Redis client with application-specific functionality
// Note: This works with both Redis and Valkey (Redis-compatible)
type RedisClient struct {
	client     *redis.Client
	defaultTTL time.Duration
}

// NewRedisClient creates a new Redis client with the provided configuration
// Note: This works with both Redis and Valkey (Redis-compatible)
func NewRedisClient(cfg config.CacheConfig) (*RedisClient, error) {
	if !cfg.Enabled {
		return nil, fmt.Errorf("cache is disabled")
	}

	rdb, err := NewInstrumentedClient(cfg)
	if err != nil {
		return nil, err
	}

	return &RedisClient{
		client:     rdb,
		defaultTTL: cfg.DefaultTTL,
	}, nil
}

// NewInstrumentedClient returns a Valkey client with redisotel tracing and
// metrics (db.client.* spans and connection-pool metrics), after a ping.
func NewInstrumentedClient(cfg config.CacheConfig) (*redis.Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr: cfg.Address, Password: cfg.Password, DB: cfg.Database,
		MaxRetries: cfg.MaxRetries, MinRetryBackoff: cfg.MinRetryBackoff, MaxRetryBackoff: cfg.MaxRetryBackoff,
		DialTimeout: cfg.DialTimeout, ReadTimeout: cfg.ReadTimeout, WriteTimeout: cfg.WriteTimeout,
		PoolSize: cfg.PoolSize, MinIdleConns: cfg.MinIdleConns, PoolTimeout: cfg.PoolTimeout,
	})
	if err := redisotel.InstrumentTracing(rdb); err != nil {
		return nil, fmt.Errorf("redisotel tracing: %w", err)
	}
	if err := redisotel.InstrumentMetrics(rdb); err != nil {
		return nil, fmt.Errorf("redisotel metrics: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis/Valkey: %w", err)
	}
	return rdb, nil
}

// getCachedValue is a helper method to get and unmarshal cached values
func (r *RedisClient) getCachedValue(ctx context.Context, key, notFoundMsg, getErrMsg, unmarshalErrMsg string, result interface{}) error {
	val, err := r.client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return fmt.Errorf("%s", notFoundMsg)
		}
		return fmt.Errorf("%s: %w", getErrMsg, err)
	}

	if err := json.Unmarshal([]byte(val), result); err != nil {
		return fmt.Errorf("%s: %w", unmarshalErrMsg, err)
	}

	return nil
}

// GetImage retrieves a cached image
func (r *RedisClient) GetImage(ctx context.Context, id int) (*image.Image, error) {
	key := fmt.Sprintf("image:%d", id)
	var img image.Image

	if err := r.getCachedValue(ctx, key, "image not found in cache", "failed to get image from cache", "failed to unmarshal cached image", &img); err != nil {
		return nil, err
	}

	return &img, nil
}

// SetImage caches an image
func (r *RedisClient) SetImage(ctx context.Context, img *image.Image, expiry int64) error {
	key := fmt.Sprintf("image:%d", img.ID)

	data, err := json.Marshal(img)
	if err != nil {
		return fmt.Errorf("failed to marshal image: %w", err)
	}

	var ttl time.Duration
	if expiry > 0 {
		ttl = time.Duration(expiry) * time.Second
	} else {
		ttl = r.defaultTTL
	}

	if err := r.client.Set(ctx, key, data, ttl).Err(); err != nil {
		return fmt.Errorf("failed to cache image: %w", err)
	}

	return nil
}

// DeleteImage removes an image from cache
func (r *RedisClient) DeleteImage(ctx context.Context, id int) error {
	key := fmt.Sprintf("image:%d", id)

	if err := r.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("failed to delete image from cache: %w", err)
	}

	return nil
}

// GetImageList retrieves a cached image list
func (r *RedisClient) GetImageList(ctx context.Context, key string) (*image.ListImagesResponse, error) {
	cacheKey := fmt.Sprintf("image_list:%s", key)
	var response image.ListImagesResponse

	if err := r.getCachedValue(ctx, cacheKey, "image list not found in cache", "failed to get image list from cache", "failed to unmarshal cached image list", &response); err != nil {
		return nil, err
	}

	return &response, nil
}

// SetImageList caches an image list
func (r *RedisClient) SetImageList(ctx context.Context, key string, response *image.ListImagesResponse, expiry int64) error {
	cacheKey := fmt.Sprintf("image_list:%s", key)

	data, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("failed to marshal image list: %w", err)
	}

	var ttl time.Duration
	if expiry > 0 {
		ttl = time.Duration(expiry) * time.Second
	} else {
		ttl = r.defaultTTL
	}

	if err := r.client.Set(ctx, cacheKey, data, ttl).Err(); err != nil {
		return fmt.Errorf("failed to cache image list: %w", err)
	}

	return nil
}

// InvalidateImageLists clears cached image lists
func (r *RedisClient) InvalidateImageLists(ctx context.Context) error {
	pattern := "image_list:*"

	keys, err := r.client.Keys(ctx, pattern).Result()
	if err != nil {
		return fmt.Errorf("failed to get cache keys: %w", err)
	}

	if len(keys) > 0 {
		if err := r.client.Del(ctx, keys...).Err(); err != nil {
			return fmt.Errorf("failed to delete cache keys: %w", err)
		}
	}

	return nil
}

// GetStats retrieves cached statistics
func (r *RedisClient) GetStats(ctx context.Context, key string) (interface{}, error) {
	cacheKey := fmt.Sprintf("stats:%s", key)
	var stats interface{}

	if err := r.getCachedValue(ctx, cacheKey, "stats not found in cache", "failed to get stats from cache", "failed to unmarshal cached stats", &stats); err != nil {
		return nil, err
	}

	return stats, nil
}

// SetStats caches statistics
func (r *RedisClient) SetStats(ctx context.Context, key string, stats interface{}, expiry int64) error {
	cacheKey := fmt.Sprintf("stats:%s", key)

	data, err := json.Marshal(stats)
	if err != nil {
		return fmt.Errorf("failed to marshal stats: %w", err)
	}

	var ttl time.Duration
	if expiry > 0 {
		ttl = time.Duration(expiry) * time.Second
	} else {
		ttl = r.defaultTTL
	}

	if err := r.client.Set(ctx, cacheKey, data, ttl).Err(); err != nil {
		return fmt.Errorf("failed to cache stats: %w", err)
	}

	return nil
}

// Health checks if the Redis/Valkey connection is healthy
func (r *RedisClient) Health(ctx context.Context) error {
	if err := r.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("Redis/Valkey health check failed: %w", err)
	}
	return nil
}

// Close closes the Redis/Valkey connection
func (r *RedisClient) Close() error {
	return r.client.Close()
}

// GetCacheInfo returns information about the cache
func (r *RedisClient) GetCacheInfo(ctx context.Context) (map[string]interface{}, error) {
	info := r.client.Info(ctx, "memory", "stats", "clients")
	if info.Err() != nil {
		return nil, fmt.Errorf("failed to get Redis/Valkey info: %w", info.Err())
	}

	dbSize := r.client.DBSize(ctx)
	if dbSize.Err() != nil {
		return nil, fmt.Errorf("failed to get Redis/Valkey DB size: %w", dbSize.Err())
	}

	return map[string]interface{}{
		"info":    info.Val(),
		"db_size": dbSize.Val(),
	}, nil
}

// FlushCache clears all cached data (use with caution)
func (r *RedisClient) FlushCache(ctx context.Context) error {
	if err := r.client.FlushDB(ctx).Err(); err != nil {
		return fmt.Errorf("failed to flush cache: %w", err)
	}
	return nil
}

// Generic cache methods for any type

// Get retrieves a cached value by key and unmarshals it into result
func (r *RedisClient) Get(ctx context.Context, key string, result interface{}) error {
	val, err := r.client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return fmt.Errorf("key not found in cache")
		}
		return fmt.Errorf("failed to get from cache: %w", err)
	}

	if err := json.Unmarshal([]byte(val), result); err != nil {
		return fmt.Errorf("failed to unmarshal cached value: %w", err)
	}

	return nil
}

// Set caches a value with the specified key and TTL
func (r *RedisClient) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal value: %w", err)
	}

	if ttl == 0 {
		ttl = r.defaultTTL
	}

	if err := r.client.Set(ctx, key, data, ttl).Err(); err != nil {
		return fmt.Errorf("failed to cache value: %w", err)
	}

	return nil
}

// Delete removes a value from cache by key
func (r *RedisClient) Delete(ctx context.Context, key string) error {
	if err := r.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("failed to delete from cache: %w", err)
	}

	return nil
}

// GenerateListKey generates a consistent cache key for image lists
func GenerateListKey(req *image.ListImagesRequest) string {
	return fmt.Sprintf("page_%d_pagesize_%d_tag_%s",
		req.Page, req.PageSize, req.Tag)
}

// GenerateStatsKey generates a consistent cache key for statistics
func GenerateStatsKey(statsType string, params ...string) string {
	key := statsType
	for _, param := range params {
		key += "_" + param
	}
	return key
}
