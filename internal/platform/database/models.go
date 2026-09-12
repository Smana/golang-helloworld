package database

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// Image represents an image record in the database
type Image struct {
	ID               int        `json:"id" db:"id"`
	Filename         string     `json:"filename" db:"filename"`
	OriginalFilename string     `json:"original_filename" db:"original_filename"`
	ContentType      string     `json:"content_type" db:"content_type"`
	FileSize         int64      `json:"file_size" db:"file_size"`
	StoragePath      string     `json:"storage_path" db:"storage_path"`
	ThumbnailPath    *string    `json:"thumbnail_path,omitempty" db:"thumbnail_path"`
	Status           string     `json:"status" db:"status"`
	ProcessingError  *string    `json:"processing_error,omitempty" db:"processing_error"`
	ProcessedAt      *time.Time `json:"processed_at,omitempty" db:"processed_at"`
	Width            *int       `json:"width,omitempty" db:"width"`
	Height           *int       `json:"height,omitempty" db:"height"`
	UploadedAt       time.Time  `json:"uploaded_at" db:"uploaded_at"`
	Metadata         Metadata   `json:"metadata" db:"metadata"`
	CreatedAt        time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at" db:"updated_at"`
	Tags             []Tag      `json:"tags,omitempty" db:"-"` // Loaded separately
}

// ProcessingResult is what the worker writes back for one image.
type ProcessingResult struct {
	ThumbnailPath string
	Width, Height int
	Metadata      Metadata // merged into images.metadata (jsonb ||)
}

// Tag represents a tag for categorizing images
type Tag struct {
	ID          int       `json:"id" db:"id"`
	Name        string    `json:"name" db:"name"`
	Description *string   `json:"description,omitempty" db:"description"`
	Color       *string   `json:"color,omitempty" db:"color"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	ImageCount  int       `json:"image_count,omitempty" db:"image_count"` // For aggregated queries
}

// Album represents a collection of images
type Album struct {
	ID               int       `json:"id" db:"id"`
	Name             string    `json:"name" db:"name"`
	Description      *string   `json:"description,omitempty" db:"description"`
	ThumbnailImageID *int      `json:"thumbnail_image_id,omitempty" db:"thumbnail_image_id"`
	IsPublic         bool      `json:"is_public" db:"is_public"`
	CreatedAt        time.Time `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time `json:"updated_at" db:"updated_at"`
	ImageCount       int       `json:"image_count,omitempty" db:"image_count"` // For aggregated queries
}

// ImageTag represents the many-to-many relationship between images and tags
type ImageTag struct {
	ImageID   int       `json:"image_id" db:"image_id"`
	TagID     int       `json:"tag_id" db:"tag_id"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// ImageAlbum represents the many-to-many relationship between images and albums
type ImageAlbum struct {
	ImageID   int       `json:"image_id" db:"image_id"`
	AlbumID   int       `json:"album_id" db:"album_id"`
	Position  int       `json:"position" db:"position"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// Metadata represents flexible metadata as JSON
type Metadata map[string]interface{}

// Value implements the driver.Valuer interface for storing to database
func (m Metadata) Value() (driver.Value, error) {
	if m == nil {
		return json.RawMessage("{}"), nil
	}
	return json.Marshal(m)
}

// Scan implements the sql.Scanner interface for loading from database
func (m *Metadata) Scan(value interface{}) error {
	if value == nil {
		*m = make(Metadata)
		return nil
	}

	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return fmt.Errorf("cannot scan %T into Metadata", value)
	}

	return json.Unmarshal(bytes, m)
}

// ImageStats represents aggregate statistics about images
type ImageStats struct {
	TotalImages  int     `json:"total_images" db:"total_images"`
	ContentTypes int     `json:"content_types" db:"content_types"`
	TotalSize    int64   `json:"total_size" db:"total_size"`
	AverageSize  float64 `json:"average_size" db:"avg_size"`
	MaxSize      int64   `json:"max_size" db:"max_size"`
	MinSize      int64   `json:"min_size" db:"min_size"`
}

// SearchFilters represents filters for image searches
type SearchFilters struct {
	ContentTypes []string   `json:"content_types,omitempty"`
	Tags         []string   `json:"tags,omitempty"`
	Albums       []int      `json:"albums,omitempty"`
	MinSize      *int64     `json:"min_size,omitempty"`
	MaxSize      *int64     `json:"max_size,omitempty"`
	StartDate    *time.Time `json:"start_date,omitempty"`
	EndDate      *time.Time `json:"end_date,omitempty"`
	Filename     string     `json:"filename,omitempty"`
}

// PaginationParams represents pagination parameters
type PaginationParams struct {
	Offset int `json:"offset"`
	Limit  int `json:"limit"`
}

// DefaultPagination returns sensible default pagination
func DefaultPagination() PaginationParams {
	return PaginationParams{
		Offset: 0,
		Limit:  50,
	}
}

// Validate ensures pagination parameters are sensible
func (p *PaginationParams) Validate() {
	// Hard limit of 50 images per page to prevent memory exhaustion
	// Under high concurrency (15+ requests), larger limits cause OOMKills
	if p.Limit <= 0 || p.Limit > 50 {
		p.Limit = 50
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
}

// SortOrder represents sort direction
type SortOrder string

const (
	SortAsc  SortOrder = "ASC"
	SortDesc SortOrder = "DESC"
)

// ImageSortField represents fields images can be sorted by
type ImageSortField string

const (
	SortByUploadedAt ImageSortField = "uploaded_at"
	SortByFilename   ImageSortField = "filename"
	SortByFileSize   ImageSortField = "file_size"
	SortByCreatedAt  ImageSortField = "created_at"
)

// SortParams represents sorting parameters
type SortParams struct {
	Field ImageSortField `json:"field"`
	Order SortOrder      `json:"order"`
}

// DefaultSort returns default sorting (newest first)
func DefaultSort() SortParams {
	return SortParams{
		Field: SortByUploadedAt,
		Order: SortDesc,
	}
}
