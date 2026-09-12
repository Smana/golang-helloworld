package config

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		expected *Config
	}{
		{
			name: "test environment with defaults",
			envVars: map[string]string{
				"GO_ENV": "test", // Test environment allows empty DB URL
			},
			expected: &Config{
				Environment: "test",
				Port:        "8080",
				Host:        "localhost",
				DatabaseURL: "",
				Storage: StorageConfig{
					Endpoint:        "localhost:9000",
					AccessKeyID:     "minioadmin",
					SecretAccessKey: "minioadmin",
					BucketName:      "images",
					UseSSL:          false,
					Region:          "us-east-1",
					MaxUploadSize:   10 * 1024 * 1024,
					AllowedTypes:    []string{"image/jpeg", "image/png", "image/gif", "image/webp"},
				},
				Logging: &LoggingConfig{
					Level:  "info",
					Format: "json",
					Output: "stdout",
				},
				Server: &ServerConfig{
					ReadTimeout:  10 * time.Second,
					WriteTimeout: 10 * time.Second,
					IdleTimeout:  30 * time.Second,
				},
			},
		},
		{
			name: "development with database",
			envVars: map[string]string{
				"GO_ENV":             "development",
				"PORT":               "3000",
				"HOST":               "0.0.0.0",
				"DATABASE_URL":       "postgres://test:test@localhost/testdb",
				"STORAGE_ENDPOINT":   "s3.amazonaws.com",
				"STORAGE_ACCESS_KEY": "access123",
				"STORAGE_SECRET_KEY": "secret123",
				"STORAGE_BUCKET":     "mybucket",
				"STORAGE_USE_SSL":    "true",
				"STORAGE_REGION":     "us-west-2",
				"MAX_UPLOAD_SIZE":    "50MB",
				"LOG_LEVEL":          "debug",
				"LOG_FORMAT":         "text",
				"READ_TIMEOUT":       "15s",
				"WRITE_TIMEOUT":      "15s",
				"SERVER_TIMEOUT":     "60s",
			},
			expected: &Config{
				Environment: "development",
				Port:        "3000",
				Host:        "0.0.0.0",
				DatabaseURL: "postgres://test:test@localhost/testdb",
				Storage: StorageConfig{
					Endpoint:        "s3.amazonaws.com",
					AccessKeyID:     "access123",
					SecretAccessKey: "secret123",
					BucketName:      "mybucket",
					UseSSL:          true,
					Region:          "us-west-2",
					MaxUploadSize:   50 * 1024 * 1024,
					AllowedTypes:    []string{"image/jpeg", "image/png", "image/gif", "image/webp"},
				},
				Logging: &LoggingConfig{
					Level:  "debug",
					Format: "text",
					Output: "stdout",
				},
				Server: &ServerConfig{
					ReadTimeout:  15 * time.Second,
					WriteTimeout: 15 * time.Second,
					IdleTimeout:  60 * time.Second,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for key, value := range tt.envVars {
				_ = os.Setenv(key, value)
			}
			defer func() {
				for key := range tt.envVars {
					_ = os.Unsetenv(key)
				}
			}()

			config, err := Load()
			require.NoError(t, err, "Load() should not return error")
			require.NotNil(t, config, "Config should not be nil")

			// Test basic configuration
			assert.Equal(t, tt.expected.Environment, config.Environment)
			assert.Equal(t, tt.expected.Port, config.Port)
			assert.Equal(t, tt.expected.Host, config.Host)
			assert.Equal(t, tt.expected.DatabaseURL, config.DatabaseURL)

			// Test storage configuration
			assert.Equal(t, tt.expected.Storage.Endpoint, config.Storage.Endpoint)
			assert.Equal(t, tt.expected.Storage.UseSSL, config.Storage.UseSSL)
			assert.Equal(t, tt.expected.Storage.MaxUploadSize, config.Storage.MaxUploadSize)
			assert.Equal(t, tt.expected.Storage.AllowedTypes, config.Storage.AllowedTypes)

			// Test logging configuration
			require.NotNil(t, config.Logging)
			assert.Equal(t, tt.expected.Logging.Level, config.Logging.Level)
			assert.Equal(t, tt.expected.Logging.Format, config.Logging.Format)

			// Test server configuration
			require.NotNil(t, config.Server)
			assert.Equal(t, tt.expected.Server.ReadTimeout, config.Server.ReadTimeout)
			assert.Equal(t, tt.expected.Server.WriteTimeout, config.Server.WriteTimeout)
			assert.Equal(t, tt.expected.Server.IdleTimeout, config.Server.IdleTimeout)
		})
	}
}

func TestLoadStorageProvider(t *testing.T) {
	tests := []struct {
		name          string
		envVars       map[string]string
		expectError   bool
		errorContains string
	}{
		{
			name: "gcs provider with bucket loads and validates",
			envVars: map[string]string{
				"GO_ENV":           "test",
				"STORAGE_PROVIDER": "gcs",
				"STORAGE_BUCKET":   "gallery_images-2",
			},
			expectError: false,
		},
		{
			name: "gcs provider rejects an invalid bucket name at startup",
			envVars: map[string]string{
				"GO_ENV":           "test",
				"STORAGE_PROVIDER": "gcs",
				"STORAGE_BUCKET":   "My Bucket",
			},
			expectError:   true,
			errorContains: "storage.bucket_name",
		},
		{
			name: "unknown provider fails validation",
			envVars: map[string]string{
				"GO_ENV":           "test",
				"STORAGE_PROVIDER": "azure",
			},
			expectError:   true,
			errorContains: "STORAGE_PROVIDER",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for key, value := range tt.envVars {
				_ = os.Setenv(key, value)
			}
			defer func() {
				for key := range tt.envVars {
					_ = os.Unsetenv(key)
				}
			}()

			config, err := Load()
			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, config)
			assert.Equal(t, tt.envVars["STORAGE_PROVIDER"], config.Storage.Provider)
		})
	}
}

func TestLoadDemoControlsEnabled(t *testing.T) {
	t.Setenv("GO_ENV", "test")
	for value, want := range map[string]bool{"": true, "true": true, "false": false} {
		t.Setenv("DEMO_CONTROLS_ENABLED", value)
		config, err := Load()
		require.NoError(t, err)
		assert.Equal(t, want, config.DemoControlsEnabled, "DEMO_CONTROLS_ENABLED=%q", value)
	}
}
