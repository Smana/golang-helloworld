package image

import (
	"context"
	"io"
)

// Repository defines the interface for image data persistence
type Repository interface {
	// Create stores a new image in the repository
	Create(ctx context.Context, image *Image) error

	// GetByID retrieves an image by its ID
	GetByID(ctx context.Context, id int) (*Image, error)

	// List retrieves images based on the provided criteria
	List(ctx context.Context, req *ListImagesRequest) (*ListImagesResponse, error)

	// Update modifies an existing image
	Update(ctx context.Context, image *Image) error

	// UpdateStatus sets the processing status (and error text when failed).
	UpdateStatus(ctx context.Context, id int, status string, processingError *string) error

	// CompleteProcessing stores the worker's result and marks the image ready.
	CompleteProcessing(ctx context.Context, id int, res ProcessingResult) error

	// Delete removes an image from the repository
	Delete(ctx context.Context, id int) error

	// GetByFilename retrieves an image by its filename
	GetByFilename(ctx context.Context, filename string) (*Image, error)

	// ExistsByFilename checks if an image with the given filename exists
	ExistsByFilename(ctx context.Context, filename string) (bool, error)

	// CountByTag returns the number of images with a specific tag
	CountByTag(ctx context.Context, tagName string) (int, error)

	// GetStats returns aggregate statistics about all images
	GetStats(ctx context.Context) (*ImageStats, error)
}

// TagRepository defines the interface for tag data persistence
type TagRepository interface {
	// Create stores a new tag in the repository
	Create(ctx context.Context, tag *Tag) error

	// GetByID retrieves a tag by its ID
	GetByID(ctx context.Context, id int) (*Tag, error)

	// GetByName retrieves a tag by its name
	GetByName(ctx context.Context, name string) (*Tag, error)

	// List retrieves all tags or tags matching criteria
	List(ctx context.Context, limit, offset int) ([]*Tag, error)

	// Delete removes a tag from the repository
	Delete(ctx context.Context, id int) error

	// GetOrCreate gets an existing tag or creates a new one
	GetOrCreate(ctx context.Context, name string) (*Tag, error)

	// GetPopularTags returns the most frequently used tags
	GetPopularTags(ctx context.Context, limit int) ([]*Tag, error)

	// GetPredefinedTags returns all predefined tags
	GetPredefinedTags(ctx context.Context) ([]*Tag, error)

	// ExistsByName checks if a tag with the given name exists
	ExistsByName(ctx context.Context, name string) (bool, error)
}

// StorageService defines the interface for file storage operations
type StorageService interface {
	// Store saves a file and returns the storage path
	// size is the known file size in bytes - passing this prevents MinIO SDK from buffering entire file
	Store(ctx context.Context, filename string, contentType string, data io.Reader, size int64) (string, error)

	// Retrieve gets a file from storage
	Retrieve(ctx context.Context, path string) (io.ReadCloser, error)

	// Delete removes a file from storage
	Delete(ctx context.Context, path string) error

	// Exists checks if a file exists in storage
	Exists(ctx context.Context, path string) (bool, error)

	// StoreAt saves data under an exact path (used for derived objects such as thumbnails).
	StoreAt(ctx context.Context, path string, contentType string, data io.Reader, size int64) error

	// GetFileInfo returns metadata about a stored file
	GetFileInfo(ctx context.Context, path string) (*FileInfo, error)
}

// FileInfo represents metadata about a stored file
type FileInfo struct {
	Path         string
	Size         int64
	ContentType  string
	LastModified int64
	ETag         string
}

