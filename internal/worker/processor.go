// Package worker turns process_image jobs into thumbnails and metadata.
package worker

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif" // decoders for image.DecodeConfig, registered here rather than relied on from storage
	_ "image/jpeg"
	_ "image/png"
	"io"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	_ "golang.org/x/image/webp"

	domain "image-gallery/internal/domain/image"
	obs "image-gallery/internal/observability"
	"image-gallery/internal/platform/queue"
	"image-gallery/internal/platform/storage"
)

// Faults injects demo faults into job processing (nil = none).
type Faults interface {
	WorkerFault(ctx context.Context) error
}

// Processor handles process_image jobs. Processing is idempotent by image ID:
// the thumbnail key is deterministic and a ready image is skipped.
type Processor struct {
	images    domain.Repository
	store     domain.StorageService
	decoder   *storage.ImageProcessor
	faults    Faults
	log       *obs.Logger
	tracer    trace.Tracer
	stepDur   metric.Float64Histogram
	maxBytes  int64 // download cap: 10 MiB, mirrors the upload cap
	maxPixels int   // decode guard: bounds worker memory per job
	thumbSize int
}

// NewProcessor builds a Processor; faults and log may be nil.
//
//nolint:errcheck // instrument name is a constant; the SDK returns a usable instrument alongside any error
func NewProcessor(images domain.Repository, store domain.StorageService, faults Faults, log *obs.Logger) *Processor {
	stepDur, _ := otel.Meter("image-gallery/worker").Float64Histogram(obs.MetricImageProcessing,
		metric.WithUnit("s"), metric.WithDescription("Duration of each image-processing step"))
	return &Processor{
		images: images, store: store, decoder: storage.NewImageProcessor(0, 0, 85), faults: faults, log: log,
		tracer: otel.Tracer("image-gallery/worker"), stepDur: stepDur,
		maxBytes: 10 << 20, maxPixels: 40_000_000, thumbSize: 320,
	}
}

// Handle is the queue.Handler for process_image jobs. Every exit path leaves
// the row in a defensible state: ready (success), unchanged (skip, or a
// failure before the status write), processing (a retryable failure — the
// next attempt, or eventually OnDeadLetter, resolves it) or failed (via
// OnDeadLetter once retries are exhausted).
func (p *Processor) Handle(ctx context.Context, job queue.Job) (err error) {
	start := time.Now()
	defer func() { p.logOutcome(ctx, job, err, time.Since(start)) }()

	if job.Type != queue.JobTypeProcessImage {
		return queue.Permanent(fmt.Errorf("unknown job type %q", job.Type))
	}
	img, err := p.images.GetByID(ctx, job.ImageID)
	if err != nil {
		return fmt.Errorf("load image %d: %w", job.ImageID, err)
	}
	if img.Status == domain.StatusReady {
		return queue.ErrSkip
	}
	if err := p.images.UpdateStatus(ctx, img.ID, domain.StatusProcessing, nil); err != nil {
		return err
	}
	if err := p.injectFault(ctx); err != nil {
		return err // retryable: the demo shows retries, then the dead letter
	}
	res, err := p.process(ctx, img)
	if err != nil {
		return err
	}
	return p.images.CompleteProcessing(ctx, img.ID, *res)
}

// injectFault lets Faults (Task 14's demo-controls injector) fail a job on
// purpose; a nil Faults means none are configured.
func (p *Processor) injectFault(ctx context.Context) error {
	if p.faults == nil {
		return nil
	}
	return p.faults.WorkerFault(ctx)
}

// process runs the fetch/decode/thumbnail/store pipeline, each step under its
// own span and duration sample.
func (p *Processor) process(ctx context.Context, img *domain.Image) (*domain.ProcessingResult, error) {
	var data, thumb []byte
	var info *storage.ImageInfo
	var key string

	if err := p.step(ctx, img.ID, "fetch", func(ctx context.Context) (err error) {
		data, err = p.fetch(ctx, img.StoragePath)
		return err
	}); err != nil {
		return nil, err
	}
	if err := p.step(ctx, img.ID, "decode", func(ctx context.Context) (err error) {
		info, err = p.decode(ctx, data)
		return err
	}); err != nil {
		return nil, err
	}
	if err := p.step(ctx, img.ID, "thumbnail", func(ctx context.Context) (err error) {
		thumb, err = p.thumbnail(ctx, data)
		return err
	}); err != nil {
		return nil, err
	}
	if err := p.step(ctx, img.ID, "store", func(ctx context.Context) (err error) {
		key, err = p.storeThumbnail(ctx, img.ID, info.Format, thumb)
		return err
	}); err != nil {
		return nil, err
	}
	return &domain.ProcessingResult{
		ThumbnailPath: key, Width: info.Width, Height: info.Height,
		Format: info.Format, ColorSpace: info.ColorSpace, HasAlpha: info.HasAlpha,
	}, nil
}

