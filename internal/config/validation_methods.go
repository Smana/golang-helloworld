// Package config provides configuration validation
// with Go 1.25 best practices and comprehensive error handling
package config

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Constants for repeated string literals
const (
	minioadminCredential = "minioadmin"
	storageProviderGCS   = "gcs"
)

// ValidationError represents a configuration validation error
type ValidationError struct {
	Field   string
	Value   interface{}
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("config validation failed for %s: %s (value: %v)", e.Field, e.Message, e.Value)
}

// ValidationErrors is a collection of validation errors
type ValidationErrors []ValidationError

func (ve ValidationErrors) Error() string {
	if len(ve) == 0 {
		return "no validation errors"
	}

	messages := make([]string, 0, len(ve))
	for _, err := range ve {
		messages = append(messages, err.Error())
	}

	return fmt.Sprintf("configuration validation failed: %s", strings.Join(messages, "; "))
}

// Has checks if ValidationErrors contains any errors
func (ve ValidationErrors) Has() bool {
	return len(ve) > 0
}

// Validate validates the entire configuration
func (c *Config) Validate() error {
	var validationErrors ValidationErrors

	// Validate basic server configuration
	if err := c.validateServer(); err != nil {
		validationErrors = append(validationErrors, err...)
	}

	// Validate database configuration
	if err := c.validateDatabase(); err != nil {
		validationErrors = append(validationErrors, err...)
	}

	// Validate storage configuration
	if err := c.validateStorage(); err != nil {
		validationErrors = append(validationErrors, err...)
	}

	// Validate logging configuration (if present)
	if c.Logging != nil {
		if err := c.validateLogging(); err != nil {
			validationErrors = append(validationErrors, err...)
		}
	}

	// Validate server timeouts (if present)
	if c.Server != nil {
		if err := c.validateServerTimeouts(); err != nil {
			validationErrors = append(validationErrors, err...)
		}
	}

	if validationErrors.Has() {
		return validationErrors
	}

	return nil
}

func (c *Config) validateServer() ValidationErrors {
	var errors ValidationErrors

	// Validate port
	if c.Port == "" {
		errors = append(errors, ValidationError{
			Field:   "port",
			Value:   c.Port,
			Message: "port cannot be empty",
		})
	} else {
		if port, err := strconv.Atoi(c.Port); err != nil {
			errors = append(errors, ValidationError{
				Field:   "port",
				Value:   c.Port,
				Message: "port must be a valid integer",
			})
		} else if port < 1 || port > 65535 {
			errors = append(errors, ValidationError{
				Field:   "port",
				Value:   c.Port,
				Message: "port must be between 1 and 65535",
			})
		}
	}

	// Validate environment
	if c.Environment != "" {
		validEnvs := []string{"development", "production", "test", "staging"}
		isValid := false
		for _, validEnv := range validEnvs {
			if c.Environment == validEnv {
				isValid = true
				break
			}
		}

		if !isValid {
			errors = append(errors, ValidationError{
				Field:   "environment",
				Value:   c.Environment,
				Message: "environment must be one of: development, production, test, staging",
			})
		}
	}

	return errors
}

func (c *Config) validateDatabase() ValidationErrors {
	var errors ValidationErrors

	// Database URL is required for non-test environments
	if c.Environment != "test" && c.DatabaseURL == "" {
		errors = append(errors, ValidationError{
			Field:   "database_url",
			Value:   c.DatabaseURL,
			Message: "database URL is required for non-test environments",
		})
		return errors
	}

	// Skip validation if empty (test environment)
	if c.DatabaseURL == "" {
		return errors
	}

	// Validate database URL format
	parsedURL, err := url.Parse(c.DatabaseURL)
	if err != nil {
		errors = append(errors, ValidationError{
			Field:   "database_url",
			Value:   c.DatabaseURL,
			Message: "database URL must be a valid URL",
		})
		return errors
	}

	// Check for required components
	if parsedURL.Scheme != "postgres" && parsedURL.Scheme != "postgresql" {
		errors = append(errors, ValidationError{
			Field:   "database_url",
			Value:   parsedURL.Scheme,
			Message: "database URL must use postgres or postgresql scheme",
		})
	}

	if parsedURL.Host == "" {
		errors = append(errors, ValidationError{
			Field:   "database_url",
			Value:   c.DatabaseURL,
			Message: "database URL must include host",
		})
	}

	if parsedURL.Path == "" || parsedURL.Path == "/" {
		errors = append(errors, ValidationError{
			Field:   "database_url",
			Value:   c.DatabaseURL,
			Message: "database URL must include database name",
		})
	}

	return errors
}

