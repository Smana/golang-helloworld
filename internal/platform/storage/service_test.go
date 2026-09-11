package storage_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"image-gallery/internal/config"
	"image-gallery/internal/platform/storage"
	"image-gallery/internal/platform/storage/storetest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestService builds a Service backed by a fresh in-memory ObjectStore.
func newTestService(t *testing.T) *storage.Service {
	t.Helper()
	svc, err := storage.NewService(&config.StorageConfig{BucketName: "test-bucket"}, storetest.NewMemStore())
	require.NoError(t, err)
	return svc
}

func TestNewService(t *testing.T) {
	t.Run("valid config and store", func(t *testing.T) {
		svc, err := storage.NewService(&config.StorageConfig{BucketName: "test-bucket"}, storetest.NewMemStore())
		assert.NoError(t, err)
		assert.NotNil(t, svc)
	})

	t.Run("nil config", func(t *testing.T) {
		svc, err := storage.NewService(nil, storetest.NewMemStore())
		assert.Error(t, err)
		assert.Nil(t, svc)
	})

	t.Run("nil store", func(t *testing.T) {
		svc, err := storage.NewService(&config.StorageConfig{BucketName: "test-bucket"}, nil)
		assert.Error(t, err)
		assert.Nil(t, svc)
	})
}

func TestService_Store(t *testing.T) {
	service := newTestService(t)

	tests := []struct {
		name        string
		filename    string
		contentType string
		data        io.Reader
		size        int64
		expectError bool
	}{
		{
			name:        "empty filename",
			filename:    "",
			contentType: "image/jpeg",
			data:        strings.NewReader("test data"),
			size:        9,
			expectError: true,
		},
		{
			name:        "empty content type",
			filename:    "test.jpg",
			contentType: "",
			data:        strings.NewReader("test data"),
			size:        9,
			expectError: true,
		},
		{
			name:        "nil data",
			filename:    "test.jpg",
			contentType: "image/jpeg",
			data:        nil,
			size:        0,
			expectError: true,
		},
		{
			name:        "unsupported content type",
			filename:    "test.txt",
			contentType: "text/plain",
			data:        strings.NewReader("test data"),
			size:        9,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := service.Store(context.Background(), tt.filename, tt.contentType, tt.data, tt.size)

			if tt.expectError {
				assert.Error(t, err)
			} else if err != nil {
				t.Log("Store succeeded unexpectedly")
			}
		})
	}
}

func TestService_StoreAt(t *testing.T) {
	service := newTestService(t)
	ctx := context.Background()

	t.Run("empty key", func(t *testing.T) {
		err := service.StoreAt(ctx, "", "image/png", strings.NewReader("data"), 4)
		assert.Error(t, err)
	})

	t.Run("writes to the exact key", func(t *testing.T) {
		err := service.StoreAt(ctx, "thumbnails/42.png", "image/png", strings.NewReader("data"), 4)
		require.NoError(t, err)

		exists, err := service.Exists(ctx, "thumbnails/42.png")
		require.NoError(t, err)
		assert.True(t, exists)
	})
}

