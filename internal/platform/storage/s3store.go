package storage

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"image-gallery/internal/config"
)

// S3Store is the S3 backend (AWS S3, or MinIO locally).
type S3Store struct {
	client *minio.Client
	bucket string
}

// NewS3Store connects with static keys when both are set (local MinIO), else
// the IAM chain: EKS Pod Identity on aws-0, unchanged from v1.
func NewS3Store(ctx context.Context, cfg config.StorageConfig) (*S3Store, error) {
	creds := credentials.NewIAM("")
	if cfg.AccessKeyID != "" || cfg.SecretAccessKey != "" {
		creds = credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, "")
	}
	client, err := minio.New(cfg.Endpoint, &minio.Options{Creds: creds, Secure: cfg.UseSSL, Region: cfg.Region})
	if err != nil {
		return nil, fmt.Errorf("s3 client: %w", err)
	}
	s := &S3Store{client: client, bucket: cfg.BucketName}
	exists, err := client.BucketExists(ctx, s.bucket)
	if err != nil {
		return nil, fmt.Errorf("s3 bucket %s: %w", s.bucket, err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{Region: cfg.Region}); err != nil {
			return nil, fmt.Errorf("s3 make bucket %s: %w", s.bucket, err)
		}
	}
	return s, nil
}

func (s *S3Store) Provider() string { return ProviderS3 }

func (s *S3Store) Put(ctx context.Context, key, contentType string, r io.Reader, size int64, metadata map[string]string) error {
	// A known size lets minio-go stream instead of buffering a multipart part.
	_, err := s.client.PutObject(ctx, s.bucket, key, r, size, minio.PutObjectOptions{ContentType: contentType, UserMetadata: metadata})
	return err
}

func (s *S3Store) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, mapS3Err(err)
	}
	if _, statErr := obj.Stat(); statErr != nil { // GetObject is lazy; surface a missing key now
		return nil, errors.Join(mapS3Err(statErr), obj.Close())
	}
	return obj, nil
}

func (s *S3Store) Stat(ctx context.Context, key string) (ObjectInfo, error) {
	st, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return ObjectInfo{}, mapS3Err(err)
	}
	return ObjectInfo{Key: key, Size: st.Size, ContentType: st.ContentType, LastModified: st.LastModified,
		ETag: st.ETag, UserMetadata: lowerKeys(st.UserMetadata)}, nil
}

func (s *S3Store) Delete(ctx context.Context, key string) error {
	return s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}) // S3 delete is idempotent
}

func (s *S3Store) List(ctx context.Context, prefix string, maxKeys int) ([]ObjectInfo, error) {
	if maxKeys <= 0 {
		maxKeys = 1000
	}
	out := make([]ObjectInfo, 0)
	for o := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: prefix, MaxKeys: maxKeys, Recursive: true, WithMetadata: true}) {
		if o.Err != nil {
			return nil, fmt.Errorf("s3 list: %w", o.Err)
		}
		out = append(out, ObjectInfo{Key: o.Key, Size: o.Size, ContentType: o.ContentType, LastModified: o.LastModified,
			ETag: o.ETag, UserMetadata: lowerKeys(o.UserMetadata)})
		if len(out) == maxKeys {
			break
		}
	}
	return out, nil
}

func (s *S3Store) Health(ctx context.Context) error {
	_, err := s.List(ctx, "", 1)
	return err
}

func mapS3Err(err error) error {
	if minio.ToErrorResponse(err).Code == noSuchKeyError {
		return fmt.Errorf("%w: %v", ErrNotFound, err)
	}
	return err
}
