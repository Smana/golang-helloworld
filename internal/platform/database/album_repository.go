package database

import (
	"context"
	"database/sql"
	"fmt"
)

// albumRepository implements AlbumRepository interface
type albumRepository struct {
	db *sql.DB
}

// NewAlbumRepository creates a new AlbumRepository
func NewAlbumRepository(db *sql.DB) AlbumRepository {
	return &albumRepository{db: db}
}

// Create inserts a new album record
func (r *albumRepository) Create(ctx context.Context, album *Album) error {
	query := `
		INSERT INTO albums (name, description, thumbnail_image_id, is_public)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at, updated_at
	`

	err := r.db.QueryRowContext(
		ctx, query,
		album.Name,
		album.Description,
		album.ThumbnailImageID,
		album.IsPublic,
	).Scan(&album.ID, &album.CreatedAt, &album.UpdatedAt)

	return err
}

// GetByID retrieves an album by its ID
func (r *albumRepository) GetByID(ctx context.Context, id int) (*Album, error) {
	query := `
		SELECT id, name, description, thumbnail_image_id, is_public, created_at, updated_at
		FROM albums WHERE id = $1
	`

	album := &Album{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&album.ID,
		&album.Name,
		&album.Description,
		&album.ThumbnailImageID,
		&album.IsPublic,
		&album.CreatedAt,
		&album.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("album with ID %d not found", id)
	}

	return album, err
}

// GetByName retrieves an album by its name
func (r *albumRepository) GetByName(ctx context.Context, name string) (*Album, error) {
	query := `
		SELECT id, name, description, thumbnail_image_id, is_public, created_at, updated_at
		FROM albums WHERE name = $1
	`

	album := &Album{}
	err := r.db.QueryRowContext(ctx, query, name).Scan(
		&album.ID,
		&album.Name,
		&album.Description,
		&album.ThumbnailImageID,
		&album.IsPublic,
		&album.CreatedAt,
		&album.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("album with name %s not found", name)
	}

	return album, err
}

// Update updates an existing album record
func (r *albumRepository) Update(ctx context.Context, album *Album) error {
	query := `
		UPDATE albums SET
			name = $2,
			description = $3,
			thumbnail_image_id = $4,
			is_public = $5,
			updated_at = NOW()
		WHERE id = $1
		RETURNING updated_at
	`

	err := r.db.QueryRowContext(
		ctx, query,
		album.ID,
		album.Name,
		album.Description,
		album.ThumbnailImageID,
		album.IsPublic,
	).Scan(&album.UpdatedAt)

	return err
}

