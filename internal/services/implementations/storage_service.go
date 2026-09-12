package implementations

import (
	"context"
	"errors"
	"io"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"image-gallery/internal/domain/image"
	obs "image-gallery/internal/observability"
	"image-gallery/internal/platform/storage"
)

// StorageServiceImpl is the single instrumentation point for object storage:
// both backends emit the same storage.* spans and metrics, told apart by
// storage.provider.
type StorageServiceImpl struct {
	service     *storage.Service
	provider    string
	tracer      trace.Tracer
	ops         metric.Int64Counter
	duration    metric.Float64Histogram
	transferred metric.Int64Counter
}

// NewStorageService instruments svc.
//
//nolint:errcheck // instrument names are constants; the SDK returns a usable instrument alongside any error
func NewStorageService(svc *storage.Service) image.StorageService {
	meter := otel.Meter("image-gallery/storage")
	ops, _ := meter.Int64Counter(obs.MetricStorageOps, metric.WithUnit("{operation}"), metric.WithDescription("Object storage operations"))
	duration, _ := meter.Float64Histogram(obs.MetricStorageDuration, metric.WithUnit("s"), metric.WithDescription("Object storage operation duration"))
	transferred, _ := meter.Int64Counter(obs.MetricStorageTransferred, metric.WithUnit("By"), metric.WithDescription("Bytes written to and read from object storage"))
	return &StorageServiceImpl{service: svc, provider: svc.Provider(), tracer: otel.Tracer("image-gallery/storage"),
		ops: ops, duration: duration, transferred: transferred}
}

// observe starts a CLIENT span storage.<op> and returns the span context plus
// its finisher, which records the span status and the operation metrics.
func (s *StorageServiceImpl) observe(ctx context.Context, op, key string) (spanCtx context.Context, done func(error)) {
	start := time.Now()
	ctx, span := s.tracer.Start(ctx, "storage."+op, trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(
		attribute.String(obs.AttrStorageProvider, s.provider), attribute.String(obs.AttrStorageKey, key)))
	return ctx, func(err error) {
		outcome := obs.OutcomeSuccess
		if err != nil {
			outcome = obs.OutcomeError
			span.RecordError(err)
			span.SetStatus(codes.Error, op+" failed")
		}
		attrs := metric.WithAttributes(attribute.String(obs.AttrStorageProvider, s.provider),
			attribute.String(obs.AttrStorageOperation, op), attribute.String(obs.AttrOutcome, outcome))
		s.ops.Add(ctx, 1, attrs)
		s.duration.Record(ctx, time.Since(start).Seconds(), attrs)
		span.End()
	}
}

func (s *StorageServiceImpl) addBytes(ctx context.Context, dir string, n int64) {
	if n > 0 {
		s.transferred.Add(ctx, n, metric.WithAttributes(attribute.String(obs.AttrStorageProvider, s.provider), attribute.String(obs.AttrStorageDirection, dir)))
	}
}

func (s *StorageServiceImpl) Store(ctx context.Context, filename, contentType string, data io.Reader, size int64) (string, error) {
	ctx, done := s.observe(ctx, "put", filename)
	path, err := s.service.Store(ctx, filename, contentType, data, size)
	done(err)
	if err == nil {
		s.addBytes(ctx, "write", size)
	}
	return path, err
}

func (s *StorageServiceImpl) StoreAt(ctx context.Context, path, contentType string, data io.Reader, size int64) error {
	ctx, done := s.observe(ctx, "put", path)
	err := s.service.StoreAt(ctx, path, contentType, data, size)
	done(err)
	if err == nil {
		s.addBytes(ctx, "write", size)
	}
	return err
}

// countingReadCloser reports the bytes read, and the first read or close
// error, once, on the first Close.
type countingReadCloser struct {
	io.ReadCloser
	n       int64
	readErr error
	closed  bool
	onClose func(n int64, err error)
}

func (c *countingReadCloser) Read(p []byte) (int, error) {
	n, err := c.ReadCloser.Read(p)
	c.n += int64(n)
	if err != nil && !errors.Is(err, io.EOF) && c.readErr == nil {
		c.readErr = err
	}
	return n, err
}

func (c *countingReadCloser) Close() error {
	err := c.ReadCloser.Close()
	if !c.closed {
		c.closed = true
		c.onClose(c.n, errors.Join(c.readErr, err))
	}
	return err
}

// Retrieve ends its storage.get span when the caller closes the reader, so the
// span and storage.operation.duration cover the transfer, not just the open.
func (s *StorageServiceImpl) Retrieve(ctx context.Context, path string) (io.ReadCloser, error) {
	ctx, done := s.observe(ctx, "get", path)
	rc, err := s.service.Retrieve(ctx, path)
	if err != nil {
		done(err)
		return nil, err
	}
	return &countingReadCloser{ReadCloser: rc, onClose: func(n int64, err error) {
		s.addBytes(ctx, "read", n)
		done(err)
	}}, nil
}

func (s *StorageServiceImpl) Delete(ctx context.Context, path string) error {
	ctx, done := s.observe(ctx, "delete", path)
	err := s.service.Delete(ctx, path)
	done(err)
	return err
}

// Exists records a missing object as a successful stat, not an error.
func (s *StorageServiceImpl) Exists(ctx context.Context, path string) (bool, error) {
	ctx, done := s.observe(ctx, "stat", path)
	ok, err := s.service.Exists(ctx, path)
	done(err)
	return ok, err
}

func (s *StorageServiceImpl) GetFileInfo(ctx context.Context, path string) (*image.FileInfo, error) {
	ctx, done := s.observe(ctx, "stat", path)
	info, err := s.service.GetFileInfo(ctx, path)
	done(err)
	if err != nil {
		return nil, err
	}
	return &image.FileInfo{Path: info.Path, Size: info.Size, ContentType: info.ContentType, LastModified: info.LastModified, ETag: info.ETag}, nil
}

// ListObjects lists objects (gallery fallback and the startup sync).
func (s *StorageServiceImpl) ListObjects(ctx context.Context, prefix string, maxKeys int) ([]ObjectInfo, error) {
	ctx, done := s.observe(ctx, "list", prefix)
	objects, err := s.service.ListObjects(ctx, prefix, maxKeys)
	done(err)
	if err != nil {
		return nil, err
	}
	result := make([]ObjectInfo, len(objects))
	for i, o := range objects {
		result[i] = ObjectInfo{Key: o.Key, Size: o.Size, ContentType: o.ContentType, LastModified: o.LastModified, ETag: o.ETag, UserMetadata: o.UserMetadata}
	}
	return result, nil
}

// ObjectInfo represents information about a stored object (for compatibility).
type ObjectInfo struct {
	Key          string            `json:"key"`
	Size         int64             `json:"size"`
	ContentType  string            `json:"content_type"`
	LastModified time.Time         `json:"last_modified"`
	ETag         string            `json:"etag"`
	UserMetadata map[string]string `json:"user_metadata,omitempty"`
}
