package config

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
		errorCount  int
	}{
		{
			name: "valid development config",
			config: &Config{
				Environment: "development",
				Port:        "8080",
				Host:        "localhost",
				DatabaseURL: "postgres://user:pass@localhost:5432/testdb",
				Storage: StorageConfig{
					Endpoint:        "localhost:9000",
					AccessKeyID:     "minioadmin",
					SecretAccessKey: "minioadmin",
					BucketName:      "images",
					UseSSL:          false,
					Region:          "us-east-1",
					MaxUploadSize:   10 * 1024 * 1024,
					AllowedTypes:    []string{"image/jpeg", "image/png"},
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
			expectError: false,
		},
		{
			name: "test environment allows empty database URL",
			config: &Config{
				Environment: "test",
				Port:        "8080",
				DatabaseURL: "", // OK for test environment
				Storage: StorageConfig{
					Endpoint:   "localhost:9000",
					BucketName: "test-images",
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

			if tt.expectError {
				require.Error(t, err)

				// Check if it's ValidationErrors and count them
				if ve, ok := err.(ValidationErrors); ok {
					assert.Len(t, ve, tt.errorCount, "Expected %d validation errors, got %d", tt.errorCount, len(ve))
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidationError(t *testing.T) {
	err := &ValidationError{
		Field:   "test_field",
		Value:   "test_value",
		Message: "test message",
	}

	expected := "config validation failed for test_field: test message (value: test_value)"
	assert.Equal(t, expected, err.Error())
}

func TestBucketNameValidation(t *testing.T) {
	tests := []struct {
		name   string
		bucket string
		valid  bool
	}{
		{"valid lowercase", "my-bucket", true},
		{"valid with numbers", "bucket123", true},
		{"valid mixed", "my-bucket-123", true},
		{"too short", "ab", false},
		{"too long", strings.Repeat("a", 64), false},
		{"uppercase", "MyBucket", false},
		{"starts with hyphen", "-bucket", false},
		{"ends with hyphen", "bucket-", false},
		{"consecutive hyphens", "my--bucket", false},
		{"ip address format", "192.168.1.1", false},
		{"underscore", "my_bucket", false},
		{"empty", "", false},
		{"non-ascii rune whose low byte is a letter", "my-bšcket", false}, // byte('š') == 'a'
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidBucketName(tt.bucket)
			assert.Equal(t, tt.valid, result, "Bucket name '%s' validation failed", tt.bucket)
		})
	}
}

func TestLoadWithEnvironmentValidation(t *testing.T) {
	// Clean environment
	envVars := []string{
		"GO_ENV", "PORT", "HOST", "DATABASE_URL",
		"STORAGE_ENDPOINT", "STORAGE_ACCESS_KEY", "STORAGE_SECRET_KEY",
		"STORAGE_BUCKET", "STORAGE_USE_SSL", "STORAGE_REGION",
		"MAX_UPLOAD_SIZE", "ALLOWED_FILE_TYPES",
		"LOG_LEVEL", "LOG_FORMAT", "LOG_OUTPUT",
		"READ_TIMEOUT", "WRITE_TIMEOUT", "SERVER_TIMEOUT",
	}

	for _, env := range envVars {
		_ = os.Unsetenv(env)
	}
	defer func() {
		for _, env := range envVars {
			_ = os.Unsetenv(env)
		}
	}()

	// Set test environment for valid config
	_ = os.Setenv("GO_ENV", "test")
	config, err := Load()
	assert.NoError(t, err)
	assert.NotNil(t, config)
	assert.Equal(t, "test", config.Environment)
}
