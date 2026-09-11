package storage

import (
	"bytes"
	"crypto/sha256"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Test constants for repeated string literals
const testContent = "hello world"

func TestService_generateStoragePath(t *testing.T) {
	service := &Service{}

	tests := []struct {
		name     string
		filename string
	}{
		{
			name:     "simple filename",
			filename: "test.jpg",
		},
		{
			name:     "filename with spaces",
			filename: "my photo.jpg",
		},
		{
			name:     "complex filename",
			filename: "My Complex File Name (1).jpeg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := service.generateStoragePath(tt.filename)

			// Should not be empty
			assert.NotEmpty(t, path)

			// Should contain directory structure (xx/yy/)
			parts := strings.Split(path, "/")
			assert.GreaterOrEqual(t, len(parts), 3, "Should have at least 3 parts: dir1/dir2/filename")

			// Should preserve extension
			originalExt := strings.ToLower(filepath.Ext(tt.filename))
			if originalExt != "" {
				assert.True(t, strings.HasSuffix(path, originalExt))
			}
		})
	}

	// Test uniqueness
	t.Run("generates unique paths", func(t *testing.T) {
		filename := "test.jpg"
		path1 := service.generateStoragePath(filename)
		time.Sleep(1 * time.Millisecond) // Ensure different timestamp
		path2 := service.generateStoragePath(filename)

		assert.NotEqual(t, path1, path2, "Should generate unique paths for same filename")
	})
}

func TestService_sanitizeFilename(t *testing.T) {
	service := &Service{}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple filename",
			input:    "test",
			expected: "test",
		},
		{
			name:     "filename with spaces",
			input:    "my photo",
			expected: "my_photo",
		},
		{
			name:     "filename with problematic characters",
			input:    "my/photo\\test..file",
			expected: "my_photo_test_file",
		},
		{
			name:     "long filename",
			input:    strings.Repeat("a", 150),
			expected: strings.Repeat("a", 100),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := service.sanitizeFilename(tt.input)
			assert.Equal(t, tt.expected, result)
			assert.LessOrEqual(t, len(result), 100, "Sanitized filename should not exceed 100 characters")
		})
	}
}

func TestService_isValidContentType(t *testing.T) {
	service := &Service{}

	tests := []struct {
		name        string
		contentType string
		expected    bool
	}{
		{"valid jpeg", "image/jpeg", true},
		{"valid jpg", "image/jpg", true},
		{"valid png", "image/png", true},
		{"valid gif", "image/gif", true},
		{"valid webp", "image/webp", true},
		{"invalid pdf", "application/pdf", false},
		{"invalid text", "text/plain", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := service.isValidContentType(tt.contentType)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestService_getMaxFileSize(t *testing.T) {
	service := &Service{config: nil}
	assert.Equal(t, int64(10*1024*1024), service.getMaxFileSize())
}

func TestSizeCountingReader(t *testing.T) {
	data := testContent
	reader := &sizeCountingReader{
		reader: strings.NewReader(data),
		size:   0,
	}

	buffer := make([]byte, 5)
	n, err := reader.Read(buffer)

	assert.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, int64(5), reader.size)
	assert.Equal(t, "hello", string(buffer))

	// Read remaining data
	remainingBuffer := make([]byte, 10)
	n, err = reader.Read(remainingBuffer)

	assert.NoError(t, err)
	assert.Equal(t, 6, n)                   // " world" is 6 characters
	assert.Equal(t, int64(11), reader.size) // Total size should now be 11
	assert.Equal(t, " world", string(remainingBuffer[:n]))
}

func TestSizeCountingReader_ExceedsMax(t *testing.T) {
	data := make([]byte, 1000)
	reader := &sizeCountingReader{
		reader:  bytes.NewReader(data),
		maxSize: 500, // Set limit to 500 bytes
	}

	buffer := make([]byte, 600) // Try to read more than limit
	_, err := reader.Read(buffer)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "file size exceeds maximum allowed size")
}

func TestHashingReader(t *testing.T) {
	data := testContent
	reader := &hashingReader{
		reader: strings.NewReader(data),
		hasher: sha256.New(),
	}

	buffer := make([]byte, len(data))
	n, err := reader.Read(buffer)

	if err != nil && err != io.EOF {
		t.Errorf("Unexpected error: %v", err)
	}
	assert.Equal(t, len(data), n)
	assert.Equal(t, data, string(buffer))
}

func TestService_validateMagicNumber(t *testing.T) {
	service := &Service{}

	tests := []struct {
		name        string
		data        []byte
		contentType string
		expectError bool
	}{
		{
			name:        "valid JPEG",
			data:        []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46},
			contentType: "image/jpeg",
			expectError: false,
		},
		{
			name:        "valid PNG",
			data:        []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A},
			contentType: "image/png",
			expectError: false,
		},
		{
			name:        "invalid JPEG",
			data:        []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07},
			contentType: "image/jpeg",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := service.validateMagicNumber(tt.data, tt.contentType)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
