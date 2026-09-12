package implementations

import (
	"context"
	"fmt"
	"time"

	"image-gallery/internal/domain/settings"
	obs "image-gallery/internal/observability"
	"image-gallery/internal/platform/cache"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const (
	settingsCacheKeyPrefix = "settings:"
	settingsCacheTTL       = 5 * time.Minute
	defaultUserID          = "default"
)

// SettingsServiceImpl implements the settings.SettingsService interface
type SettingsServiceImpl struct {
	repo  settings.Repository
	cache *cache.RedisClient // can be nil

	// Observability
	tracer       trace.Tracer
	settingsOps  metric.Int64Counter
	cacheLookups metric.Int64Counter
}

// NewSettingsService creates a new settings service implementation
//
//nolint:errcheck // instrument names are constants; the SDK returns a usable instrument alongside any error
func NewSettingsService(
	repo settings.Repository,
	redisCache *cache.RedisClient,
) settings.SettingsService {
	tracer := otel.Tracer("image-gallery/service/settings")
	meter := otel.Meter("image-gallery/service/settings")

	// Create metrics (ignore errors for graceful degradation)
	settingsOps, _ := meter.Int64Counter(obs.MetricSettingsOps, metric.WithUnit("{operation}"), metric.WithDescription("Settings operations by operation and source"))
	cacheLookups, _ := meter.Int64Counter(obs.MetricCacheLookups, metric.WithUnit("{lookup}"), metric.WithDescription("Cache lookups by cache and result"))

	return &SettingsServiceImpl{
		repo:         repo,
		cache:        redisCache,
		tracer:       tracer,
		settingsOps:  settingsOps,
		cacheLookups: cacheLookups,
	}
}

// GetSettings retrieves settings for a user (or default if not found)
func (s *SettingsServiceImpl) GetSettings(ctx context.Context, req *settings.GetSettingsRequest) (*settings.UserSettings, error) {
	userIDStr := s.getUserIDString(req)

	ctx, span := s.tracer.Start(ctx, "GetSettings",
		trace.WithAttributes(
			attribute.String("settings.user_id", userIDStr),
		),
	)
	defer span.End()

	// Try cache first
	if cached := s.tryGetFromCache(ctx, span, req); cached != nil {
		s.recordReadMetric(ctx, "cache")
		span.SetStatus(codes.Ok, "")
		return cached, nil
	}

	// Fetch from database with fallback to defaults
	result, err := s.fetchSettingsWithDefault(ctx, span, req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to get settings")
		return nil, fmt.Errorf("failed to get settings: %w", err)
	}

	// Cache the result
	s.cacheSettings(ctx, span, req, result)

	s.recordReadMetric(ctx, "database")
	span.SetAttributes(attribute.Int("settings.id", result.ID))
	span.SetStatus(codes.Ok, "")
	return result, nil
}

// getUserIDString extracts user ID as string from request
func (s *SettingsServiceImpl) getUserIDString(req *settings.GetSettingsRequest) string {
	if req != nil && req.UserID != nil {
		return *req.UserID
	}
	return defaultUserID
}

// tryGetFromCache attempts to retrieve settings from cache
func (s *SettingsServiceImpl) tryGetFromCache(ctx context.Context, span trace.Span, req *settings.GetSettingsRequest) *settings.UserSettings {
	if s.cache == nil {
		return nil
	}

	cacheKey := s.getCacheKey(req.UserID)
	span.AddEvent("checking_cache")

	var cached settings.UserSettings
	if err := s.cache.Get(ctx, cacheKey, &cached); err == nil {
		span.AddEvent("cache_hit")
		s.recordCacheHit(ctx)
		return &cached
	}

	span.AddEvent("cache_miss")
	s.recordCacheMiss(ctx)
	return nil
}

// fetchSettingsWithDefault fetches settings from database with fallback to defaults
func (s *SettingsServiceImpl) fetchSettingsWithDefault(ctx context.Context, span trace.Span, req *settings.GetSettingsRequest) (*settings.UserSettings, error) {
	span.AddEvent("querying_database")

	var result *settings.UserSettings
	var err error

	if req == nil || req.UserID == nil {
		// Get default settings
		result, err = s.repo.GetDefault(ctx)
	} else {
		// Get user-specific settings
		result, err = s.repo.GetByUserID(ctx, req.UserID)
		if err != nil {
			return nil, fmt.Errorf("failed to get user settings: %w", err)
		}

		// If user doesn't have settings, return default
		if result == nil {
			span.AddEvent("user_settings_not_found_returning_default")
			result, err = s.repo.GetDefault(ctx)
		}
	}

	if err != nil {
		return nil, err
	}

	// If still nil, return programmatic defaults
	if result == nil {
		span.AddEvent("using_programmatic_defaults")
		result = settings.DefaultSettings()
	}

	return result, nil
}

// cacheSettings stores settings in cache if available
func (s *SettingsServiceImpl) cacheSettings(ctx context.Context, span trace.Span, req *settings.GetSettingsRequest, result *settings.UserSettings) {
	if s.cache == nil || result == nil {
		return
	}

	cacheKey := s.getCacheKey(req.UserID)
	span.AddEvent("caching_settings")

	if err := s.cache.Set(ctx, cacheKey, result, settingsCacheTTL); err != nil {
		// Log but don't fail - cache errors are non-critical
		span.AddEvent("cache_set_failed", trace.WithAttributes(
			attribute.String("error", err.Error()),
		))
	}
}

// UpdateSettings updates user settings
func (s *SettingsServiceImpl) UpdateSettings(ctx context.Context, req *settings.UpdateSettingsRequest) (*settings.UserSettings, error) {
	if req == nil {
		err := fmt.Errorf("update request cannot be nil")
		return nil, err
	}

	// Validate the request
	if err := req.Validate(); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	userIDStr := defaultUserID
	if req.UserID != nil {
		userIDStr = *req.UserID
	}

	ctx, span := s.tracer.Start(ctx, "UpdateSettings",
		trace.WithAttributes(
			attribute.String("settings.user_id", userIDStr),
		),
	)
	defer span.End()

	span.AddEvent("fetching_current_settings")
	// Get current settings (or default)
	current, err := s.GetSettings(ctx, &settings.GetSettingsRequest{UserID: req.UserID})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to get current settings")
		return nil, fmt.Errorf("failed to get current settings: %w", err)
	}

	// Apply updates to current settings
	span.AddEvent("applying_updates")
	updated := s.applyUpdates(current, req)

	// Upsert the settings
	span.AddEvent("upserting_to_database")
	result, err := s.repo.Upsert(ctx, updated)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to upsert settings")
		return nil, fmt.Errorf("failed to save settings: %w", err)
	}

	// Invalidate cache
	if s.cache != nil {
		cacheKey := s.getCacheKey(req.UserID)
		span.AddEvent("invalidating_cache")
		if err := s.cache.Delete(ctx, cacheKey); err != nil {
			// Log but don't fail - cache errors are non-critical
			span.AddEvent("cache_delete_failed", trace.WithAttributes(
				attribute.String("error", err.Error()),
			))
		}
	}

	s.recordWriteMetric(ctx)
	span.SetAttributes(attribute.Int("settings.id", result.ID))
	span.SetStatus(codes.Ok, "")
	return result, nil
}

