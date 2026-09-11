package implementations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"image-gallery/internal/domain/image"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// ImageServiceImpl implements the image.ImageService interface
type ImageServiceImpl struct {
	imageRepo image.Repository
	tagRepo   image.TagRepository
	storage   image.StorageService
	processor image.ImageProcessor
	validator image.ValidationService
	eventPub  image.EventPublisher // can be nil
	cache     image.CacheService   // can be nil

	// Observability
	tracer               trace.Tracer
	imageUploadCounter   metric.Int64Counter
	imageProcessingTime  metric.Float64Histogram
	imageDeletionCounter metric.Int64Counter
	imageDeletionTime    metric.Float64Histogram
	cacheHitCounter      metric.Int64Counter
	cacheMissCounter     metric.Int64Counter
}

// NewImageService creates a new image service implementation
func NewImageService(
	imageRepo image.Repository,
	tagRepo image.TagRepository,
	storage image.StorageService,
	processor image.ImageProcessor,
	validator image.ValidationService,
	eventPub image.EventPublisher,
	cache image.CacheService,
) image.ImageService {
	tracer := otel.Tracer("image-gallery/service/image")
	meter := otel.Meter("image-gallery/service/image")

	// Create metrics (ignore errors for graceful degradation)
	uploadCounter, err := meter.Int64Counter(
		"image.uploads.total",
		metric.WithDescription("Total number of image uploads"),
		metric.WithUnit("{upload}"),
	)
	if err != nil {
		uploadCounter = nil
	}

	processingTime, err := meter.Float64Histogram(
		"image.processing.duration",
		metric.WithDescription("Duration of image processing operations"),
		metric.WithUnit("s"),
	)
	if err != nil {
		processingTime = nil
	}

	cacheHitCounter, err := meter.Int64Counter(
		"image.cache.hits",
		metric.WithDescription("Number of cache hits"),
		metric.WithUnit("{hit}"),
	)
	if err != nil {
		cacheHitCounter = nil
	}

	cacheMissCounter, err := meter.Int64Counter(
		"image.cache.misses",
		metric.WithDescription("Number of cache misses"),
		metric.WithUnit("{miss}"),
	)
	if err != nil {
		cacheMissCounter = nil
	}

	deletionCounter, err := meter.Int64Counter(
		"image.deletions.total",
		metric.WithDescription("Total number of image deletions"),
		metric.WithUnit("{deletion}"),
	)
	if err != nil {
		deletionCounter = nil
	}

	deletionTime, err := meter.Float64Histogram(
		"image.deletion.duration",
		metric.WithDescription("Duration of image deletion operations"),
		metric.WithUnit("s"),
	)
	if err != nil {
		deletionTime = nil
	}

	return &ImageServiceImpl{
		imageRepo:            imageRepo,
		tagRepo:              tagRepo,
		storage:              storage,
		processor:            processor,
		validator:            validator,
		eventPub:             eventPub,
		cache:                cache,
		tracer:               tracer,
		imageUploadCounter:   uploadCounter,
		imageProcessingTime:  processingTime,
		imageDeletionCounter: deletionCounter,
		imageDeletionTime:    deletionTime,
		cacheHitCounter:      cacheHitCounter,
		cacheMissCounter:     cacheMissCounter,
	}
}

// CreateImage handles the complete image creation process
func (s *ImageServiceImpl) CreateImage(ctx context.Context, req *image.CreateImageRequest, data io.Reader) (*image.Image, error) {
	startTime := time.Now()

	// Build attributes for the span
	attrs := []attribute.KeyValue{
		attribute.String("image.filename", req.OriginalFilename),
		attribute.String("image.content_type", req.ContentType),
		attribute.Int64("image.size", req.FileSize),
		attribute.Int("tags.count", len(req.Tags)),
	}
	if req.Width != nil {
		attrs = append(attrs, attribute.Int("image.width", *req.Width))
	}
	if req.Height != nil {
		attrs = append(attrs, attribute.Int("image.height", *req.Height))
	}

	ctx, span := s.tracer.Start(ctx, "CreateImage", trace.WithAttributes(attrs...))
	defer span.End()

	if req == nil {
		err := fmt.Errorf("create request cannot be nil")
		span.RecordError(err)
		span.SetStatus(codes.Error, "nil request")
		return nil, err
	}

	span.AddEvent("validating_request")
	if err := s.validateCreateRequest(ctx, req, data); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "validation failed")
		return nil, err
	}

	span.AddEvent("storing_image_file")
	storageResp, err := s.storeImageFile(ctx, req, data)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "storage failed")
		return nil, err
	}
	span.SetAttributes(attribute.String("storage.path", storageResp))

	span.AddEvent("processing_tags")
	tags, err := s.processTags(ctx, req.Tags)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "tag processing failed")
		return nil, err
	}

	img := s.buildImageObject(req, storageResp, tags)
	if err := img.Validate(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "validation failed")
		return nil, fmt.Errorf("final image validation failed: %w", err)
	}

	span.AddEvent("saving_to_database")
	if err := s.saveImageToDatabase(ctx, img, storageResp); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "database save failed")
		return nil, err
	}
	span.SetAttributes(attribute.Int("image.id", img.ID))

	s.handlePostCreation(ctx, img)

	// Record metrics
	s.recordImageCreationMetrics(ctx, time.Since(startTime).Seconds(), req.ContentType)

	span.SetStatus(codes.Ok, "")
	span.AddEvent("image_created_successfully")
	return img, nil
}

