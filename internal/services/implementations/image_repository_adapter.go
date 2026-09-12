package implementations

import (
	"context"
	"encoding/json"
	"fmt"

	"image-gallery/internal/domain/image"
	"image-gallery/internal/platform/database"
)

// CacheInvalidator drops cached list views. The worker writes image status
// directly through this adapter (UpdateStatus, CompleteProcessing), bypassing
// ImageService's own cache invalidation on Create/Update/Delete; without this,
// a list cached while an upload was still processing stays stale until its
// TTL expires. A nil invalidator disables it (e.g. caching turned off).
type CacheInvalidator interface {
	InvalidateImageLists(ctx context.Context) error
}

// ImageRepositoryAdapter adapts database ImageRepository to domain Repository interface
type ImageRepositoryAdapter struct {
	dbRepo  database.ImageRepository
	tagRepo database.TagRepository
	cache   CacheInvalidator
}

// NewImageRepositoryAdapter creates a domain repository adapter
func NewImageRepositoryAdapter(dbRepo database.ImageRepository) image.Repository {
	return &ImageRepositoryAdapter{
		dbRepo:  dbRepo,
		tagRepo: nil, // Will be set later if needed
	}
}

// SetTagRepository sets the tag repository for handling tag relationships
func (a *ImageRepositoryAdapter) SetTagRepository(tagRepo database.TagRepository) {
	a.tagRepo = tagRepo
}

// SetCacheInvalidator wires list-cache invalidation for the worker's direct
// status writes; see CacheInvalidator.
func (a *ImageRepositoryAdapter) SetCacheInvalidator(cache CacheInvalidator) {
	a.cache = cache
}

// invalidateLists best-effort drops cached list views: a failure here must
// not fail the status write it follows, so any error is discarded.
func (a *ImageRepositoryAdapter) invalidateLists(ctx context.Context) {
	if a.cache == nil {
		return
	}
	_ = a.cache.InvalidateImageLists(ctx) //nolint:errcheck // best-effort; a stale cached list expires within its TTL anyway
}

func (a *ImageRepositoryAdapter) Create(ctx context.Context, img *image.Image) error {
	dbImage := &database.Image{
		Filename:         img.Filename,
		OriginalFilename: img.OriginalFilename,
		ContentType:      img.ContentType,
		FileSize:         img.FileSize,
		StoragePath:      img.StoragePath,
		ThumbnailPath:    img.ThumbnailPath,
		Status:           img.Status,
		ProcessingError:  img.ProcessingError,
		ProcessedAt:      img.ProcessedAt,
		Width:            img.Width,
		Height:           img.Height,
		UploadedAt:       img.UploadedAt,
		Metadata:         database.Metadata{},
	}

	// Convert domain metadata to database metadata if present
	if img.Metadata != nil {
		if err := json.Unmarshal(img.Metadata, &dbImage.Metadata); err != nil {
			return fmt.Errorf("failed to convert metadata: %w", err)
		}
	}

	if err := a.dbRepo.Create(ctx, dbImage); err != nil {
		return err
	}

	// Update the domain image with generated values
	img.ID = dbImage.ID
	img.CreatedAt = dbImage.CreatedAt
	img.UpdatedAt = dbImage.UpdatedAt
	img.UploadedAt = dbImage.UploadedAt

	// Save tag associations if tags are provided
	if a.tagRepo != nil && len(img.Tags) > 0 {
		for _, tag := range img.Tags {
			if err := a.tagRepo.AddToImage(ctx, img.ID, tag.ID); err != nil {
				// If tag attachment fails, we should consider rolling back
				// For now, we'll log but continue
				// TODO: Consider transaction handling
				return fmt.Errorf("failed to attach tag %s to image: %w", tag.Name, err)
			}
		}
	}

	return nil
}

func (a *ImageRepositoryAdapter) GetByID(ctx context.Context, id int) (*image.Image, error) {
	dbImage, err := a.dbRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Load tags for this image, if a tag repository has been wired in
	if a.tagRepo != nil {
		tags, err := a.tagRepo.GetImageTags(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("failed to load tags for image %d: %w", id, err)
		}
		dbImage.Tags = make([]database.Tag, len(tags))
		for i, tag := range tags {
			dbImage.Tags[i] = *tag
		}
	}

	return a.convertToBaseImage(dbImage), nil
}

