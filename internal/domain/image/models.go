package image

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// Image represents an image in the system with validation and business logic
type Image struct {
	ID               int             `json:"id" db:"id"`
	Filename         string          `json:"filename" db:"filename"`
	OriginalFilename string          `json:"original_filename" db:"original_filename"`
	ContentType      string          `json:"content_type" db:"content_type"`
	FileSize         int64           `json:"file_size" db:"file_size"`
	StoragePath      string          `json:"storage_path" db:"storage_path"`
	ThumbnailPath    *string         `json:"thumbnail_path" db:"thumbnail_path"`
	Status           string          `json:"status" db:"status"`
	ProcessingError  *string         `json:"processing_error,omitempty" db:"processing_error"`
	ProcessedAt      *time.Time      `json:"processed_at,omitempty" db:"processed_at"`
	Width            *int            `json:"width" db:"width"`
	Height           *int            `json:"height" db:"height"`
	UploadedAt       time.Time       `json:"uploaded_at" db:"uploaded_at"`
	Metadata         json.RawMessage `json:"metadata" db:"metadata"`
	CreatedAt        time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at" db:"updated_at"`
	Tags             []Tag           `json:"tags,omitempty"`
}

// Processing status values for Image.Status.
const (
	StatusPending    = "pending"
	StatusProcessing = "processing"
	StatusReady      = "ready"
	StatusFailed     = "failed"
)

// ProcessingResult is the worker's output for one image.
type ProcessingResult struct {
	ThumbnailPath string
	Width, Height int
	Format        string
	ColorSpace    string
	HasAlpha      bool
}