func TestService_Retrieve(t *testing.T) {
	service := newTestService(t)

	t.Run("empty path", func(t *testing.T) {
		_, err := service.Retrieve(context.Background(), "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "path cannot be empty")
	})
}

func TestService_Delete(t *testing.T) {
	service := newTestService(t)

	t.Run("empty path", func(t *testing.T) {
		err := service.Delete(context.Background(), "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "path cannot be empty")
	})
}

func TestService_Exists(t *testing.T) {
	service := newTestService(t)

	t.Run("empty path", func(t *testing.T) {
		_, err := service.Exists(context.Background(), "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "path cannot be empty")
	})
}

func TestService_GetFileInfo(t *testing.T) {
	service := newTestService(t)

	t.Run("empty path", func(t *testing.T) {
		_, err := service.GetFileInfo(context.Background(), "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "path cannot be empty")
	})
}

func TestService_Health(t *testing.T) {
	service := newTestService(t)
	assert.NoError(t, service.Health(context.Background()))
}

func TestService_Provider(t *testing.T) {
	service := newTestService(t)
	assert.Equal(t, "memory", service.Provider())
}

// Security validation tests
func TestService_SecurityValidations(t *testing.T) {
	service := newTestService(t)

	t.Run("path traversal attempts", func(t *testing.T) {
		tests := []string{
			"../../../etc/passwd",
			"..\\windows\\system32\\config\\sam",
			"file/../../../secret.txt",
			"normal_file/../hidden.txt",
		}

		for _, filename := range tests {
			_, err := service.Store(context.Background(), filename, "image/jpeg", strings.NewReader("fake jpeg data"), 14)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "security validation failed")
		}
	})

	t.Run("absolute path attempts", func(t *testing.T) {
		tests := []string{
			"/etc/passwd",
			"\\windows\\system32\\hosts",
			"/var/log/sensitive.log",
		}

		for _, filename := range tests {
			_, err := service.Store(context.Background(), filename, "image/jpeg", strings.NewReader("fake jpeg data"), 14)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "security validation failed")
		}
	})

	t.Run("hidden file attempts", func(t *testing.T) {
		tests := []string{
			".htaccess",
			".env",
			".git/config",
			"folder/.secret",
		}

		for _, filename := range tests {
			_, err := service.Store(context.Background(), filename, "image/jpeg", strings.NewReader("fake jpeg data"), 14)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "security validation failed")
		}
	})

	t.Run("suspicious patterns", func(t *testing.T) {
		tests := []string{
			"<script>alert('xss')</script>.jpg",
			"javascript:void(0).png",
			"data:image/jpeg;base64,xyz.jpg",
			"vbscript:msgbox.gif",
			"onload=alert().webp",
			"onerror=steal().jpeg",
		}

		for _, filename := range tests {
			_, err := service.Store(context.Background(), filename, "image/jpeg", strings.NewReader("fake jpeg data"), 14)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "security validation failed")
		}
	})

	t.Run("extension content type mismatch", func(t *testing.T) {
		tests := []struct {
			filename    string
			contentType string
		}{
			{"image.jpg", "image/png"},
			{"document.pdf", "image/jpeg"},
			{"script.js", "image/gif"},
			{"photo.png", "image/jpeg"},
		}

		for _, test := range tests {
			data := "fake data"
			_, err := service.Store(context.Background(), test.filename, test.contentType, strings.NewReader(data), int64(len(data)))
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "security validation failed")
		}
	})

	t.Run("filename too long", func(t *testing.T) {
		longFilename := strings.Repeat("a", 300) + ".jpg"
		data := "fake jpeg data"
		_, err := service.Store(context.Background(), longFilename, "image/jpeg", strings.NewReader(data), int64(len(data)))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "security validation failed")
	})

	t.Run("null bytes in filename", func(t *testing.T) {
		filename := "image\x00.jpg"
		data := "fake jpeg data"
		_, err := service.Store(context.Background(), filename, "image/jpeg", strings.NewReader(data), int64(len(data)))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "security validation failed")
	})
}

func TestService_MagicNumberValidation(t *testing.T) {
	service := newTestService(t)

	t.Run("invalid JPEG magic number", func(t *testing.T) {
		// Create data with wrong magic number
		fakeData := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09}
		fakeData = append(fakeData, make([]byte, 100)...)

		_, err := service.Store(context.Background(), "test.jpg", "image/jpeg", bytes.NewReader(fakeData), int64(len(fakeData)))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "file content does not match declared content type")
	})

	t.Run("file too small", func(t *testing.T) {
		smallData := []byte{0x01, 0x02}

		_, err := service.Store(context.Background(), "test.jpg", "image/jpeg", bytes.NewReader(smallData), int64(len(smallData)))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "file too small to validate")
	})
}
