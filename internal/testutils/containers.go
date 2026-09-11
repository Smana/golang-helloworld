package testutils

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	minioClient "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/minio"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	redisModule "github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"

	"image-gallery/internal/config"
	"image-gallery/internal/platform/cache"
	"image-gallery/internal/platform/storage"
)

// TestContainers manages test containers for integration testing
type TestContainers struct {
	PostgresContainer testcontainers.Container
	MinioContainer    testcontainers.Container
	RedisContainer    testcontainers.Container
	DB                *sql.DB
	ObjectStore       storage.ObjectStore
	RedisClient       *cache.RedisClient
	DatabaseURL       string
	MinioEndpoint     string
	MinioUsername     string
	MinioPassword     string
	RedisEndpoint     string
}

// SetupTestContainers initializes and starts test containers
func SetupTestContainers(ctx context.Context) (*TestContainers, error) {
	containers := &TestContainers{
		MinioUsername: "testuser",
		MinioPassword: "testpass123",
	}

	// Setup PostgreSQL container
	if err := containers.setupPostgres(ctx); err != nil {
		return nil, fmt.Errorf("failed to setup postgres container: %w", err)
	}

	// Setup MinIO container
	if err := containers.setupMinio(ctx); err != nil {
		_ = containers.Cleanup(ctx) //nolint:errcheck // Test cleanup on error
		return nil, fmt.Errorf("failed to setup minio container: %w", err)
	}

	// Setup Redis container
	if err := containers.setupRedis(ctx); err != nil {
		_ = containers.Cleanup(ctx) //nolint:errcheck // Test cleanup on error
		return nil, fmt.Errorf("failed to setup redis container: %w", err)
	}

	// Apply migrations using Atlas CLI
	if err := containers.applyMigrations(); err != nil {
		_ = containers.Cleanup(ctx) //nolint:errcheck // Test cleanup on error
		return nil, fmt.Errorf("failed to apply migrations with Atlas: %w", err)
	}

	return containers, nil
}

// applyMigrations applies schema migrations using Atlas CLI
func (tc *TestContainers) applyMigrations() error {
	// Set environment variable for Atlas test environment
	originalEnv := os.Getenv("TEST_DATABASE_URL")
	defer func() {
		if originalEnv == "" {
			_ = os.Unsetenv("TEST_DATABASE_URL") //nolint:errcheck // Test cleanup
		} else {
			_ = os.Setenv("TEST_DATABASE_URL", originalEnv) //nolint:errcheck // Test cleanup
		}
	}()

	// Set the test database URL for Atlas
	_ = os.Setenv("TEST_DATABASE_URL", tc.DatabaseURL) //nolint:errcheck // Test setup

	// Find project root directory dynamically
	_, filename, _, _ := runtime.Caller(0)
	projectRoot := filepath.Join(filepath.Dir(filename), "..", "..")
	atlasConfig := "file://" + filepath.Join(projectRoot, "atlas.hcl")

	// Use Atlas CLI to apply migrations with fixed arguments
	args := []string{"migrate", "apply", "--env", "test", "--config", atlasConfig}
	cmd := exec.Command("atlas", args...) //nolint:gosec // Atlas CLI with controlled arguments
	cmd.Dir = projectRoot

	// Capture output for debugging
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("atlas migrate apply failed: %w\nOutput: %s", err, output)
	}

	return nil
}

// setupPostgres creates and starts a PostgreSQL test container
func (tc *TestContainers) setupPostgres(ctx context.Context) error {
	postgresContainer, err := postgres.Run(ctx,
		"postgres:15-alpine",
		postgres.WithDatabase("testdb"),
		postgres.WithUsername("testuser"),
		postgres.WithPassword("testpass"),
		postgres.WithSQLDriver("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		return fmt.Errorf("failed to start postgres container: %w", err)
	}

	tc.PostgresContainer = postgresContainer

	// Get connection string
	connStr, err := postgresContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return fmt.Errorf("failed to get postgres connection string: %w", err)
	}

	tc.DatabaseURL = connStr

	// Connect to database
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return fmt.Errorf("failed to connect to postgres: %w", err)
	}

	// Test connection with retry
	for i := 0; i < 10; i++ {
		if err := db.Ping(); err != nil {
			if i == 9 {
				return fmt.Errorf("failed to ping postgres after retries: %w", err)
			}
			time.Sleep(time.Second)
			continue
		}
		break
	}

	tc.DB = db
	return nil
}

