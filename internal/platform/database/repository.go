package database

import (
	"context"
	"database/sql"
	"time"
)

// ImageRepository defines the interface for image data access
type ImageRepository interface {
	// Basic CRUD operations
	Create(ctx context.Context, image *Image) error
	GetByID(ctx context.Context, id int) (*Image, error)
	GetByFilename(ctx context.Context, filename string) (*Image, error)
	GetByStoragePath(ctx context.Context, path string) (*Image, error)
	Update(ctx context.Context, image *Image) error
	UpdateThumbnail(ctx context.Context, id int, thumbnailPath string) error
	Delete(ctx context.Context, id int) error
	DeleteByStoragePath(ctx context.Context, path string) error

	// List and search operations
	List(ctx context.Context, pagination PaginationParams, sort SortParams) ([]*Image, error)
	ListByContentType(ctx context.Context, contentType string, pagination PaginationParams) ([]*Image, error)
	Search(ctx context.Context, filters SearchFilters, pagination PaginationParams, sort SortParams) ([]*Image, error)
	GetByDateRange(ctx context.Context, start, end time.Time, pagination PaginationParams) ([]*Image, error)
	GetRecent(ctx context.Context, since time.Time, limit int) ([]*Image, error)
	GetLargest(ctx context.Context, pagination PaginationParams) ([]*Image, error)

	// Statistics and counts
	Count(ctx context.Context) (int, error)
	CountByContentType(ctx context.Context, contentType string) (int, error)
	GetStats(ctx context.Context) (*ImageStats, error)
	GetContentTypeCounts(ctx context.Context) (map[string]int64, error)

	// Tag relationships
	GetWithTags(ctx context.Context, pagination PaginationParams, sort SortParams) ([]*Image, error)
	GetByTags(ctx context.Context, tags []string, matchAll bool, pagination PaginationParams) ([]*Image, error)
}

// TagRepository defines the interface for tag data access
type TagRepository interface {
	// Basic CRUD operations
	Create(ctx context.Context, tag *Tag) error
	GetByID(ctx context.Context, id int) (*Tag, error)
	GetByName(ctx context.Context, name string) (*Tag, error)
	Update(ctx context.Context, tag *Tag) error
	Delete(ctx context.Context, id int) error

	// List and search operations
	List(ctx context.Context, pagination PaginationParams) ([]*Tag, error)
	Search(ctx context.Context, query string, pagination PaginationParams) ([]*Tag, error)
	GetPopular(ctx context.Context, limit int) ([]*Tag, error)
	GetWithImageCount(ctx context.Context, pagination PaginationParams) ([]*Tag, error)

	// Predefined tags
	GetPredefined(ctx context.Context) ([]*Tag, error)
	GetPredefinedByCategory(ctx context.Context) (map[string][]*Tag, error)

	// Statistics
	Count(ctx context.Context) (int, error)

	// Image-tag relationships
	AddToImage(ctx context.Context, imageID, tagID int) error
	RemoveFromImage(ctx context.Context, imageID, tagID int) error
	RemoveAllFromImage(ctx context.Context, imageID int) error
	GetImageTags(ctx context.Context, imageID int) ([]*Tag, error)
	GetTagImages(ctx context.Context, tagID int, pagination PaginationParams) ([]*Image, error)
	CountImageTags(ctx context.Context, imageID int) (int, error)
	CountTagImages(ctx context.Context, tagID int) (int, error)
}

// AlbumRepository defines the interface for album data access
type AlbumRepository interface {
	// Basic CRUD operations
	Create(ctx context.Context, album *Album) error
	GetByID(ctx context.Context, id int) (*Album, error)
	GetByName(ctx context.Context, name string) (*Album, error)
	Update(ctx context.Context, album *Album) error
	Delete(ctx context.Context, id int) error

	// List and search operations
	List(ctx context.Context, pagination PaginationParams) ([]*Album, error)
	ListPublic(ctx context.Context, pagination PaginationParams) ([]*Album, error)
	Search(ctx context.Context, query string, pagination PaginationParams) ([]*Album, error)
	GetWithImageCount(ctx context.Context, pagination PaginationParams) ([]*Album, error)

	// Statistics
	Count(ctx context.Context) (int, error)
	CountPublic(ctx context.Context) (int, error)

	// Image-album relationships
	AddImage(ctx context.Context, albumID, imageID int, position int) error
	RemoveImage(ctx context.Context, albumID, imageID int) error
	RemoveAllImages(ctx context.Context, albumID int) error
	GetAlbumImages(ctx context.Context, albumID int, pagination PaginationParams) ([]*Image, error)
	GetImageAlbums(ctx context.Context, imageID int) ([]*Album, error)
	ReorderImages(ctx context.Context, albumID int, imagePositions map[int]int) error
	CountAlbumImages(ctx context.Context, albumID int) (int, error)
}

// Repositories aggregates all repository interfaces
type Repositories struct {
	Images ImageRepository
	Tags   TagRepository
	Albums AlbumRepository
}

// NewRepositories creates a new Repositories instance
func NewRepositories(imageRepo ImageRepository, tagRepo TagRepository, albumRepo AlbumRepository) *Repositories {
	return &Repositories{
		Images: imageRepo,
		Tags:   tagRepo,
		Albums: albumRepo,
	}
}

// scanImages is a shared helper function to scan multiple image records from database rows
func scanImages(ctx context.Context, rows *sql.Rows) ([]*Image, error) {
	defer func() { _ = rows.Close() }() //nolint:errcheck // Resource cleanup

	var images []*Image
	for rows.Next() {
		image := &Image{}
		err := rows.Scan(
			&image.ID,
			&image.Filename,
			&image.OriginalFilename,
			&image.ContentType,
			&image.FileSize,
			&image.StoragePath,
			&image.ThumbnailPath,
			&image.Width,
			&image.Height,
			&image.UploadedAt,
			&image.Metadata,
			&image.CreatedAt,
			&image.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		images = append(images, image)
	}

	return images, rows.Err()
}