// Delete removes an album record by ID
func (r *albumRepository) Delete(ctx context.Context, id int) error {
	query := `DELETE FROM albums WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return fmt.Errorf("album with ID %d not found", id)
	}

	return nil
}

// List retrieves a paginated list of albums
func (r *albumRepository) List(ctx context.Context, pagination PaginationParams) ([]*Album, error) {
	pagination.Validate()

	query := `
		SELECT id, name, description, thumbnail_image_id, is_public, created_at, updated_at
		FROM albums
		ORDER BY name ASC
		LIMIT $1 OFFSET $2
	`

	return r.scanAlbums(ctx, query, pagination.Limit, pagination.Offset)
}

// ListPublic retrieves a paginated list of public albums
func (r *albumRepository) ListPublic(ctx context.Context, pagination PaginationParams) ([]*Album, error) {
	pagination.Validate()

	query := `
		SELECT id, name, description, thumbnail_image_id, is_public, created_at, updated_at
		FROM albums
		WHERE is_public = true
		ORDER BY name ASC
		LIMIT $1 OFFSET $2
	`

	return r.scanAlbums(ctx, query, pagination.Limit, pagination.Offset)
}

// Search searches for albums by name
func (r *albumRepository) Search(ctx context.Context, query string, pagination PaginationParams) ([]*Album, error) {
	pagination.Validate()

	sqlQuery := `
		SELECT id, name, description, thumbnail_image_id, is_public, created_at, updated_at
		FROM albums
		WHERE name ILIKE $1 OR description ILIKE $1
		ORDER BY name ASC
		LIMIT $2 OFFSET $3
	`

	searchTerm := "%" + query + "%"
	return r.scanAlbums(ctx, sqlQuery, searchTerm, pagination.Limit, pagination.Offset)
}

// GetWithImageCount retrieves albums with their image counts
func (r *albumRepository) GetWithImageCount(ctx context.Context, pagination PaginationParams) ([]*Album, error) {
	pagination.Validate()

	query := `
		SELECT a.id, a.name, a.description, a.thumbnail_image_id, a.is_public, 
			   a.created_at, a.updated_at, COUNT(ia.image_id) as image_count
		FROM albums a
		LEFT JOIN image_albums ia ON a.id = ia.album_id
		GROUP BY a.id, a.name, a.description, a.thumbnail_image_id, a.is_public, a.created_at, a.updated_at
		ORDER BY a.name ASC
		LIMIT $1 OFFSET $2
	`

	rows, err := r.db.QueryContext(ctx, query, pagination.Limit, pagination.Offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }() //nolint:errcheck // Resource cleanup

	var albums []*Album
	for rows.Next() {
		album := &Album{}
		err := rows.Scan(
			&album.ID,
			&album.Name,
			&album.Description,
			&album.ThumbnailImageID,
			&album.IsPublic,
			&album.CreatedAt,
			&album.UpdatedAt,
			&album.ImageCount,
		)
		if err != nil {
			return nil, err
		}
		albums = append(albums, album)
	}

	return albums, rows.Err()
}

// Count returns the total number of albums
func (r *albumRepository) Count(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM albums").Scan(&count)
	return count, err
}

// CountPublic returns the number of public albums
func (r *albumRepository) CountPublic(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM albums WHERE is_public = true").Scan(&count)
	return count, err
}

// AddImage adds an image to an album
func (r *albumRepository) AddImage(ctx context.Context, albumID, imageID int, position int) error {
	query := `
		INSERT INTO image_albums (album_id, image_id, position)
		VALUES ($1, $2, $3)
		ON CONFLICT (album_id, image_id) 
		DO UPDATE SET position = $3
	`

	_, err := r.db.ExecContext(ctx, query, albumID, imageID, position)
	return err
}

// RemoveImage removes an image from an album
func (r *albumRepository) RemoveImage(ctx context.Context, albumID, imageID int) error {
	query := `DELETE FROM image_albums WHERE album_id = $1 AND image_id = $2`

	_, err := r.db.ExecContext(ctx, query, albumID, imageID)
	return err
}

// RemoveAllImages removes all images from an album
func (r *albumRepository) RemoveAllImages(ctx context.Context, albumID int) error {
	query := `DELETE FROM image_albums WHERE album_id = $1`

	_, err := r.db.ExecContext(ctx, query, albumID)
	return err
}

// GetAlbumImages retrieves all images in an album
func (r *albumRepository) GetAlbumImages(ctx context.Context, albumID int, pagination PaginationParams) ([]*Image, error) {
	pagination.Validate()

	query := `
		SELECT i.id, i.filename, i.original_filename, i.content_type, i.file_size,
			   i.storage_path, i.thumbnail_path, i.status, i.processing_error, i.processed_at,
			   i.width, i.height, i.uploaded_at,
			   i.metadata, i.created_at, i.updated_at
		FROM images i
		INNER JOIN image_albums ia ON i.id = ia.image_id
		WHERE ia.album_id = $1
		ORDER BY ia.position ASC, i.uploaded_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := r.db.QueryContext(ctx, query, albumID, pagination.Limit, pagination.Offset)
	if err != nil {
		return nil, err
	}

	return scanImages(ctx, rows)
}

// GetImageAlbums retrieves all albums containing a specific image
func (r *albumRepository) GetImageAlbums(ctx context.Context, imageID int) ([]*Album, error) {
	query := `
		SELECT a.id, a.name, a.description, a.thumbnail_image_id, a.is_public, a.created_at, a.updated_at
		FROM albums a
		INNER JOIN image_albums ia ON a.id = ia.album_id
		WHERE ia.image_id = $1
		ORDER BY a.name ASC
	`

	return r.scanAlbums(ctx, query, imageID)
}

// ReorderImages updates the position of multiple images in an album
func (r *albumRepository) ReorderImages(ctx context.Context, albumID int, imagePositions map[int]int) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() //nolint:errcheck // Transaction cleanup

	query := `UPDATE image_albums SET position = $3 WHERE album_id = $1 AND image_id = $2`

	for imageID, position := range imagePositions {
		_, err := tx.ExecContext(ctx, query, albumID, imageID, position)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// CountAlbumImages returns the number of images in an album
func (r *albumRepository) CountAlbumImages(ctx context.Context, albumID int) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM image_albums WHERE album_id = $1", albumID).Scan(&count)
	return count, err
}

// scanAlbums is a helper method to scan multiple album records
func (r *albumRepository) scanAlbums(ctx context.Context, query string, args ...interface{}) ([]*Album, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }() //nolint:errcheck // Resource cleanup

	var albums []*Album
	for rows.Next() {
		album := &Album{}
		err := rows.Scan(
			&album.ID,
			&album.Name,
			&album.Description,
			&album.ThumbnailImageID,
			&album.IsPublic,
			&album.CreatedAt,
			&album.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		albums = append(albums, album)
	}

	return albums, rows.Err()
}