// OnDeadLetter marks the image failed once retries are exhausted. It must
// stay quick: the consumer bounds it to deadLetterHookTimeout (queue package)
// and abandons it if it runs longer.
func (p *Processor) OnDeadLetter(ctx context.Context, job queue.Job, cause error) {
	msg := cause.Error()
	if err := p.images.UpdateStatus(ctx, job.ImageID, domain.StatusFailed, &msg); err != nil {
		trace.SpanFromContext(ctx).RecordError(err)
	}
	if p.log != nil {
		p.log.Error(ctx).Int(obs.AttrImageID, job.ImageID).Str("job.id", job.ID).Err(cause).Msg("job dead-lettered")
	}
}

// fetch downloads the original, capped at maxBytes.
func (p *Processor) fetch(ctx context.Context, path string) ([]byte, error) {
	rc, err := p.store.Retrieve(ctx, path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }() //nolint:errcheck // best-effort close after a successful read
	data, err := io.ReadAll(io.LimitReader(rc, p.maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > p.maxBytes {
		return nil, queue.Permanent(fmt.Errorf("object larger than %d bytes", p.maxBytes))
	}
	return data, nil
}

// decode rejects images too large to process, then extracts their metadata.
// The size check reads the header only: GetImageInfo fully decodes, and a file
// well under maxBytes can declare enough pixels to exhaust worker memory.
func (p *Processor) decode(ctx context.Context, data []byte) (*storage.ImageInfo, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, queue.Permanent(fmt.Errorf("decode: %w", err))
	}
	if cfg.Width*cfg.Height > p.maxPixels {
		return nil, queue.Permanent(fmt.Errorf("image too large to process: %dx%d", cfg.Width, cfg.Height))
	}
	info, err := p.decoder.GetImageInfo(ctx, bytes.NewReader(data))
	if err != nil {
		return nil, queue.Permanent(fmt.Errorf("decode: %w", err))
	}
	return info, nil
}

// thumbnail renders the fixed-size thumbnail.
func (p *Processor) thumbnail(ctx context.Context, data []byte) ([]byte, error) {
	r, err := p.decoder.GenerateThumbnail(ctx, bytes.NewReader(data), p.thumbSize, p.thumbSize)
	if err != nil {
		return nil, queue.Permanent(fmt.Errorf("thumbnail: %w", err))
	}
	return io.ReadAll(r)
}

// storeThumbnail writes the thumbnail under its deterministic key (rewriting
// the same key on reprocessing keeps this idempotent).
func (p *Processor) storeThumbnail(ctx context.Context, imageID int, format string, thumb []byte) (string, error) {
	ext, contentType := thumbnailFormat(format)
	key := fmt.Sprintf("thumbnails/%d%s", imageID, ext)
	if err := p.store.StoreAt(ctx, key, contentType, bytes.NewReader(thumb), int64(len(thumb))); err != nil {
		return "", err
	}
	return key, nil
}

// step runs fn under an image.<name> span and records its duration.
func (p *Processor) step(ctx context.Context, imageID int, name string, fn func(context.Context) error) error {
	start := time.Now()
	ctx, span := p.tracer.Start(ctx, "image."+name, trace.WithAttributes(attribute.Int(obs.AttrImageID, imageID)))
	err := fn(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, name+" failed")
	}
	span.End()
	p.stepDur.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(attribute.String(obs.AttrProcessingStep, name)))
	return err
}

// formatPNG is the decoded image format name (as returned by
// ImageProcessor.GetImageInfo) for PNG images, kept as a constant since the
// bare string "png" also appears in this package's tests (goconst).
const formatPNG = "png"

// thumbnailFormat mirrors ImageProcessor.GenerateThumbnail's encoder choice.
func thumbnailFormat(format string) (ext, contentType string) {
	switch format {
	case formatPNG:
		return ".png", "image/png"
	case "gif":
		return ".gif", "image/gif"
	default:
		return ".jpg", "image/jpeg"
	}
}

// logOutcome logs each attempt: success, or a failure worth a line (a skip is
// the normal "nothing to do" case and stays quiet).
func (p *Processor) logOutcome(ctx context.Context, job queue.Job, err error, d time.Duration) {
	if p.log == nil {
		return
	}
	switch {
	case err == nil:
		p.log.Info(ctx).Int(obs.AttrImageID, job.ImageID).Str("job.id", job.ID).Dur("duration", d).Msg("job processed")
	case err != queue.ErrSkip:
		p.log.Warn(ctx).Int(obs.AttrImageID, job.ImageID).Str("job.id", job.ID).
			Bool("permanent", queue.IsPermanent(err)).Err(err).Msg("job attempt failed")
	}
}
