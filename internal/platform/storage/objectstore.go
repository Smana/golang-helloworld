package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"image-gallery/internal/config"
)

// Provider names, also the storage.provider telemetry attribute.
const (
	ProviderS3  = "s3"
	ProviderGCS = "gcs"
)

// ErrNotFound is returned by Get and Stat for a missing key.
var ErrNotFound = errors.New("object not found")

// ObjectStore is the minimal surface both backends implement. Instrumentation
// lives one layer up (implementations.StorageServiceImpl) so S3 and GCS emit
// identical storage.* telemetry, distinguished only by storage.provider.
type ObjectStore interface {
	Provider() string
	Put(ctx context.Context, key, contentType string, r io.Reader, size int64, metadata map[string]string) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Stat(ctx context.Context, key string) (ObjectInfo, error)
	Delete(ctx context.Context, key string) error
	List(ctx context.Context, prefix string, max int) ([]ObjectInfo, error)
	Health(ctx context.Context) error
}

// NewObjectStore picks the backend from STORAGE_PROVIDER.
func NewObjectStore(ctx context.Context, cfg config.StorageConfig) (ObjectStore, error) {
	switch cfg.Provider {
	case "", ProviderS3:
		return NewS3Store(ctx, cfg)
	case ProviderGCS:
		return NewGCSStore(ctx, cfg.BucketName)
	default:
		return nil, fmt.Errorf("unknown STORAGE_PROVIDER %q (want %s or %s)", cfg.Provider, ProviderS3, ProviderGCS)
	}
}

// lowerKeys normalises user-metadata keys: S3 canonicalises them
// (Original-Filename), GCS keeps them verbatim.
func lowerKeys(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[strings.ToLower(k)] = v
	}
	return out
}