func (a *ImageRepositoryAdapter) List(ctx context.Context, req *image.ListImagesRequest) (*image.ListImagesResponse, error) {
	// Determine which tags to filter by (prioritize Tags array over single Tag)
	var tagFilters []string
	if len(req.Tags) > 0 {
		tagFilters = req.Tags
	} else if req.Tag != "" {
		tagFilters = []string{req.Tag}
	}

	// Handle tag-based filtering
	if len(tagFilters) > 0 {
		pagination := database.PaginationParams{
			Limit:  req.PageSize,
			Offset: req.GetOffset(),
		}

		// Use GetByTags with matchAll parameter
		dbImages, err := a.dbRepo.GetByTags(ctx, tagFilters, req.MatchAll, pagination)
		if err != nil {
			return nil, err
		}

		// For count, we need to get all matching images (this is simplified)
		// TODO: Add a dedicated count method for tag filtering in repository
		count := len(dbImages)
		if len(dbImages) == pagination.Limit {
			// If we got exactly the limit, there might be more
			// This is an approximation - proper implementation would use COUNT query
			count = pagination.Limit * req.Page
		}

		images := make([]image.Image, len(dbImages))
		for i, dbImg := range dbImages {
			images[i] = *a.convertToBaseImage(dbImg)
		}

		response := &image.ListImagesResponse{
			Images:     images,
			TotalCount: count,
			Page:       req.Page,
			PageSize:   req.PageSize,
		}
		response.CalculateTotalPages()

		return response, nil
	}

	// Regular list without tag filter
	pagination := database.PaginationParams{
		Limit:  req.PageSize,
		Offset: req.GetOffset(),
	}

	sort := database.SortParams{
		Field: "uploaded_at",
		Order: "DESC",
	}

	// Use GetWithTags to load tags with images
	dbImages, err := a.dbRepo.GetWithTags(ctx, pagination, sort)
	if err != nil {
		return nil, err
	}

	// Get total count
	totalCount, err := a.dbRepo.Count(ctx)
	if err != nil {
		return nil, err
	}

	images := make([]image.Image, len(dbImages))
	for i, dbImg := range dbImages {
		images[i] = *a.convertToBaseImage(dbImg)
	}

	response := &image.ListImagesResponse{
		Images:     images,
		TotalCount: totalCount,
		Page:       req.Page,
		PageSize:   req.PageSize,
	}
	response.CalculateTotalPages()

	return response, nil
}

func (a *ImageRepositoryAdapter) Update(ctx context.Context, img *image.Image) error {
	dbImage := &database.Image{
		ID:               img.ID,
		Filename:         img.Filename,
		OriginalFilename: img.OriginalFilename,
		ContentType:      img.ContentType,
		FileSize:         img.FileSize,
		StoragePath:      img.StoragePath,
		ThumbnailPath:    img.ThumbnailPath,
		// Status/ProcessingError/ProcessedAt are mapped for symmetry with the other conversion
		// sites, but dbRepo.Update's SET clause does not persist them by design: this is the
		// generic metadata path, and the dedicated writers are UpdateStatus/CompleteProcessing.
		Status:          img.Status,
		ProcessingError: img.ProcessingError,
		ProcessedAt:     img.ProcessedAt,
		Width:           img.Width,
		Height:          img.Height,
		Metadata:        database.Metadata{},
	}

	// Convert domain metadata to database metadata if present
	if img.Metadata != nil {
		if err := json.Unmarshal(img.Metadata, &dbImage.Metadata); err != nil {
			return fmt.Errorf("failed to convert metadata: %w", err)
		}
	}

	if err := a.dbRepo.Update(ctx, dbImage); err != nil {
		return err
	}

	img.UpdatedAt = dbImage.UpdatedAt
	return nil
}

func (a *ImageRepositoryAdapter) UpdateStatus(ctx context.Context, id int, status string, processingError *string) error {
	if err := a.dbRepo.UpdateStatus(ctx, id, status, processingError); err != nil {
		return err
	}
	a.invalidateLists(ctx)
	return nil
}