func (s *ImageServiceImpl) recordImageCreationMetrics(ctx context.Context, duration float64, contentType string) {
	if s.imageProcessingTime != nil {
		s.imageProcessingTime.Record(ctx, duration,
			metric.WithAttributes(
				attribute.String("operation", "create"),
				attribute.String("content_type", contentType),
			),
		)
	}
	if s.imageUploadCounter != nil {
		s.imageUploadCounter.Add(ctx, 1,
			metric.WithAttributes(
				attribute.String("content_type", contentType),
				attribute.String("status", "success"),
			),
		)
	}
}

func (s *ImageServiceImpl) validateCreateRequest(ctx context.Context, req *image.CreateImageRequest, data io.Reader) error {
	if err := s.validator.ValidateImageUpload(ctx, req); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}
	if err := req.Validate(); err != nil {
		return fmt.Errorf("domain validation failed: %w", err)
	}
	return s.processor.ValidateImage(ctx, data, req.ContentType)
}

func (s *ImageServiceImpl) storeImageFile(ctx context.Context, req *image.CreateImageRequest, data io.Reader) (string, error) {
	filename := s.generateUniqueFilename(req.OriginalFilename)
	// Pass actual file size to storage layer to prevent MinIO SDK memory buffering
	storageResp, err := s.storage.Store(ctx, filename, req.ContentType, data, req.FileSize)
	if err != nil {
		return "", fmt.Errorf("failed to store image: %w", err)
	}
	return storageResp, nil
}

func (s *ImageServiceImpl) processTags(ctx context.Context, tagNames []string) ([]image.Tag, error) {
	tags := make([]image.Tag, 0, len(tagNames))
	for _, tagName := range tagNames {
		tag, err := s.tagRepo.GetOrCreate(ctx, tagName)
		if err != nil {
			return nil, fmt.Errorf("failed to create/get tag %s: %w", tagName, err)
		}
		tags = append(tags, *tag)
	}
	return tags, nil
}

func (s *ImageServiceImpl) buildImageObject(req *image.CreateImageRequest, storageResp string, tags []image.Tag) *image.Image {
	now := time.Now()
	return &image.Image{
		Filename:         s.generateUniqueFilename(req.OriginalFilename),
		OriginalFilename: req.OriginalFilename,
		ContentType:      req.ContentType,
		FileSize:         req.FileSize,
		StoragePath:      storageResp,
		Width:            req.Width,
		Height:           req.Height,
		UploadedAt:       now,
		Metadata:         req.Metadata,
		CreatedAt:        now,
		UpdatedAt:        now,
		Tags:             tags,
	}
}

func (s *ImageServiceImpl) saveImageToDatabase(ctx context.Context, img *image.Image, storageResp string) error {
	if err := s.imageRepo.Create(ctx, img); err != nil {
		if deleteErr := s.storage.Delete(ctx, storageResp); deleteErr != nil {
			_ = deleteErr // explicitly ignore cleanup errors
		}
		return fmt.Errorf("failed to save image to database: %w", err)
	}
	return nil
}

func (s *ImageServiceImpl) handlePostCreation(ctx context.Context, img *image.Image) {
	if s.cache != nil {
		if err := s.cache.InvalidateImageLists(ctx); err != nil {
			_ = err
		}
	}
	if s.eventPub != nil {
		if err := s.eventPub.PublishImageCreated(ctx, img); err != nil {
			_ = err
		}
	}
}

// generateUniqueFilename creates a unique filename using timestamp and hash
func (s *ImageServiceImpl) generateUniqueFilename(originalFilename string) string {
	ext := filepath.Ext(originalFilename)
	base := filepath.Base(originalFilename)
	base = base[:len(base)-len(ext)] // Remove extension

	// Create hash of original filename and current time for uniqueness
	hasher := sha256.New()
	hasher.Write([]byte(base + strconv.FormatInt(time.Now().UnixNano(), 10)))
	hash := hex.EncodeToString(hasher.Sum(nil))[:8] // Use first 8 chars

	return fmt.Sprintf("%s_%s%s", base, hash, ext)
}

