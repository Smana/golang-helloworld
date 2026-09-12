package services

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	"image-gallery/internal/config"
	"image-gallery/internal/domain/image"
	"image-gallery/internal/domain/settings"
	"image-gallery/internal/faults"
	"image-gallery/internal/observability"
	"image-gallery/internal/platform/cache"
	"image-gallery/internal/platform/database"
	"image-gallery/internal/platform/storage"
	"image-gallery/internal/services/implementations"
)

// TestConfig provides test-specific configuration for integration testing
type TestConfig struct {
	DatabaseURL string
}

// Container holds all the application dependencies
type Container struct {
	config *config.Config
	db     *sql.DB

	// Storage
	objectStore    storage.ObjectStore
	storageService image.StorageService

	// Repositories
	imageRepository    image.Repository
	tagRepository      image.TagRepository
	settingsRepository settings.Repository

	// Services
	imageService      image.ImageService
	tagService        image.TagService
	settingsService   settings.SettingsService
	imageProcessor    image.ImageProcessor
	validationService image.ValidationService

	// Infrastructure services (optional - can be nil for now)
	eventPublisher      image.EventPublisher
	cacheService        image.CacheService
	redisClient         *cache.RedisClient // Direct access to Redis for generic caching
	searchService       image.SearchService
	auditService        image.AuditService
	notificationService image.NotificationService

	// Demo controls (fault injection)
	demoService  *faults.Service
	demoInjector *faults.Injector

	// Observability
	logger *observability.Logger
}

// NewContainer creates a new dependency injection container
func NewContainer(cfg *config.Config, db *sql.DB, store storage.ObjectStore) (*Container, error) {
	container := &Container{
		config:      cfg,
		db:          db,
		objectStore: store,
		logger:      nil, // No logger in legacy constructor
	}

	if err := container.initializeServices(); err != nil {
		return nil, err
	}

	return container, nil
}

// NewContainerWithObservability creates a new dependency injection container with observability support
func NewContainerWithObservability(cfg *config.Config, db *sql.DB, store storage.ObjectStore, logger *observability.Logger) (*Container, error) {
	container := &Container{
		config:      cfg,
		db:          db,
		objectStore: store,
		logger:      logger,
	}

	if err := container.initializeServices(); err != nil {
		return nil, err
	}

	return container, nil
}

// NewContainerForTest creates a new dependency injection container for testing
func NewContainerForTest(testCfg *TestConfig, db *sql.DB, store storage.ObjectStore) (*Container, error) {
	// Create a minimal config for testing
	cfg := &config.Config{
		Environment: "test",
		DatabaseURL: testCfg.DatabaseURL,
		Storage: config.StorageConfig{
			BucketName: "test-images",
		},
	}

	return NewContainer(cfg, db, store)
}

