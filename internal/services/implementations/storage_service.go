package implementations

import (
	"context"
	"io"
	"time"

	"image-gallery/internal/domain/image"
	"image-gallery/internal/platform/storage"

	"github.com/minio/minio-go/v7"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// StorageServiceImpl implements the image.StorageService interface
type StorageServiceImpl struct {
	client  *storage.MinIOClient
	service *storage.Service

	// Observability
	tracer                    trace.Tracer
	storageOperationsCounter  metric.Int64Counter
	storageOperationsDuration metric.Float64Histogram
	storageBytesTransferred   metric.Int64Counter
}

// NewStorageService creates a new storage service implementation
func NewStorageService(client *storage.MinIOClient) image.StorageService {
	return initStorageService(&StorageServiceImpl{
		client: client,
	})
}

// NewStorageServiceWithService creates a new storage service with the full Service implementation
func NewStorageServiceWithService(service *storage.Service) image.StorageService {
	return initStorageService(&StorageServiceImpl{
		service: service,
	})
}

// initStorageService initializes observability for storage service
func initStorageService(s *StorageServiceImpl) *StorageServiceImpl {
	s.tracer = otel.Tracer("image-gallery/service/storage")
	meter := otel.Meter("image-gallery/service/storage")

	// Create metrics (ignore errors for graceful degradation)
	var err error
	s.storageOperationsCounter, err = meter.Int64Counter(
		"storage.operations.total",
		metric.WithDescription("Total number of storage operations"),
		metric.WithUnit("{operation}"),
	)
	if err != nil {
		s.storageOperationsCounter = nil
	}

	s.storageOperationsDuration, err = meter.Float64Histogram(
		"storage.operation.duration",
		metric.WithDescription("Duration of storage operations"),
		metric.WithUnit("s"),
	)
	if err != nil {
		s.storageOperationsDuration = nil
	}

	s.storageBytesTransferred, err = meter.Int64Counter(
		"storage.bytes.transferred",
		metric.WithDescription("Total bytes transferred to/from storage"),
		metric.WithUnit("By"),
	)
	if err != nil {
		s.storageBytesTransferred = nil
	}

	return s
}

// Store saves a file and returns the storage path
func (s *StorageServiceImpl) Store(ctx context.Context, filename string, contentType string, data io.Reader, size int64) (string, error) {
	startTime := time.Now()
	ctx, span := s.tracer.Start(ctx, "Store",
		trace.WithAttributes(
			attribute.String("storage.filename", filename),
			attribute.String("storage.content_type", contentType),
			attribute.Int64("storage.size", size),
		),
	)
	defer span.End()

	var path string
	var err error

	if s.service != nil {
		path, err = s.service.Store(ctx, filename, contentType, data, size)
	} else {
		// Fallback to MinIOClient if service not available
		path = "images/" + filename
		err = s.client.UploadFile(ctx, path, data, size, contentType)
	}

	// Record metrics
	duration := time.Since(startTime).Seconds()
	attrs := []attribute.KeyValue{
		attribute.String("operation", "store"),
		attribute.String("content_type", contentType),
	}

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "store failed")
		attrs = append(attrs, attribute.String("status", "error"))
	} else {
		span.SetAttributes(attribute.String("storage.path", path))
		span.SetStatus(codes.Ok, "")
		attrs = append(attrs, attribute.String("status", "success"))
	}

	if s.storageOperationsCounter != nil {
		s.storageOperationsCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
	if s.storageOperationsDuration != nil {
		s.storageOperationsDuration.Record(ctx, duration, metric.WithAttributes(attrs...))
	}

	return path, err
}

// Retrieve gets a file from storage
func (s *StorageServiceImpl) Retrieve(ctx context.Context, path string) (io.ReadCloser, error) {
	startTime := time.Now()
	ctx, span := s.tracer.Start(ctx, "Retrieve",
		trace.WithAttributes(
			attribute.String("storage.path", path),
		),
	)
	defer span.End()

	var obj io.ReadCloser
	var err error

	if s.service != nil {
		obj, err = s.service.Retrieve(ctx, path)
	} else {
		// Fallback to MinIOClient. GetFile/GetObject is lazy and does not
		// contact the server, so it never errors on a missing key - only
		// Stat (or Read) does. Stat eagerly here so a missing object is
		// reported as an error from Retrieve instead of surfacing later
		// on the first Read of the returned reader.
		var minioObj *minio.Object
		minioObj, err = s.client.GetFile(ctx, path)
		if err == nil {
			if _, statErr := minioObj.Stat(); statErr != nil {
				_ = minioObj.Close() //nolint:errcheck // Cleanup operation in error path
				err = statErr
			} else {
				obj = minioObj
			}
		}
	}

	// Record metrics
	duration := time.Since(startTime).Seconds()
	attrs := []attribute.KeyValue{
		attribute.String("operation", "retrieve"),
	}

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "retrieve failed")
		attrs = append(attrs, attribute.String("status", "error"))
	} else {
		span.SetStatus(codes.Ok, "")
		attrs = append(attrs, attribute.String("status", "success"))
	}

	if s.storageOperationsCounter != nil {
		s.storageOperationsCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
	if s.storageOperationsDuration != nil {
		s.storageOperationsDuration.Record(ctx, duration, metric.WithAttributes(attrs...))
	}

	return obj, err
}

