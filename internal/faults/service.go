// Package faults applies the demo controls: a cached controls service and an
// injector for the web middleware, the slow list query and the worker.
package faults

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"image-gallery/internal/domain/demo"
	obs "image-gallery/internal/observability"
)

const (
	cacheKey = "demo:controls"
	cacheTTL = 5 * time.Second // the worker sees a change within 5 s
)

// Cache is the subset of *cache.RedisClient the service uses. Pass a nil
// interface, never a nil *RedisClient, when there is no cache.
type Cache interface {
	Get(ctx context.Context, key string, result interface{}) error
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
}

// Service reads and writes the controls through a short-lived cache.
type Service struct {
	repo    demo.Repository
	cache   Cache
	lookups metric.Int64Counter
}

// NewService builds a Service; cache may be nil.
//
//nolint:errcheck // instrument name is a constant; the SDK returns a usable instrument alongside any error
func NewService(repo demo.Repository, cache Cache) *Service {
	lookups, _ := otel.Meter("image-gallery/demo").Int64Counter(obs.MetricCacheLookups, metric.WithUnit("{lookup}"))
	return &Service{repo: repo, cache: cache, lookups: lookups}
}

// Get returns the current controls.
func (s *Service) Get(ctx context.Context) (demo.Controls, error) {
	if s.cache != nil {
		var c demo.Controls
		if err := s.cache.Get(ctx, cacheKey, &c); err == nil {
			s.lookup(ctx, "hit")
			return c, nil
		}
		s.lookup(ctx, "miss")
	}
	c, err := s.repo.Get(ctx)
	if err == nil && s.cache != nil {
		_ = s.cache.Set(ctx, cacheKey, c, cacheTTL) //nolint:errcheck // best-effort cache warm; the repo read already succeeded
	}
	return c, err
}

// Update validates and saves c, then invalidates the cache.
func (s *Service) Update(ctx context.Context, c demo.Controls) (demo.Controls, error) {
	if err := c.Validate(); err != nil {
		return demo.Controls{}, err
	}
	if c.LatencyRoutes == nil {
		c.LatencyRoutes = []string{} // the column is NOT NULL
	}
	saved, err := s.repo.Save(ctx, c)
	if err == nil && s.cache != nil {
		_ = s.cache.Delete(ctx, cacheKey) //nolint:errcheck // best-effort invalidation; a stale entry expires within cacheTTL anyway
	}
	return saved, err
}

// Reset switches every control off.
func (s *Service) Reset(ctx context.Context) (demo.Controls, error) {
	return s.Update(ctx, demo.Controls{LatencyRoutes: []string{}})
}

func (s *Service) lookup(ctx context.Context, result string) {
	s.lookups.Add(ctx, 1, metric.WithAttributes(attribute.String(obs.AttrCacheName, "demo"), attribute.String(obs.AttrCacheResult, result)))
}
