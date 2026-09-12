package database

import (
	"context"
	"database/sql"
	"fmt"
)

// tagRepository implements TagRepository interface
type tagRepository struct {
	db *sql.DB
}

// NewTagRepository creates a new TagRepository
func NewTagRepository(db *sql.DB) TagRepository {
	return &tagRepository{db: db}
}

// Create inserts a new tag record
func (r *tagRepository) Create(ctx context.Context, tag *Tag) error {
	query := `
		INSERT INTO tags (name, description, color)
		VALUES ($1, $2, $3)
		RETURNING id, created_at
	`

	err := r.db.QueryRowContext(
		ctx, query,
		tag.Name,
		tag.Description,
		tag.Color,
	).Scan(&tag.ID, &tag.CreatedAt)

	return err
}

// GetByID retrieves a tag by its ID
func (r *tagRepository) GetByID(ctx context.Context, id int) (*Tag, error) {
	query := `
		SELECT id, name, description, color, created_at
		FROM tags WHERE id = $1
	`

	tag := &Tag{}
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&tag.ID,
		&tag.Name,
		&tag.Description,
		&tag.Color,
		&tag.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("tag with ID %d not found", id)
	}

	return tag, err
}

// GetByName retrieves a tag by its name
func (r *tagRepository) GetByName(ctx context.Context, name string) (*Tag, error) {
	query := `
		SELECT id, name, description, color, created_at
		FROM tags WHERE name = $1
	`

	tag := &Tag{}
	err := r.db.QueryRowContext(ctx, query, name).Scan(
		&tag.ID,
		&tag.Name,
		&tag.Description,
		&tag.Color,
		&tag.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("tag with name %s not found", name)
	}

	return tag, err
}

// Update updates an existing tag record
func (r *tagRepository) Update(ctx context.Context, tag *Tag) error {
	query := `
		UPDATE tags SET
			name = $2,
			description = $3,
			color = $4
		WHERE id = $1
	`

	result, err := r.db.ExecContext(ctx, query, tag.ID, tag.Name, tag.Description, tag.Color)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return fmt.Errorf("tag with ID %d not found", tag.ID)
	}

	return nil
}

// Delete removes a tag record by ID
func (r *tagRepository) Delete(ctx context.Context, id int) error {
	query := `DELETE FROM tags WHERE id = $1`

	result, err := r.db.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return fmt.Errorf("tag with ID %d not found", id)
	}

	return nil
}

// List retrieves a paginated list of tags
func (r *tagRepository) List(ctx context.Context, pagination PaginationParams) ([]*Tag, error) {
	pagination.Validate()

	query := `
		SELECT id, name, description, color, created_at
		FROM tags
		ORDER BY name ASC
		LIMIT $1 OFFSET $2
	`

	return r.scanTags(ctx, query, pagination.Limit, pagination.Offset)
}

// Search searches for tags by name
func (r *tagRepository) Search(ctx context.Context, query string, pagination PaginationParams) ([]*Tag, error) {
	pagination.Validate()

	sqlQuery := `
		SELECT id, name, description, color, created_at
		FROM tags
		WHERE name ILIKE $1
		ORDER BY name ASC
		LIMIT $2 OFFSET $3
	`

	searchTerm := "%" + query + "%"
	return r.scanTags(ctx, sqlQuery, searchTerm, pagination.Limit, pagination.Offset)
}

// GetPopular retrieves the most popular tags by image count
func (r *tagRepository) GetPopular(ctx context.Context, limit int) ([]*Tag, error) {
	if limit <= 0 || limit > 1000 {
		limit = 50
	}

	query := `
		SELECT t.id, t.name, t.description, t.color, t.created_at, COUNT(it.image_id) as image_count
		FROM tags t
		LEFT JOIN image_tags it ON t.id = it.tag_id
		GROUP BY t.id, t.name, t.description, t.color, t.created_at
		ORDER BY image_count DESC, t.name ASC
		LIMIT $1
	`

	rows, err := r.db.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }() //nolint:errcheck // Resource cleanup

	var tags []*Tag
	for rows.Next() {
		tag := &Tag{}
		err := rows.Scan(
			&tag.ID,
			&tag.Name,
			&tag.Description,
			&tag.Color,
			&tag.CreatedAt,
			&tag.ImageCount,
		)
		if err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}

	return tags, rows.Err()
}

// GetWithImageCount retrieves tags with their image counts
func (r *tagRepository) GetWithImageCount(ctx context.Context, pagination PaginationParams) ([]*Tag, error) {
	pagination.Validate()

	query := `
		SELECT t.id, t.name, t.description, t.color, t.created_at, COUNT(it.image_id) as image_count
		FROM tags t
		LEFT JOIN image_tags it ON t.id = it.tag_id
		GROUP BY t.id, t.name, t.description, t.color, t.created_at
		ORDER BY t.name ASC
		LIMIT $1 OFFSET $2
	`

	rows, err := r.db.QueryContext(ctx, query, pagination.Limit, pagination.Offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }() //nolint:errcheck // Resource cleanup

	var tags []*Tag
	for rows.Next() {
		tag := &Tag{}
		err := rows.Scan(
			&tag.ID,
			&tag.Name,
			&tag.Description,
			&tag.Color,
			&tag.CreatedAt,
			&tag.ImageCount,
		)
		if err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}

	return tags, rows.Err()
}