// ImageProcessor defines the interface for image processing operations
type ImageProcessor interface {
	// GenerateThumbnail creates a thumbnail for an image
	GenerateThumbnail(ctx context.Context, data io.Reader, maxWidth, maxHeight int) (io.Reader, error)

	// GetImageInfo extracts metadata from an image
	GetImageInfo(ctx context.Context, data io.Reader) (*ImageInfo, error)

	// Resize resizes an image to specified dimensions
	Resize(ctx context.Context, data io.Reader, width, height int) (io.Reader, error)

	// ValidateImage checks if the provided data is a valid image
	ValidateImage(ctx context.Context, data io.Reader, contentType string) error

	// OptimizeImage compresses and optimizes an image
	OptimizeImage(ctx context.Context, data io.Reader, quality int) (io.Reader, error)
}

// ImageInfo represents metadata extracted from an image
type ImageInfo struct {
	Width       int
	Height      int
	Format      string
	ColorSpace  string
	HasAlpha    bool
	Orientation int
}

// JobPublisher hands an uploaded image to the asynchronous worker.
type JobPublisher interface {
	PublishProcessImage(ctx context.Context, imageID int, objectKey string) error
}

// EventPublisher defines the interface for publishing domain events
type EventPublisher interface {
	// PublishImageCreated publishes an event when an image is created
	PublishImageCreated(ctx context.Context, image *Image) error

	// PublishImageDeleted publishes an event when an image is deleted
	PublishImageDeleted(ctx context.Context, imageID int) error

	// PublishImageUpdated publishes an event when an image is updated
	PublishImageUpdated(ctx context.Context, image *Image) error

	// PublishTagCreated publishes an event when a tag is created
	PublishTagCreated(ctx context.Context, tag *Tag) error
}

// ImageService defines the high-level business operations for images
type ImageService interface {
	// CreateImage handles the complete image creation process
	CreateImage(ctx context.Context, req *CreateImageRequest, data io.Reader) (*Image, error)

	// GetImage retrieves an image by ID with all related data
	GetImage(ctx context.Context, id int) (*Image, error)

	// ListImages retrieves images based on criteria
	ListImages(ctx context.Context, req *ListImagesRequest) (*ListImagesResponse, error)

	// UpdateImage modifies an existing image
	UpdateImage(ctx context.Context, id int, req *UpdateImageRequest) (*Image, error)

	// DeleteImage removes an image and its associated files
	DeleteImage(ctx context.Context, id int) error

	// DownloadImage provides access to the original image file
	DownloadImage(ctx context.Context, id int) (io.ReadCloser, string, error)

	// GetImageStats returns statistics about images
	GetImageStats(ctx context.Context) (*ImageStats, error)
}

// TagService defines the high-level business operations for tags
type TagService interface {
	// CreateTag creates a new tag
	CreateTag(ctx context.Context, name string) (*Tag, error)

	// GetTag retrieves a tag by ID
	GetTag(ctx context.Context, id int) (*Tag, error)

	// ListTags retrieves tags with pagination
	ListTags(ctx context.Context, limit, offset int) ([]*Tag, error)

	// GetPopularTags returns frequently used tags
	GetPopularTags(ctx context.Context, limit int) ([]*Tag, error)

	// GetPredefinedTags returns all predefined tags
	GetPredefinedTags(ctx context.Context) ([]*Tag, error)

	// DeleteTag removes a tag
	DeleteTag(ctx context.Context, id int) error

	// GetTagStats returns statistics about tags
	GetTagStats(ctx context.Context) (*TagStats, error)
}

// ImageStats represents statistics about images in the system
type ImageStats struct {
	TotalImages    int64
	TotalSize      int64
	AverageSize    int64
	MostUsedTags   []string
	ContentTypes   map[string]int64
	SizeCategories map[string]int64
	ImagesPerMonth map[string]int64
}

// TagStats represents statistics about tags in the system
type TagStats struct {
	TotalTags           int64
	AverageTagsPerImage float64
	MostUsedTags        []*TagUsage
	UnusedTags          []*Tag
}

// TagUsage represents usage statistics for a tag
type TagUsage struct {
	Tag   *Tag
	Count int64
}

