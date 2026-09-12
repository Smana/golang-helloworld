package services

import (
	"database/sql"
	"fmt"
	"log"

	"image-gallery/internal/config"
	"image-gallery/internal/domain/image"
	"image-gallery/internal/domain/settings"
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

// Close cleans up resources
func (c *Container) Close() error {
	if c.db != nil {
		return c.db.Close()
	}
	return nil
}