// Delete removes a file from storage
func (s *StorageServiceImpl) Delete(ctx context.Context, path string) error {
	startTime := time.Now()
	ctx, span := s.tracer.Start(ctx, "Delete",
		trace.WithAttributes(
			attribute.String("storage.path", path),
		),
	)
	defer span.End()

	var err error
	if s.service != nil {
		err = s.service.Delete(ctx, path)
	} else {
		err = s.client.DeleteFile(ctx, path)
	}

	// Record metrics
	duration := time.Since(startTime).Seconds()
	attrs := []attribute.KeyValue{
		attribute.String("operation", "delete"),
	}

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "delete failed")
		attrs = append(attrs, attribute.String("status", "error"))
	} else {
		span.SetStatus(codes.Ok, "")
		attrs = append(attrs, attribute.String("status", "success"))
	}

	if s.storageOperationsCounter != nil {
		s.storageOperationsCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
	if s.storageOperationsDuration != nil {
		s.storageOperationsDuration.Record(ctx, duration, metric.WithAttributes(attrs...))
	}

	return err
}

// Exists checks if a file exists in storage
func (s *StorageServiceImpl) Exists(ctx context.Context, path string) (bool, error) {
	if s.service != nil {
		return s.service.Exists(ctx, path)
	}
	// Check if file exists using MinIOClient. GetFile/GetObject is lazy and
	// does not contact the server, so Stat is required to actually verify
	// the object is present.
	obj, err := s.client.GetFile(ctx, path)
	if err != nil {
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return false, nil
		}
		return false, err
	}
	defer func() { _ = obj.Close() }() //nolint:errcheck // Cleanup operation, error not actionable

	if _, err := obj.Stat(); err != nil {
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// GenerateURL creates a temporary or permanent URL for file access
func (s *StorageServiceImpl) GenerateURL(ctx context.Context, path string, expiry int64) (string, error) {
	if s.service != nil {
		return s.service.GenerateURL(ctx, path, expiry)
	}
	// Return a simple URL path for MinIOClient
	return s.client.GetFileURL(path), nil
}

// GetFileInfo returns metadata about a stored file
func (s *StorageServiceImpl) GetFileInfo(ctx context.Context, path string) (*image.FileInfo, error) {
	if s.service != nil {
		info, err := s.service.GetFileInfo(ctx, path)
		if err != nil {
			return nil, err
		}
		return &image.FileInfo{
			Path:         info.Path,
			Size:         info.Size,
			ContentType:  info.ContentType,
			LastModified: info.LastModified,
			ETag:         info.ETag,
		}, nil
	}
	// Return minimal info for MinIOClient
	return &image.FileInfo{
		Path:         path,
		Size:         0,
		ContentType:  "application/octet-stream",
		LastModified: time.Now().Unix(),
	}, nil
}

// ListObjects lists objects in storage (additional method for gallery)
func (s *StorageServiceImpl) ListObjects(ctx context.Context, prefix string, maxKeys int) ([]ObjectInfo, error) {
	if s.service != nil {
		objects, err := s.service.ListObjects(ctx, prefix, maxKeys)
		if err != nil {
			return nil, err
		}
		// Convert from storage.ObjectInfo to local ObjectInfo
		result := make([]ObjectInfo, len(objects))
		for i, obj := range objects {
			result[i] = ObjectInfo{
				Key:          obj.Key,
				Size:         obj.Size,
				ContentType:  obj.ContentType,
				LastModified: obj.LastModified,
				ETag:         obj.ETag,
				UserMetadata: obj.UserMetadata,
			}
		}
		return result, nil
	}

	// Use MinIOClient to list objects directly
	if s.client != nil {
		objects, err := s.client.ListObjects(ctx, prefix, maxKeys)
		if err != nil {
			return nil, err
		}

		// Convert from storage.MinIOObjectInfo to local ObjectInfo
		result := make([]ObjectInfo, len(objects))
		for i, obj := range objects {
			result[i] = ObjectInfo{
				Key:          obj.Key,
				Size:         obj.Size,
				ContentType:  obj.ContentType,
				LastModified: obj.LastModified,
				ETag:         obj.ETag,
				UserMetadata: obj.UserMetadata,
			}
		}
		return result, nil
	}

	return []ObjectInfo{}, nil
}

// ObjectInfo represents information about a stored object (for compatibility)
type ObjectInfo struct {
	Key          string            `json:"key"`
	Size         int64             `json:"size"`
	ContentType  string            `json:"content_type"`
	LastModified time.Time         `json:"last_modified"`
	ETag         string            `json:"etag"`
	UserMetadata map[string]string `json:"user_metadata,omitempty"`
}