// ValidationService defines the interface for business rule validation
type ValidationService interface {
	// ValidateImageUpload validates an image upload request
	ValidateImageUpload(ctx context.Context, req *CreateImageRequest) error

	// ValidateImageUpdate validates an image update request
	ValidateImageUpdate(ctx context.Context, id int, req *UpdateImageRequest) error

	// ValidateImageDeletion validates if an image can be deleted
	ValidateImageDeletion(ctx context.Context, id int) error

	// ValidateTagOperation validates tag operations
	ValidateTagOperation(ctx context.Context, operation string, tagID int) error
}

// CacheService defines the interface for caching operations
type CacheService interface {
	// GetImage retrieves a cached image
	GetImage(ctx context.Context, id int) (*Image, error)

	// SetImage caches an image
	SetImage(ctx context.Context, image *Image, expiry int64) error

	// DeleteImage removes an image from cache
	DeleteImage(ctx context.Context, id int) error

	// GetImageList retrieves a cached image list
	GetImageList(ctx context.Context, key string) (*ListImagesResponse, error)

	// SetImageList caches an image list
	SetImageList(ctx context.Context, key string, response *ListImagesResponse, expiry int64) error

	// InvalidateImageLists clears cached image lists
	InvalidateImageLists(ctx context.Context) error

	// GetStats retrieves cached statistics
	GetStats(ctx context.Context, key string) (interface{}, error)

	// SetStats caches statistics
	SetStats(ctx context.Context, key string, stats interface{}, expiry int64) error
}

// SearchService defines the interface for search operations
type SearchService interface {
	// IndexImage adds or updates an image in the search index
	IndexImage(ctx context.Context, image *Image) error

	// RemoveImage removes an image from the search index
	RemoveImage(ctx context.Context, imageID int) error

	// SearchImages performs a full-text search on images
	SearchImages(ctx context.Context, query string, limit, offset int) ([]*Image, int, error)

	// SearchByTags searches images by tag names
	SearchByTags(ctx context.Context, tags []string, limit, offset int) ([]*Image, int, error)

	// SuggestTags provides tag suggestions based on partial input
	SuggestTags(ctx context.Context, partial string, limit int) ([]string, error)

	// GetSimilarImages finds images similar to a given image
	GetSimilarImages(ctx context.Context, imageID int, limit int) ([]*Image, error)
}

// AuditService defines the interface for audit logging
type AuditService interface {
	// LogImageOperation logs an operation performed on an image
	LogImageOperation(ctx context.Context, userID string, operation string, imageID int, details map[string]interface{}) error

	// LogTagOperation logs an operation performed on a tag
	LogTagOperation(ctx context.Context, userID string, operation string, tagID int, details map[string]interface{}) error

	// GetAuditLogs retrieves audit logs for a specific resource
	GetAuditLogs(ctx context.Context, resourceType string, resourceID int, limit, offset int) ([]*AuditLog, error)

	// GetUserActivity retrieves activity logs for a specific user
	GetUserActivity(ctx context.Context, userID string, limit, offset int) ([]*AuditLog, error)
}

// AuditLog represents an audit log entry
type AuditLog struct {
	ID           int64
	UserID       string
	Operation    string
	ResourceType string
	ResourceID   int
	Details      map[string]interface{}
	Timestamp    int64
	IPAddress    string
	UserAgent    string
}

// NotificationService defines the interface for sending notifications
type NotificationService interface {
	// NotifyImageUploaded sends notification when an image is uploaded
	NotifyImageUploaded(ctx context.Context, image *Image, userID string) error

	// NotifyImageDeleted sends notification when an image is deleted
	NotifyImageDeleted(ctx context.Context, imageID int, userID string) error

	// NotifyStorageQuotaReached sends notification when storage quota is reached
	NotifyStorageQuotaReached(ctx context.Context, userID string, currentUsage, quota int64) error

	// NotifySystemMaintenance sends maintenance notifications
	NotifySystemMaintenance(ctx context.Context, message string, scheduledTime int64) error
}