func (a *ImageRepositoryAdapter) CompleteProcessing(ctx context.Context, id int, r image.ProcessingResult) error {
	if err := a.dbRepo.CompleteProcessing(ctx, id, database.ProcessingResult{
		ThumbnailPath: r.ThumbnailPath, Width: r.Width, Height: r.Height,
		Metadata: database.Metadata{"format": r.Format, "color_space": r.ColorSpace, "has_alpha": r.HasAlpha},
	}); err != nil {
		return err
	}
	a.invalidateLists(ctx)
	return nil
}

func (a *ImageRepositoryAdapter) Delete(ctx context.Context, id int) error {
	return a.dbRepo.Delete(ctx, id)
}

func (a *ImageRepositoryAdapter) GetByFilename(ctx context.Context, filename string) (*image.Image, error) {
	dbImage, err := a.dbRepo.GetByFilename(ctx, filename)
	if err != nil {
		return nil, err
	}

	return a.convertToBaseImage(dbImage), nil
}

func (a *ImageRepositoryAdapter) ExistsByFilename(ctx context.Context, filename string) (bool, error) {
	_, err := a.dbRepo.GetByFilename(ctx, filename)
	if err != nil {
		if err.Error() == fmt.Sprintf("image with filename %s not found", filename) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (a *ImageRepositoryAdapter) GetStats(ctx context.Context) (*image.ImageStats, error) {
	dbStats, err := a.dbRepo.GetStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get image stats: %w", err)
	}

	contentTypeCounts, err := a.dbRepo.GetContentTypeCounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get content type counts: %w", err)
	}

	return &image.ImageStats{
		TotalImages:  int64(dbStats.TotalImages),
		TotalSize:    dbStats.TotalSize,
		AverageSize:  int64(dbStats.AverageSize),
		ContentTypes: contentTypeCounts,
	}, nil
}

func (a *ImageRepositoryAdapter) CountByTag(ctx context.Context, tagName string) (int, error) {
	// This is a simplified implementation. In a real system, you'd want a dedicated count query
	// For now, we'll use a reasonable limit and count the results
	pagination := database.PaginationParams{
		Limit:  1000, // Reasonable limit for counting
		Offset: 0,
	}

	tags := []string{tagName}
	dbImages, err := a.dbRepo.GetByTags(ctx, tags, false, pagination)
	if err != nil {
		return 0, err
	}

	return len(dbImages), nil
}

// Helper method to convert database Image to domain Image
func (a *ImageRepositoryAdapter) convertToBaseImage(dbImg *database.Image) *image.Image {
	img := &image.Image{
		ID:               dbImg.ID,
		Filename:         dbImg.Filename,
		OriginalFilename: dbImg.OriginalFilename,
		ContentType:      dbImg.ContentType,
		FileSize:         dbImg.FileSize,
		StoragePath:      dbImg.StoragePath,
		ThumbnailPath:    dbImg.ThumbnailPath,
		Status:           dbImg.Status,
		ProcessingError:  dbImg.ProcessingError,
		ProcessedAt:      dbImg.ProcessedAt,
		Width:            dbImg.Width,
		Height:           dbImg.Height,
		UploadedAt:       dbImg.UploadedAt,
		CreatedAt:        dbImg.CreatedAt,
		UpdatedAt:        dbImg.UpdatedAt,
	}

	// Convert database metadata to domain metadata
	if len(dbImg.Metadata) > 0 {
		metadataBytes, err := json.Marshal(dbImg.Metadata)
		if err == nil {
			img.Metadata = metadataBytes
		}
	}

	// Convert database tags to domain tags
	if len(dbImg.Tags) > 0 {
		domainTags := make([]image.Tag, len(dbImg.Tags))
		for i, dbTag := range dbImg.Tags {
			domainTags[i] = image.Tag{
				ID:        dbTag.ID,
				Name:      dbTag.Name,
				CreatedAt: dbTag.CreatedAt,
			}
		}
		img.Tags = domainTags
	}

	return img
}