// setupMinio creates and starts a MinIO test container
func (tc *TestContainers) setupMinio(ctx context.Context) error {
	minioContainer, err := minio.Run(ctx,
		"minio/minio:latest",
		minio.WithUsername(tc.MinioUsername),
		minio.WithPassword(tc.MinioPassword),
	)
	if err != nil {
		return fmt.Errorf("failed to start minio container: %w", err)
	}

	tc.MinioContainer = minioContainer

	// Get connection details
	endpoint, err := minioContainer.ConnectionString(ctx)
	if err != nil {
		return fmt.Errorf("failed to get minio endpoint: %w", err)
	}

	tc.MinioEndpoint = endpoint

	// Create MinIO client
	minioClientInstance, err := minioClient.New(endpoint, &minioClient.Options{
		Creds:  credentials.NewStaticV4(tc.MinioUsername, tc.MinioPassword, ""),
		Secure: false,
	})
	if err != nil {
		return fmt.Errorf("failed to create minio client: %w", err)
	}

	// Wrap in our storage client using config
	storageConfig := config.StorageConfig{
		Endpoint:        endpoint,
		AccessKeyID:     tc.MinioUsername,
		SecretAccessKey: tc.MinioPassword,
		UseSSL:          false,
		BucketName:      "test-images",
		Region:          "us-east-1",
		Provider:        storage.ProviderS3,
	}

	objectStore, err := storage.NewS3Store(ctx, storageConfig)
	if err != nil {
		return fmt.Errorf("failed to create storage client: %w", err)
	}

	tc.ObjectStore = objectStore

	// Create test bucket
	bucketName := "test-images"
	exists, err := minioClientInstance.BucketExists(ctx, bucketName)
	if err != nil {
		return fmt.Errorf("failed to check if bucket exists: %w", err)
	}

	if !exists {
		err = minioClientInstance.MakeBucket(ctx, bucketName, minioClient.MakeBucketOptions{})
		if err != nil {
			return fmt.Errorf("failed to create test bucket: %w", err)
		}
	}

	return nil
}

// setupRedis creates and starts a Valkey test container (Redis-compatible)
func (tc *TestContainers) setupRedis(ctx context.Context) error {
	redisContainer, err := redisModule.Run(ctx,
		"valkey/valkey:7-alpine",
		redisModule.WithSnapshotting(10, 1),
		redisModule.WithLogLevel(redisModule.LogLevelVerbose),
	)
	if err != nil {
		return fmt.Errorf("failed to start valkey container: %w", err)
	}

	tc.RedisContainer = redisContainer

	// Get connection string and extract host:port
	endpoint, err := redisContainer.ConnectionString(ctx)
	if err != nil {
		return fmt.Errorf("failed to get valkey endpoint: %w", err)
	}

	// Remove redis:// prefix if present since go-redis expects just host:port
	address := endpoint
	if strings.HasPrefix(endpoint, "redis://") {
		address = strings.TrimPrefix(endpoint, "redis://")
	}

	tc.RedisEndpoint = address

	// Create Redis client using our cache implementation (Valkey is Redis-compatible)
	cacheConfig := config.CacheConfig{
		Enabled:     true,
		Address:     address,
		Password:    "",
		Database:    0,
		DefaultTTL:  1 * time.Hour,
		DialTimeout: 5 * time.Second,
	}

	redisClient, err := cache.NewRedisClient(cacheConfig)
	if err != nil {
		return fmt.Errorf("failed to create redis client: %w", err)
	}

	tc.RedisClient = redisClient

	// Test connection
	if err := tc.RedisClient.Health(ctx); err != nil {
		return fmt.Errorf("failed to connect to valkey: %w", err)
	}

	return nil
}

// Cleanup terminates all test containers and closes connections
func (tc *TestContainers) Cleanup(ctx context.Context) error {
	var errs []error

	// Collect cleanup errors from all resources
	tc.collectCleanupErrors(&errs)
	tc.collectContainerTerminationErrors(ctx, &errs)

	return tc.formatCleanupErrors(errs)
}

// collectCleanupErrors collects errors from closing client connections
func (tc *TestContainers) collectCleanupErrors(errs *[]error) {
	if err := tc.cleanupDatabase(); err != nil {
		*errs = append(*errs, err)
	}
	if err := tc.cleanupRedisClient(); err != nil {
		*errs = append(*errs, err)
	}
}

// collectContainerTerminationErrors collects errors from terminating containers
func (tc *TestContainers) collectContainerTerminationErrors(ctx context.Context, errs *[]error) {
	if err := tc.terminatePostgresContainer(ctx); err != nil {
		*errs = append(*errs, err)
	}
	if err := tc.terminateMinioContainer(ctx); err != nil {
		*errs = append(*errs, err)
	}
	if err := tc.terminateRedisContainer(ctx); err != nil {
		*errs = append(*errs, err)
	}
}