func (c *Config) validateStorage() ValidationErrors {
	var errors ValidationErrors

	// Validate provider
	switch c.Storage.Provider {
	case "", "s3", storageProviderGCS:
		// valid
	default:
		errors = append(errors, ValidationError{
			Field:   "storage.provider",
			Value:   c.Storage.Provider,
			Message: fmt.Sprintf("STORAGE_PROVIDER must be \"s3\" or \"gcs\" (got %q)", c.Storage.Provider),
		})
	}

	// Validate endpoint - not required for gcs, which uses Application Default Credentials
	if c.Storage.Provider != storageProviderGCS && c.Storage.Endpoint == "" {
		errors = append(errors, ValidationError{
			Field:   "storage.endpoint",
			Value:   c.Storage.Endpoint,
			Message: "storage endpoint cannot be empty",
		})
	}

	// Validate bucket name: presence is required for both providers, but the
	// S3 naming format (3-63 chars) does not apply to GCS bucket names.
	if c.Storage.BucketName == "" {
		errors = append(errors, ValidationError{
			Field:   "storage.bucket_name",
			Value:   c.Storage.BucketName,
			Message: "storage bucket name cannot be empty",
		})
	} else if c.Storage.Provider != storageProviderGCS && !isValidBucketName(c.Storage.BucketName) {
		errors = append(errors, ValidationError{
			Field:   "storage.bucket_name",
			Value:   c.Storage.BucketName,
			Message: "storage bucket name must be 3-63 characters, lowercase alphanumeric and hyphens only",
		})
	}

	// Validate access credentials for production environments
	// Allow empty credentials for EKS Pod Identity / IAM roles
	if c.Environment == "production" {
		// Only warn about minioadmin credentials, allow empty for IAM roles
		if c.Storage.AccessKeyID == minioadminCredential {
			errors = append(errors, ValidationError{
				Field:   "storage.access_key_id",
				Value:   c.Storage.AccessKeyID,
				Message: "minioadmin credentials should not be used in production (use IAM roles or proper AWS credentials)",
			})
		}

		if c.Storage.SecretAccessKey == minioadminCredential {
			errors = append(errors, ValidationError{
				Field:   "storage.secret_access_key",
				Value:   "[REDACTED]",
				Message: "minioadmin credentials should not be used in production (use IAM roles or proper AWS credentials)",
			})
		}
	}

	// Validate upload size (if configured)
	if c.Storage.MaxUploadSize > 0 {
		maxAllowed := int64(100 * 1024 * 1024) // 100MB
		if c.Storage.MaxUploadSize > maxAllowed {
			errors = append(errors, ValidationError{
				Field:   "storage.max_upload_size",
				Value:   c.Storage.MaxUploadSize,
				Message: fmt.Sprintf("max upload size cannot exceed %d bytes (100MB)", maxAllowed),
			})
		}
	}

	return errors
}