// ResetSettings resets user settings to defaults
func (s *SettingsServiceImpl) ResetSettings(ctx context.Context, userID *string) (*settings.UserSettings, error) {
	userIDStr := defaultUserID
	if userID != nil {
		userIDStr = *userID
	}

	ctx, span := s.tracer.Start(ctx, "ResetSettings",
		trace.WithAttributes(
			attribute.String("settings.user_id", userIDStr),
		),
	)
	defer span.End()

	span.AddEvent("creating_default_settings")
	// Create new settings with default values
	defaults := settings.DefaultSettings()
	defaults.UserID = userID

	span.AddEvent("upserting_to_database")
	// Upsert (replace existing with defaults)
	result, err := s.repo.Upsert(ctx, defaults)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to reset settings")
		return nil, fmt.Errorf("failed to reset settings: %w", err)
	}

	// Invalidate cache
	if s.cache != nil {
		span.AddEvent("invalidating_cache")
		cacheKey := s.getCacheKey(userID)
		if err := s.cache.Delete(ctx, cacheKey); err != nil {
			// Log but don't fail - cache errors are non-critical
			span.AddEvent("cache_delete_failed", trace.WithAttributes(
				attribute.String("error", err.Error()),
			))
		}
	}

	s.recordWriteMetric(ctx)
	span.SetAttributes(attribute.Int("settings.id", result.ID))
	span.SetStatus(codes.Ok, "")
	return result, nil
}

// Helper methods

func (s *SettingsServiceImpl) getCacheKey(userID *string) string {
	if userID == nil {
		return settingsCacheKeyPrefix + "default"
	}
	return settingsCacheKeyPrefix + *userID
}

//nolint:gocyclo // Field-by-field settings update with multiple conditionals
func (s *SettingsServiceImpl) applyUpdates(current *settings.UserSettings, req *settings.UpdateSettingsRequest) *settings.UserSettings {
	updated := *current // Copy current settings

	if req.BackgroundImageID != nil {
		updated.BackgroundImageID = req.BackgroundImageID
	}
	if req.BackgroundImageURL != nil {
		updated.BackgroundImageURL = req.BackgroundImageURL
	}
	if req.BackgroundStyle != nil {
		updated.BackgroundStyle = *req.BackgroundStyle
	}
	if req.BackgroundOpacity != nil {
		updated.BackgroundOpacity = *req.BackgroundOpacity
	}
	if req.FontFamily != nil {
		updated.FontFamily = *req.FontFamily
	}
	if req.TextTheme != nil {
		updated.TextTheme = *req.TextTheme
	}
	if req.ShowTags != nil {
		updated.ShowTags = *req.ShowTags
	}
	if req.ShowDimensions != nil {
		updated.ShowDimensions = *req.ShowDimensions
	}
	if req.ShowContentType != nil {
		updated.ShowContentType = *req.ShowContentType
	}
	if req.GridColumns != nil {
		updated.GridColumns = *req.GridColumns
	}

	updated.UpdatedAt = time.Now()
	return &updated
}

func (s *SettingsServiceImpl) recordReadMetric(ctx context.Context, source string) {
	s.settingsOps.Add(ctx, 1, metric.WithAttributes(
		attribute.String(obs.AttrSettingsOp, "read"),
		attribute.String(obs.AttrSettingsSource, source),
	))
}

func (s *SettingsServiceImpl) recordWriteMetric(ctx context.Context) {
	s.settingsOps.Add(ctx, 1, metric.WithAttributes(
		attribute.String(obs.AttrSettingsOp, "write"),
		attribute.String(obs.AttrSettingsSource, "database"),
	))
}

func (s *SettingsServiceImpl) recordCacheHit(ctx context.Context) {
	s.cacheLookups.Add(ctx, 1, metric.WithAttributes(
		attribute.String(obs.AttrCacheName, "settings"),
		attribute.String(obs.AttrCacheResult, "hit"),
	))
}

func (s *SettingsServiceImpl) recordCacheMiss(ctx context.Context) {
	s.cacheLookups.Add(ctx, 1, metric.WithAttributes(
		attribute.String(obs.AttrCacheName, "settings"),
		attribute.String(obs.AttrCacheResult, "miss"),
	))
}