// GetImage retrieves an image by ID with all related data
func (s *ImageServiceImpl) GetImage(ctx context.Context, id int) (*image.Image, error) {
	ctx, span := s.tracer.Start(ctx, "GetImage",
		trace.WithAttributes(
			attribute.Int("image.id", id),
		),
	)
	defer span.End()

	// Try to get from cache first
	if s.cache != nil {
		if cachedImage, err := s.cache.GetImage(ctx, id); err == nil {
			span.AddEvent("cache_hit")
			span.SetAttributes(attribute.Bool("cache.hit", true))
			if s.cacheHitCounter != nil {
				s.cacheHitCounter.Add(ctx, 1,
					metric.WithAttributes(attribute.String("operation", "get_image")),
				)
			}
			span.SetStatus(codes.Ok, "")
			return cachedImage, nil
		}
		// If cache miss or error, continue to database
		span.AddEvent("cache_miss")
		span.SetAttributes(attribute.Bool("cache.hit", false))
		if s.cacheMissCounter != nil {
			s.cacheMissCounter.Add(ctx, 1,
				metric.WithAttributes(attribute.String("operation", "get_image")),
			)
		}
	}

	// Get from database
	span.AddEvent("fetching_from_database")
	img, err := s.imageRepo.GetByID(ctx, id)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "database fetch failed")
		return nil, err
	}

	span.SetAttributes(
		attribute.String("image.filename", img.Filename),
		attribute.String("image.content_type", img.ContentType),
		attribute.Int64("image.size", img.FileSize),
	)

	// Cache the result for future requests
	if s.cache != nil {
		// Cache for 1 hour (3600 seconds)
		if cacheErr := s.cache.SetImage(ctx, img, 3600); cacheErr != nil {
			// Cache errors are intentionally ignored to avoid impacting user requests
			// In production, this would be logged for monitoring purposes
			span.AddEvent("cache_set_failed")
			_ = cacheErr // explicitly ignore
		} else {
			span.AddEvent("cached_for_future_requests")
		}
	}

	span.SetStatus(codes.Ok, "")
	return img, nil
}

// ListImages retrieves images based on criteria
func (s *ImageServiceImpl) ListImages(ctx context.Context, req *image.ListImagesRequest) (*image.ListImagesResponse, error) {
	// Generate cache key based on request parameters
	cacheKey := generateListCacheKey(req)

	// Try to get from cache first
	if s.cache != nil {
		if cachedResponse, err := s.cache.GetImageList(ctx, cacheKey); err == nil {
			return cachedResponse, nil
		}
		// If cache miss or error, continue to database
	}

	// Get from database
	response, err := s.imageRepo.List(ctx, req)
	if err != nil {
		return nil, err
	}

	// Cache the result for future requests
	if s.cache != nil {
		// Cache for 30 minutes (1800 seconds) - lists change more frequently
		if cacheErr := s.cache.SetImageList(ctx, cacheKey, response, 1800); cacheErr != nil {
			// Cache errors are intentionally ignored to avoid impacting user requests
			// In production, this would be logged for monitoring purposes
			_ = cacheErr // explicitly ignore
		}
	}

	return response, nil
}

// generateListCacheKey creates a cache key based on list request parameters
func generateListCacheKey(req *image.ListImagesRequest) string {
	if req == nil {
		return "list_default"
	}

	// Include Tags array and MatchAll in cache key
	var tagsKey string
	if len(req.Tags) > 0 {
		tagsKey = strings.Join(req.Tags, ",")
	} else if req.Tag != "" {
		tagsKey = req.Tag
	}

	matchAllKey := "any"
	if req.MatchAll {
		matchAllKey = "all"
	}

	return fmt.Sprintf("list_page_%d_pagesize_%d_tags_%s_match_%s",
		req.Page, req.PageSize, tagsKey, matchAllKey)
}

// UpdateImage modifies an existing image
func (s *ImageServiceImpl) UpdateImage(ctx context.Context, id int, req *image.UpdateImageRequest) (*image.Image, error) {
	if req == nil {
		return nil, fmt.Errorf("update request cannot be nil")
	}

	if err := s.validator.ValidateImageUpdate(ctx, id, req); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	existing, err := s.imageRepo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get existing image: %w", err)
	}

	tags, err := s.processTags(ctx, req.Tags)
	if err != nil {
		return nil, err
	}

	existing.Tags = tags
	existing.UpdatedAt = time.Now()

	if err := existing.Validate(); err != nil {
		return nil, fmt.Errorf("updated image validation failed: %w", err)
	}

	if err := s.imageRepo.Update(ctx, existing); err != nil {
		return nil, fmt.Errorf("failed to update image in database: %w", err)
	}

	s.handlePostUpdate(ctx, id, existing)

	return existing, nil
}