func (c *Config) validateLogging() ValidationErrors {
	var errors ValidationErrors

	// Validate log level
	validLevels := []string{"debug", "info", "warn", "error"}
	isValidLevel := false
	for _, level := range validLevels {
		if strings.EqualFold(c.Logging.Level, level) {
			isValidLevel = true
			break
		}
	}

	if !isValidLevel {
		errors = append(errors, ValidationError{
			Field:   "logging.level",
			Value:   c.Logging.Level,
			Message: "logging level must be one of: debug, info, warn, error",
		})
	}

	// Validate log format
	validFormats := []string{"json", "text"}
	isValidFormat := false
	for _, format := range validFormats {
		if strings.EqualFold(c.Logging.Format, format) {
			isValidFormat = true
			break
		}
	}

	if !isValidFormat {
		errors = append(errors, ValidationError{
			Field:   "logging.format",
			Value:   c.Logging.Format,
			Message: "logging format must be either 'json' or 'text'",
		})
	}

	return errors
}

func (c *Config) validateServerTimeouts() ValidationErrors {
	var errors ValidationErrors

	// Validate read timeout
	if c.Server.ReadTimeout <= 0 {
		errors = append(errors, ValidationError{
			Field:   "server.read_timeout",
			Value:   c.Server.ReadTimeout,
			Message: "read timeout must be greater than 0",
		})
	} else if c.Server.ReadTimeout > 5*time.Minute {
		errors = append(errors, ValidationError{
			Field:   "server.read_timeout",
			Value:   c.Server.ReadTimeout,
			Message: "read timeout should not exceed 5 minutes",
		})
	}

	// Validate write timeout
	if c.Server.WriteTimeout <= 0 {
		errors = append(errors, ValidationError{
			Field:   "server.write_timeout",
			Value:   c.Server.WriteTimeout,
			Message: "write timeout must be greater than 0",
		})
	} else if c.Server.WriteTimeout > 5*time.Minute {
		errors = append(errors, ValidationError{
			Field:   "server.write_timeout",
			Value:   c.Server.WriteTimeout,
			Message: "write timeout should not exceed 5 minutes",
		})
	}

	// Validate idle timeout
	if c.Server.IdleTimeout <= 0 {
		errors = append(errors, ValidationError{
			Field:   "server.idle_timeout",
			Value:   c.Server.IdleTimeout,
			Message: "idle timeout must be greater than 0",
		})
	}

	return errors
}

// isValidBucketName validates S3/MinIO bucket naming rules
func isValidBucketName(name string) bool {
	return isValidBucketLength(name) &&
		hasValidBucketBoundaries(name) &&
		hasValidBucketCharacters(name) &&
		!isIPAddressFormat(name)
}

// isValidBucketLength checks if bucket name length is within S3 limits
func isValidBucketLength(name string) bool {
	return len(name) >= 3 && len(name) <= 63
}

// hasValidBucketBoundaries checks if bucket name starts and ends with valid characters
func hasValidBucketBoundaries(name string) bool {
	if name == "" {
		return false
	}
	return isLowerAlphaNum(name[0]) && isLowerAlphaNum(name[len(name)-1])
}

// hasValidBucketCharacters validates all characters in bucket name
func hasValidBucketCharacters(name string) bool {
	for i, r := range name {
		if !isLowerAlphaNum(byte(r)) && r != '-' {
			return false
		}

		// No consecutive hyphens
		if i > 0 && r == '-' && name[i-1] == '-' {
			return false
		}
	}
	return true
}

// isIPAddressFormat checks if the bucket name looks like an IP address
func isIPAddressFormat(name string) bool {
	parts := strings.Split(name, ".")
	if len(parts) != 4 {
		return false
	}

	for _, part := range parts {
		if _, err := strconv.Atoi(part); err != nil {
			return false
		}
	}
	return true
}

func isLowerAlphaNum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

// MustValidate validates the configuration and panics on error
// Useful for startup scenarios where invalid config should crash the application
func (c *Config) MustValidate() {
	if err := c.Validate(); err != nil {
		panic(fmt.Sprintf("configuration validation failed: %v", err))
	}
}