// initializeServices initializes all services in the correct dependency order
func (c *Container) initializeServices() error {
	// Initialize database repositories first
	dbImageRepo := database.NewImageRepository(c.db)
	dbTagRepo := database.NewTagRepository(c.db)

	// Initialize domain repository adapters
	imageRepoAdapter := implementations.NewImageRepositoryAdapter(dbImageRepo)
	// Set tag repository on image adapter so it can save tag associations
	if adapter, ok := imageRepoAdapter.(interface{ SetTagRepository(database.TagRepository) }); ok {
		adapter.SetTagRepository(dbTagRepo)
	}
	c.imageRepository = imageRepoAdapter
	c.tagRepository = implementations.NewTagRepository(c.db)
	c.settingsRepository = implementations.NewSettingsRepository(c.db)

	// Initialize infrastructure services
	svc, err := storage.NewService(&c.config.Storage, c.objectStore)
	if err != nil {
		return fmt.Errorf("storage service: %w", err)
	}
	c.storageService = implementations.NewStorageService(svc)
	c.imageProcessor = implementations.NewImageProcessor()
	c.validationService = implementations.NewValidationService()

	// Initialize cache service (optional)
	if c.config.Cache.Enabled {
		if redisClient, err := cache.NewRedisClient(c.config.Cache); err == nil {
			c.redisClient = redisClient // Store Redis client for generic caching
			c.cacheService = implementations.NewCacheService(redisClient)
			log.Println("Cache service initialized with Valkey/Redis")
		} else {
			log.Printf("Failed to initialize cache service: %v", err)
			c.redisClient = nil
			c.cacheService = nil
		}
	} else {
		log.Println("Cache service disabled")
		c.redisClient = nil
		c.cacheService = nil
	}
	// The worker writes image status directly through imageRepoAdapter,
	// bypassing ImageService's own cache invalidation; wire it here too so a
	// list or single image cached mid-processing does not stay stale until
	// its TTL expires.
	if c.cacheService != nil {
		if adapter, ok := imageRepoAdapter.(interface {
			SetCacheInvalidator(implementations.CacheInvalidator)
		}); ok {
			adapter.SetCacheInvalidator(c.cacheService)
		}
	}

	// Initialize optional services (can be nil for now)
	c.eventPublisher = nil      // Will implement later
	c.searchService = nil       // Will implement later
	c.auditService = nil        // Will implement later
	c.notificationService = nil // Will implement later

	// Initialize domain services
	c.imageService = implementations.NewImageService(
		c.imageRepository,
		c.tagRepository,
		c.storageService,
		c.imageProcessor,
		c.validationService,
		c.eventPublisher,
		c.cacheService,
	)

	c.tagService = implementations.NewTagService(
		c.tagRepository,
		c.validationService,
		c.eventPublisher,
	)

	c.settingsService = implementations.NewSettingsService(
		c.settingsRepository,
		c.redisClient,
	)

	var demoCache faults.Cache // a nil *RedisClient inside a non-nil interface would panic
	if c.redisClient != nil {
		demoCache = c.redisClient
	}
	c.demoService = faults.NewService(implementations.NewDemoRepository(c.db), demoCache)
	c.demoInjector = faults.NewInjector(c.demoService, c.logger)
	if s, ok := c.imageService.(interface{ SetSlowDB(func(context.Context)) }); ok {
		s.SetSlowDB(func(ctx context.Context) {
			if d := c.demoInjector.SlowDB(ctx); d > 0 {
				_ = implementations.SleepInDB(ctx, c.db, d.Seconds()) //nolint:errcheck // best-effort demo delay; a failure here must not break the list query
			}
		})
	}

	log.Println("Dependency injection container initialized successfully")
	return nil
}

// Getters for accessing services

func (c *Container) Config() *config.Config {
	return c.config
}

func (c *Container) DB() *sql.DB {
	return c.db
}

func (c *Container) ObjectStore() storage.ObjectStore {
	return c.objectStore
}

func (c *Container) StorageService() image.StorageService {
	return c.storageService
}

func (c *Container) ImageRepository() image.Repository {
	return c.imageRepository
}

func (c *Container) TagRepository() image.TagRepository {
	return c.tagRepository
}

func (c *Container) ImageService() image.ImageService {
	return c.imageService
}

func (c *Container) TagService() image.TagService {
	return c.tagService
}

func (c *Container) SettingsRepository() settings.Repository {
	return c.settingsRepository
}

func (c *Container) SettingsService() settings.SettingsService {
	return c.settingsService
}

func (c *Container) ImageProcessor() image.ImageProcessor {
	return c.imageProcessor
}

func (c *Container) ValidationService() image.ValidationService {
	return c.validationService
}

func (c *Container) EventPublisher() image.EventPublisher {
	return c.eventPublisher
}

func (c *Container) CacheService() image.CacheService {
	return c.cacheService
}

func (c *Container) SearchService() image.SearchService {
	return c.searchService
}

func (c *Container) AuditService() image.AuditService {
	return c.auditService
}

func (c *Container) NotificationService() image.NotificationService {
	return c.notificationService
}

// UseJobPublisher wires the asynchronous processing queue into the image service.
func (c *Container) UseJobPublisher(p image.JobPublisher) {
	if s, ok := c.imageService.(interface{ SetJobPublisher(image.JobPublisher) }); ok {
		s.SetJobPublisher(p)
	}
}

func (c *Container) Logger() *observability.Logger {
	return c.logger
}

// DemoService returns the demo-controls service (fault injection settings).
func (c *Container) DemoService() *faults.Service {
	return c.demoService
}

// DemoInjector returns the demo-controls fault injector.
func (c *Container) DemoInjector() *faults.Injector {
	return c.demoInjector
}

// Close cleans up resources
func (c *Container) Close() error {
	if c.db != nil {
		return c.db.Close()
	}
	return nil
}