// Count returns the total number of tags
func (r *tagRepository) Count(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM tags").Scan(&count)
	return count, err
}

// AddToImage adds a tag to an image
func (r *tagRepository) AddToImage(ctx context.Context, imageID, tagID int) error {
	query := `
		INSERT INTO image_tags (image_id, tag_id)
		VALUES ($1, $2)
		ON CONFLICT (image_id, tag_id) DO NOTHING
	`

	_, err := r.db.ExecContext(ctx, query, imageID, tagID)
	return err
}

// RemoveFromImage removes a tag from an image
func (r *tagRepository) RemoveFromImage(ctx context.Context, imageID, tagID int) error {
	query := `DELETE FROM image_tags WHERE image_id = $1 AND tag_id = $2`

	_, err := r.db.ExecContext(ctx, query, imageID, tagID)
	return err
}

// RemoveAllFromImage removes all tags from an image
func (r *tagRepository) RemoveAllFromImage(ctx context.Context, imageID int) error {
	query := `DELETE FROM image_tags WHERE image_id = $1`

	_, err := r.db.ExecContext(ctx, query, imageID)
	return err
}

// GetImageTags retrieves all tags for a specific image
func (r *tagRepository) GetImageTags(ctx context.Context, imageID int) ([]*Tag, error) {
	query := `
		SELECT t.id, t.name, t.description, t.color, t.created_at
		FROM tags t
		INNER JOIN image_tags it ON t.id = it.tag_id
		WHERE it.image_id = $1
		ORDER BY t.name ASC
	`

	return r.scanTags(ctx, query, imageID)
}

// GetTagImages retrieves all images for a specific tag
func (r *tagRepository) GetTagImages(ctx context.Context, tagID int, pagination PaginationParams) ([]*Image, error) {
	pagination.Validate()

	query := `
		SELECT i.id, i.filename, i.original_filename, i.content_type, i.file_size,
			   i.storage_path, i.thumbnail_path, i.status, i.processing_error, i.processed_at,
			   i.width, i.height, i.uploaded_at,
			   i.metadata, i.created_at, i.updated_at
		FROM images i
		INNER JOIN image_tags it ON i.id = it.image_id
		WHERE it.tag_id = $1
		ORDER BY i.uploaded_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := r.db.QueryContext(ctx, query, tagID, pagination.Limit, pagination.Offset)
	if err != nil {
		return nil, err
	}

	return scanImages(ctx, rows)
}

// CountImageTags returns the number of tags for an image
func (r *tagRepository) CountImageTags(ctx context.Context, imageID int) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM image_tags WHERE image_id = $1", imageID).Scan(&count)
	return count, err
}

// CountTagImages returns the number of images for a tag
func (r *tagRepository) CountTagImages(ctx context.Context, tagID int) (int, error) {
	var count int
	err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM image_tags WHERE tag_id = $1", tagID).Scan(&count)
	return count, err
}

// GetPredefined retrieves all active predefined tags ordered by display_order and name
func (r *tagRepository) GetPredefined(ctx context.Context) ([]*Tag, error) {
	query := `
		SELECT id, name, description, color, created_at
		FROM tags
		WHERE is_predefined = true AND is_active = true
		ORDER BY display_order ASC, name ASC
	`
	return r.scanTags(ctx, query)
}

// GetPredefinedByCategory retrieves predefined tags grouped by category
func (r *tagRepository) GetPredefinedByCategory(ctx context.Context) (map[string][]*Tag, error) {
	query := `
		SELECT id, name, description, color, created_at, category
		FROM tags
		WHERE is_predefined = true AND is_active = true
		ORDER BY category ASC, display_order ASC, name ASC
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }() //nolint:errcheck // Resource cleanup

	tagsByCategory := make(map[string][]*Tag)
	for rows.Next() {
		tag := &Tag{}
		var category sql.NullString
		err := rows.Scan(
			&tag.ID,
			&tag.Name,
			&tag.Description,
			&tag.Color,
			&tag.CreatedAt,
			&category,
		)
		if err != nil {
			return nil, err
		}

		cat := "other"
		if category.Valid {
			cat = category.String
		}

		tagsByCategory[cat] = append(tagsByCategory[cat], tag)
	}

	return tagsByCategory, rows.Err()
}

// scanTags is a helper method to scan multiple tag records
func (r *tagRepository) scanTags(ctx context.Context, query string, args ...interface{}) ([]*Tag, error) {
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }() //nolint:errcheck // Resource cleanup

	var tags []*Tag
	for rows.Next() {
		tag := &Tag{}
		err := rows.Scan(
			&tag.ID,
			&tag.Name,
			&tag.Description,
			&tag.Color,
			&tag.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}

	return tags, rows.Err()
}