func (s *ImageServiceImpl) handlePostUpdate(ctx context.Context, id int, img *image.Image) {
	if s.cache != nil {
		if err := s.cache.DeleteImage(ctx, id); err != nil {
			_ = err
		}
		if err := s.cache.InvalidateImageLists(ctx); err != nil {
			_ = err
		}
	}
	if s.eventPub != nil {
		if err := s.eventPub.PublishImageUpdated(ctx, img); err != nil {
			_ = err
		}
	}
}

// DeleteImage removes an image and its associated files
func (s *ImageServiceImpl) DeleteImage(ctx context.Context, id int) error {
	startTime := time.Now()

	// Create span for deletion operation
	ctx, span := s.tracer.Start(ctx, "DeleteImage",
		trace.WithAttributes(
			attribute.Int("image.id", id),
		),
	)
	defer span.End()

	span.AddEvent("validating_deletion")
	if err := s.validator.ValidateImageDeletion(ctx, id); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "validation failed")
		s.recordDeletionMetrics(ctx, time.Since(startTime).Seconds(), "error", "validation_failed")
		return fmt.Errorf("deletion validation failed: %w", err)
	}

	span.AddEvent("fetching_image_metadata")
	img, err := s.imageRepo.GetByID(ctx, id)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "image not found")
		s.recordDeletionMetrics(ctx, time.Since(startTime).Seconds(), "error", "not_found")
		return fmt.Errorf("failed to get image for deletion: %w", err)
	}

	span.SetAttributes(
		attribute.String("image.filename", img.Filename),
		attribute.String("image.storage_path", img.StoragePath),
		attribute.String("image.content_type", img.ContentType),
		attribute.Int64("image.size", img.FileSize),
	)

	span.AddEvent("deleting_from_database")
	if err := s.imageRepo.Delete(ctx, id); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "database deletion failed")
		s.recordDeletionMetrics(ctx, time.Since(startTime).Seconds(), "error", "database_failed")
		return fmt.Errorf("failed to delete image from database: %w", err)
	}

	span.AddEvent("cleaning_up_storage")
	s.cleanupStorage(ctx, img.StoragePath)

	span.AddEvent("post_deletion_cleanup")
	s.handlePostDeletion(ctx, id)

	span.SetStatus(codes.Ok, "")
	span.AddEvent("deletion_completed")

	s.recordDeletionMetrics(ctx, time.Since(startTime).Seconds(), "success", "")

	return nil
}

func (s *ImageServiceImpl) recordDeletionMetrics(ctx context.Context, duration float64, status string, errorType string) {
	attrs := []attribute.KeyValue{
		attribute.String("status", status),
	}
	if errorType != "" {
		attrs = append(attrs, attribute.String("error_type", errorType))
	}

	if s.imageDeletionTime != nil {
		s.imageDeletionTime.Record(ctx, duration, metric.WithAttributes(attrs...))
	}
	if s.imageDeletionCounter != nil {
		s.imageDeletionCounter.Add(ctx, 1, metric.WithAttributes(attrs...))
	}
}

func (s *ImageServiceImpl) cleanupStorage(ctx context.Context, storagePath string) {
	if storagePath != "" {
		if err := s.storage.Delete(ctx, storagePath); err != nil {
			_ = err
		}
	}
}

func (s *ImageServiceImpl) handlePostDeletion(ctx context.Context, id int) {
	if s.cache != nil {
		if err := s.cache.DeleteImage(ctx, id); err != nil {
			_ = err
		}
		if err := s.cache.InvalidateImageLists(ctx); err != nil {
			_ = err
		}
	}
	if s.eventPub != nil {
		if err := s.eventPub.PublishImageDeleted(ctx, id); err != nil {
			_ = err
		}
	}
}

// DownloadImage provides access to the original image file
func (s *ImageServiceImpl) DownloadImage(ctx context.Context, id int) (io.ReadCloser, string, error) {
	// TODO: Implement image download workflow
	return nil, "", nil
}

// GenerateImageURL creates a URL for accessing an image
func (s *ImageServiceImpl) GenerateImageURL(ctx context.Context, id int, expiry int64) (string, error) {
	// TODO: Implement URL generation workflow
	return "", nil
}

// GetImageStats returns statistics about images
func (s *ImageServiceImpl) GetImageStats(ctx context.Context) (*image.ImageStats, error) {
	stats, err := s.imageRepo.GetStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get image stats: %w", err)
	}
	return stats, nil
}