// cleanupDatabase closes the database connection
func (tc *TestContainers) cleanupDatabase() error {
	if tc.DB != nil {
		if err := tc.DB.Close(); err != nil {
			return fmt.Errorf("failed to close database: %w", err)
		}
	}
	return nil
}

// cleanupRedisClient closes the Redis client connection
func (tc *TestContainers) cleanupRedisClient() error {
	if tc.RedisClient != nil {
		if err := tc.RedisClient.Close(); err != nil {
			return fmt.Errorf("failed to close valkey client: %w", err)
		}
	}
	return nil
}

// terminatePostgresContainer terminates the PostgreSQL container
func (tc *TestContainers) terminatePostgresContainer(ctx context.Context) error {
	if tc.PostgresContainer != nil {
		if err := tc.PostgresContainer.Terminate(ctx); err != nil {
			return fmt.Errorf("failed to terminate postgres container: %w", err)
		}
	}
	return nil
}

// terminateMinioContainer terminates the MinIO container
func (tc *TestContainers) terminateMinioContainer(ctx context.Context) error {
	if tc.MinioContainer != nil {
		if err := tc.MinioContainer.Terminate(ctx); err != nil {
			return fmt.Errorf("failed to terminate minio container: %w", err)
		}
	}
	return nil
}

// terminateRedisContainer terminates the Redis container
func (tc *TestContainers) terminateRedisContainer(ctx context.Context) error {
	if tc.RedisContainer != nil {
		if err := tc.RedisContainer.Terminate(ctx); err != nil {
			return fmt.Errorf("failed to terminate valkey container: %w", err)
		}
	}
	return nil
}

// formatCleanupErrors formats cleanup errors into a single error
func (tc *TestContainers) formatCleanupErrors(errs []error) error {
	if len(errs) > 0 {
		return fmt.Errorf("cleanup errors: %v", errs)
	}
	return nil
}

// GetDatabaseURL returns the database connection URL
func (tc *TestContainers) GetDatabaseURL() string {
	return tc.DatabaseURL
}

// GetMinioEndpoint returns the MinIO endpoint
func (tc *TestContainers) GetMinioEndpoint() string {
	return tc.MinioEndpoint
}

// GetMinioCredentials returns MinIO credentials
func (tc *TestContainers) GetMinioCredentials() (username, password string) {
	return tc.MinioUsername, tc.MinioPassword
}

// ResetDatabase clears all data from the test database
func (tc *TestContainers) ResetDatabase(ctx context.Context) error {
	// Clean up all tables in reverse dependency order
	tables := []string{
		"image_albums",
		"image_tags",
		"albums",
		"tags",
		"images",
		"schema_migrations",
	}

	// Execute TRUNCATE commands outside of a transaction to avoid failed transaction state
	// when tables don't exist. Each TRUNCATE is independent.
	for _, table := range tables {
		// Use TRUNCATE CASCADE which is faster and handles foreign keys automatically
		// Ignore errors for tables that might not exist yet
		_, _ = tc.DB.ExecContext(ctx, fmt.Sprintf("TRUNCATE TABLE %s CASCADE", table)) //nolint:errcheck // Ignore non-existent tables
	}

	// Re-apply migrations to ensure schema is fresh
	return tc.applyMigrations()
}

// CreateTestBucket creates a bucket for testing
func (tc *TestContainers) CreateTestBucket(ctx context.Context, bucketName string) error {
	// This would use the internal MinIO client from our storage client
	// For now, we'll assume the bucket creation is handled by the storage client
	return nil
}

// CleanBucket removes all objects from a test bucket
func (tc *TestContainers) CleanBucket(ctx context.Context, bucketName string) error {
	// This would clean all objects from the specified bucket
	// Implementation depends on the storage client capabilities
	return nil
}

// GetRedisEndpoint returns the Valkey endpoint (Redis-compatible)
func (tc *TestContainers) GetRedisEndpoint() string {
	return tc.RedisEndpoint
}

// FlushRedis clears all data from the Valkey test database
func (tc *TestContainers) FlushRedis(ctx context.Context) error {
	if tc.RedisClient == nil {
		return fmt.Errorf("valkey client not available")
	}

	return tc.RedisClient.FlushCache(ctx)
}

// GetRedisClient returns the Valkey client for tests (Redis-compatible)
func (tc *TestContainers) GetRedisClient() *cache.RedisClient {
	return tc.RedisClient
}