// Tag represents a tag that can be associated with images
type Tag struct {
	ID        int       `json:"id" db:"id"`
	Name      string    `json:"name" db:"name"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// CreateImageRequest represents a request to create a new image
type CreateImageRequest struct {
	OriginalFilename string          `json:"original_filename" validate:"required,max=255"`
	ContentType      string          `json:"content_type" validate:"required"`
	FileSize         int64           `json:"file_size" validate:"required,min=1"`
	Width            *int            `json:"width" validate:"omitempty,min=1,max=50000"`
	Height           *int            `json:"height" validate:"omitempty,min=1,max=50000"`
	Metadata         json.RawMessage `json:"metadata,omitempty"`
	Tags             []string        `json:"tags,omitempty" validate:"max=20,dive,min=1,max=100"`
}

// UpdateImageRequest represents a request to update an image
type UpdateImageRequest struct {
	Tags []string `json:"tags" validate:"max=20,dive,min=1,max=100"`
}

// ListImagesRequest represents a request to list images
type ListImagesRequest struct {
	Page     int      `json:"page" form:"page" validate:"min=1"`
	PageSize int      `json:"page_size" form:"page_size" validate:"min=1,max=100"`
	Tag      string   `json:"tag" form:"tag" validate:"omitempty,max=100"` // Single tag filter (deprecated, use Tags)
	Tags     []string `json:"tags" form:"tags"`                            // Multiple tag filters
	MatchAll bool     `json:"match_all" form:"match_all"`                  // true = AND logic, false = OR logic (default)
}

// ListImagesResponse represents the response for listing images
type ListImagesResponse struct {
	Images     []Image `json:"images"`
	TotalCount int     `json:"total_count"`
	Page       int     `json:"page"`
	PageSize   int     `json:"page_size"`
	TotalPages int     `json:"total_pages"`
}

// Domain errors
var (
	ErrInvalidImageData   = errors.New("invalid image data")
	ErrInvalidContentType = errors.New("invalid content type")
	ErrInvalidFileSize    = errors.New("invalid file size")
	ErrInvalidFilename    = errors.New("invalid filename")
	ErrInvalidDimensions  = errors.New("invalid image dimensions")
	ErrInvalidTagName     = errors.New("invalid tag name")
	ErrInvalidPagination  = errors.New("invalid pagination parameters")
	ErrImageNotFound      = errors.New("image not found")
	ErrTagNotFound        = errors.New("tag not found")
	ErrDuplicateTag       = errors.New("duplicate tag")
	ErrCacheUnavailable   = errors.New("cache service unavailable")
)

// Constants for validation
const (
	MaxFileSize     = 50 * 1024 * 1024 // 50MB
	MinFileSize     = 1                // 1 byte
	MaxFilenameLen  = 255
	MaxTagNameLen   = 100
	MinTagNameLen   = 1
	MaxTagsPerImage = 20
	DefaultPageSize = 20
	MaxPageSize     = 100
	MinPageSize     = 1
	MaxImageWidth   = 50000
	MaxImageHeight  = 50000
	MinImageWidth   = 1
	MinImageHeight  = 1
)

// Supported content types
var SupportedContentTypes = map[string]bool{
	"image/jpeg": true,
	"image/jpg":  true,
	"image/png":  true,
	"image/gif":  true,
	"image/webp": true,
}

// Business logic methods for Image

// Validate validates the image data
func (i *Image) Validate() error {
	if err := i.validateFilename(); err != nil {
		return err
	}
	if err := i.validateContentType(); err != nil {
		return err
	}
	if err := i.validateFileSize(); err != nil {
		return err
	}
	if err := i.validateDimensions(); err != nil {
		return err
	}
	if err := i.validateTags(); err != nil {
		return err
	}
	return nil
}

func (i *Image) validateFilename() error {
	if i.Filename == "" || i.OriginalFilename == "" {
		return fmt.Errorf("%w: filename cannot be empty", ErrInvalidFilename)
	}
	if len(i.Filename) > MaxFilenameLen || len(i.OriginalFilename) > MaxFilenameLen {
		return fmt.Errorf("%w: filename too long (max %d characters)", ErrInvalidFilename, MaxFilenameLen)
	}
	if !utf8.ValidString(i.Filename) || !utf8.ValidString(i.OriginalFilename) {
		return fmt.Errorf("%w: filename contains invalid UTF-8", ErrInvalidFilename)
	}
	return nil
}

func (i *Image) validateContentType() error {
	if i.ContentType == "" {
		return fmt.Errorf("%w: content type cannot be empty", ErrInvalidContentType)
	}
	if !SupportedContentTypes[i.ContentType] {
		return fmt.Errorf("%w: unsupported content type %s", ErrInvalidContentType, i.ContentType)
	}
	return nil
}

func (i *Image) validateFileSize() error {
	if i.FileSize < MinFileSize {
		return fmt.Errorf("%w: file size too small (min %d bytes)", ErrInvalidFileSize, MinFileSize)
	}
	if i.FileSize > MaxFileSize {
		return fmt.Errorf("%w: file size too large (max %d bytes)", ErrInvalidFileSize, MaxFileSize)
	}
	return nil
}

func (i *Image) validateDimensions() error {
	if i.Width != nil {
		if *i.Width < MinImageWidth || *i.Width > MaxImageWidth {
			return fmt.Errorf("%w: width %d out of range (%d-%d)", ErrInvalidDimensions, *i.Width, MinImageWidth, MaxImageWidth)
		}
	}
	if i.Height != nil {
		if *i.Height < MinImageHeight || *i.Height > MaxImageHeight {
			return fmt.Errorf("%w: height %d out of range (%d-%d)", ErrInvalidDimensions, *i.Height, MinImageHeight, MaxImageHeight)
		}
	}
	return nil
}

func (i *Image) validateTags() error {
	if len(i.Tags) > MaxTagsPerImage {
		return fmt.Errorf("%w: too many tags (max %d)", ErrInvalidTagName, MaxTagsPerImage)
	}

	seen := make(map[string]bool)
	for _, tag := range i.Tags {
		if err := tag.Validate(); err != nil {
			return err
		}
		if seen[tag.Name] {
			return fmt.Errorf("%w: %s", ErrDuplicateTag, tag.Name)
		}
		seen[tag.Name] = true
	}
	return nil
}

// GetFileExtension returns the file extension from the original filename
func (i *Image) GetFileExtension() string {
	return strings.ToLower(filepath.Ext(i.OriginalFilename))
}

// IsImage returns true if the content type is a supported image format
func (i *Image) IsImage() bool {
	return SupportedContentTypes[i.ContentType]
}

// GetAspectRatio returns the aspect ratio of the image if dimensions are available
func (i *Image) GetAspectRatio() *float64 {
	if i.Width != nil && i.Height != nil && *i.Height != 0 {
		ratio := float64(*i.Width) / float64(*i.Height)
		return &ratio
	}
	return nil
}

// GetSizeCategory returns a category based on file size
func (i *Image) GetSizeCategory() string {
	switch {
	case i.FileSize < 100*1024: // < 100KB
		return "small"
	case i.FileSize < 1024*1024: // < 1MB
		return "medium"
	case i.FileSize < 10*1024*1024: // < 10MB
		return "large"
	default:
		return "xlarge"
	}
}

// HasTag returns true if the image has the specified tag
func (i *Image) HasTag(tagName string) bool {
	for _, tag := range i.Tags {
		if tag.Name == tagName {
			return true
		}
	}
	return false
}

// Business logic methods for Tag

// Validate validates the tag data
func (t *Tag) Validate() error {
	if err := t.validateTagName(); err != nil {
		return err
	}
	if err := t.validateTagLength(); err != nil {
		return err
	}
	if err := t.validateTagEncoding(); err != nil {
		return err
	}
	return t.validateTagCharacters()
}

// validateTagName checks if tag name is empty
func (t *Tag) validateTagName() error {
	if t.Name == "" {
		return fmt.Errorf("%w: tag name cannot be empty", ErrInvalidTagName)
	}
	return nil
}

// validateTagLength checks if tag name length is within limits
func (t *Tag) validateTagLength() error {
	if len(t.Name) < MinTagNameLen || len(t.Name) > MaxTagNameLen {
		return fmt.Errorf("%w: tag name length must be between %d and %d characters", ErrInvalidTagName, MinTagNameLen, MaxTagNameLen)
	}
	return nil
}

// validateTagEncoding checks if tag name contains valid UTF-8
func (t *Tag) validateTagEncoding() error {
	if !utf8.ValidString(t.Name) {
		return fmt.Errorf("%w: tag name contains invalid UTF-8", ErrInvalidTagName)
	}
	return nil
}

// validateTagCharacters checks if tag name contains only allowed characters
func (t *Tag) validateTagCharacters() error {
	// Tag names should be lowercase and contain only letters, numbers, and hyphens
	for _, r := range t.Name {
		if !isValidTagCharacter(r) {
			return fmt.Errorf("%w: tag name can only contain lowercase letters, numbers, and hyphens", ErrInvalidTagName)
		}
	}
	return nil
}

// isValidTagCharacter checks if a rune is a valid tag character
func isValidTagCharacter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-'
}

// NormalizeName normalizes the tag name (lowercase, trim spaces)
func (t *Tag) NormalizeName() {
	t.Name = strings.TrimSpace(strings.ToLower(t.Name))
}

// Business logic methods for CreateImageRequest

// Validate validates the create image request
func (r *CreateImageRequest) Validate() error {
	if err := r.validateFilename(); err != nil {
		return err
	}
	if err := r.validateContentType(); err != nil {
		return err
	}
	if err := r.validateFileSize(); err != nil {
		return err
	}
	if err := r.validateDimensions(); err != nil {
		return err
	}
	if err := r.validateTags(); err != nil {
		return err
	}
	if err := r.validateMetadata(); err != nil {
		return err
	}
	return nil
}

func (r *CreateImageRequest) validateFilename() error {
	if r.OriginalFilename == "" {
		return fmt.Errorf("%w: original filename cannot be empty", ErrInvalidFilename)
	}
	if len(r.OriginalFilename) > MaxFilenameLen {
		return fmt.Errorf("%w: filename too long (max %d characters)", ErrInvalidFilename, MaxFilenameLen)
	}
	if !utf8.ValidString(r.OriginalFilename) {
		return fmt.Errorf("%w: filename contains invalid UTF-8", ErrInvalidFilename)
	}
	return nil
}

func (r *CreateImageRequest) validateContentType() error {
	if r.ContentType == "" {
		return fmt.Errorf("%w: content type cannot be empty", ErrInvalidContentType)
	}
	if !SupportedContentTypes[r.ContentType] {
		return fmt.Errorf("%w: unsupported content type %s", ErrInvalidContentType, r.ContentType)
	}
	return nil
}

func (r *CreateImageRequest) validateFileSize() error {
	if r.FileSize < MinFileSize {
		return fmt.Errorf("%w: file size too small (min %d bytes)", ErrInvalidFileSize, MinFileSize)
	}
	if r.FileSize > MaxFileSize {
		return fmt.Errorf("%w: file size too large (max %d bytes)", ErrInvalidFileSize, MaxFileSize)
	}
	return nil
}

func (r *CreateImageRequest) validateDimensions() error {
	if r.Width != nil {
		if *r.Width < MinImageWidth || *r.Width > MaxImageWidth {
			return fmt.Errorf("%w: width %d out of range (%d-%d)", ErrInvalidDimensions, *r.Width, MinImageWidth, MaxImageWidth)
		}
	}
	if r.Height != nil {
		if *r.Height < MinImageHeight || *r.Height > MaxImageHeight {
			return fmt.Errorf("%w: height %d out of range (%d-%d)", ErrInvalidDimensions, *r.Height, MinImageHeight, MaxImageHeight)
		}
	}
	return nil
}

func (r *CreateImageRequest) validateTags() error {
	if len(r.Tags) > MaxTagsPerImage {
		return fmt.Errorf("%w: too many tags (max %d)", ErrInvalidTagName, MaxTagsPerImage)
	}

	seen := make(map[string]bool)
	for _, tagName := range r.Tags {
		normalized := strings.TrimSpace(strings.ToLower(tagName))
		if len(normalized) < MinTagNameLen || len(normalized) > MaxTagNameLen {
			return fmt.Errorf("%w: tag name length must be between %d and %d characters", ErrInvalidTagName, MinTagNameLen, MaxTagNameLen)
		}
		if seen[normalized] {
			return fmt.Errorf("%w: %s", ErrDuplicateTag, tagName)
		}
		seen[normalized] = true
	}
	return nil
}

func (r *CreateImageRequest) validateMetadata() error {
	if len(r.Metadata) > 0 {
		var temp interface{}
		if err := json.Unmarshal(r.Metadata, &temp); err != nil {
			return fmt.Errorf("%w: invalid JSON metadata: %v", ErrInvalidImageData, err)
		}
	}
	return nil
}

// GenerateFilename generates a unique filename based on the original filename
func (r *CreateImageRequest) GenerateFilename() string {
	ext := filepath.Ext(r.OriginalFilename)
	base := strings.TrimSuffix(filepath.Base(r.OriginalFilename), ext)
	// In a real system, you'd add timestamp or UUID to ensure uniqueness
	return fmt.Sprintf("%s_%d%s", base, time.Now().Unix(), ext)
}

// GetMimeType returns the MIME type based on content type
func (r *CreateImageRequest) GetMimeType() (string, error) {
	if !SupportedContentTypes[r.ContentType] {
		return "", fmt.Errorf("%w: %s", ErrInvalidContentType, r.ContentType)
	}
	return r.ContentType, nil
}

// Business logic methods for ListImagesRequest

// Validate validates the list images request
func (r *ListImagesRequest) Validate() error {
	if r.Page < 1 {
		return fmt.Errorf("%w: page must be >= 1", ErrInvalidPagination)
	}
	if r.PageSize < MinPageSize || r.PageSize > MaxPageSize {
		return fmt.Errorf("%w: page size must be between %d and %d", ErrInvalidPagination, MinPageSize, MaxPageSize)
	}
	if r.Tag != "" {
		if len(r.Tag) > MaxTagNameLen {
			return fmt.Errorf("%w: tag name too long (max %d characters)", ErrInvalidTagName, MaxTagNameLen)
		}
		if !utf8.ValidString(r.Tag) {
			return fmt.Errorf("%w: tag name contains invalid UTF-8", ErrInvalidTagName)
		}
	}
	return nil
}

// SetDefaults sets default values for the request
func (r *ListImagesRequest) SetDefaults() {
	if r.Page == 0 {
		r.Page = 1
	}
	if r.PageSize == 0 {
		r.PageSize = DefaultPageSize
	}
}

// GetOffset returns the offset for database queries
func (r *ListImagesRequest) GetOffset() int {
	return (r.Page - 1) * r.PageSize
}

// Business logic methods for ListImagesResponse

// CalculateTotalPages calculates and sets the total pages based on total count and page size
func (r *ListImagesResponse) CalculateTotalPages() {
	if r.PageSize <= 0 {
		r.TotalPages = 0
		return
	}
	r.TotalPages = (r.TotalCount + r.PageSize - 1) / r.PageSize
}

// HasNextPage returns true if there are more pages
func (r *ListImagesResponse) HasNextPage() bool {
	return r.Page < r.TotalPages
}

// HasPrevPage returns true if there are previous pages
func (r *ListImagesResponse) HasPrevPage() bool {
	return r.Page > 1
}

// Helper functions

// NewTag creates a new tag with validation
func NewTag(name string) (*Tag, error) {
	tag := &Tag{
		Name:      strings.TrimSpace(strings.ToLower(name)),
		CreatedAt: time.Now(),
	}
	if err := tag.Validate(); err != nil {
		return nil, err
	}
	return tag, nil
}

// ValidateContentTypeFromExtension validates content type against file extension
func ValidateContentTypeFromExtension(contentType, filename string) error {
	ext := strings.ToLower(filepath.Ext(filename))
	expectedTypes := map[string][]string{
		".jpg":  {"image/jpeg", "image/jpg"},
		".jpeg": {"image/jpeg", "image/jpg"},
		".png":  {"image/png"},
		".gif":  {"image/gif"},
		".webp": {"image/webp"},
	}

	if expectedList, exists := expectedTypes[ext]; exists {
		for _, expected := range expectedList {
			if contentType == expected {
				return nil
			}
		}
		return fmt.Errorf("%w: content type %s doesn't match extension %s", ErrInvalidContentType, contentType, ext)
	}

	return fmt.Errorf("%w: unsupported file extension %s", ErrInvalidContentType, ext)
}
