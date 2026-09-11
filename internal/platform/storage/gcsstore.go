package storage

import (
	"context"
	"errors"
	"fmt"
	"io"

	gcs "cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

// GCSStore is the Cloud Storage backend. Credentials are Application Default
// Credentials: on GKE, the pod's Workload Identity. No HMAC or JSON keys.
type GCSStore struct {
	client *gcs.Client
	bucket *gcs.BucketHandle
}

// NewGCSStore opens bucket; opts exist for tests (emulator, JSON reads).
func NewGCSStore(ctx context.Context, bucket string, opts ...option.ClientOption) (*GCSStore, error) {
	if bucket == "" {
		return nil, errors.New("STORAGE_BUCKET is required for gcs")
	}
	c, err := gcs.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("gcs client: %w", err)
	}
	return &GCSStore{client: c, bucket: c.Bucket(bucket)}, nil
}

func (s *GCSStore) Provider() string { return ProviderGCS }

// Put streams r in ONE request with no buffer (ChunkSize 0): bounded memory at
// the cost of transport retries. Files are at most 10 MiB (plan ruling 8).
func (s *GCSStore) Put(ctx context.Context, key, contentType string, r io.Reader, _ int64, metadata map[string]string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	w := s.bucket.Object(key).NewWriter(ctx)
	w.ContentType = contentType
	w.Metadata = metadata
	w.ChunkSize = 0
	if _, err := io.Copy(w, r); err != nil {
		cancel() // abort: closing after a short read would commit a truncated object
		return fmt.Errorf("gcs put %s: %w", key, errors.Join(err, w.Close()))
	}
	return w.Close()
}

func (s *GCSStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	r, err := s.bucket.Object(key).NewReader(ctx)
	if errors.Is(err, gcs.ErrObjectNotExist) {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, key)
	}
	return r, err
}

func (s *GCSStore) Stat(ctx context.Context, key string) (ObjectInfo, error) {
	a, err := s.bucket.Object(key).Attrs(ctx)
	if errors.Is(err, gcs.ErrObjectNotExist) {
		return ObjectInfo{}, fmt.Errorf("%w: %s", ErrNotFound, key)
	}
	if err != nil {
		return ObjectInfo{}, err
	}
	return attrsToInfo(a), nil
}

func (s *GCSStore) Delete(ctx context.Context, key string) error {
	if err := s.bucket.Object(key).Delete(ctx); err != nil && !errors.Is(err, gcs.ErrObjectNotExist) {
		return err
	}
	return nil
}

func (s *GCSStore) List(ctx context.Context, prefix string, maxKeys int) ([]ObjectInfo, error) {
	if maxKeys <= 0 {
		maxKeys = 1000
	}
	out := make([]ObjectInfo, 0)
	it := s.bucket.Objects(ctx, &gcs.Query{Prefix: prefix})
	for len(out) < maxKeys {
		a, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("gcs list: %w", err)
		}
		out = append(out, attrsToInfo(a))
	}
	return out, nil
}

// Health lists one object: storage.objects.list is in the bucket-scoped
// objectAdmin grant; storage.buckets.get is not, so no bucket Attrs call here.
func (s *GCSStore) Health(ctx context.Context) error {
	_, err := s.List(ctx, "", 1)
	return err
}

// Close releases the client.
func (s *GCSStore) Close() error { return s.client.Close() }

func attrsToInfo(a *gcs.ObjectAttrs) ObjectInfo {
	return ObjectInfo{Key: a.Name, Size: a.Size, ContentType: a.ContentType, LastModified: a.Updated,
		ETag: a.Etag, UserMetadata: lowerKeys(a.Metadata)}
}
