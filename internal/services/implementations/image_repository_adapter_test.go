package implementations

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"image-gallery/internal/domain/image"
	"image-gallery/internal/platform/database"
)

// MockDatabaseImageRepository is a mock implementation of database.ImageRepository
type MockDatabaseImageRepository struct {
	mock.Mock
}

func (m *MockDatabaseImageRepository) Create(ctx context.Context, img *database.Image) error {
	args := m.Called(ctx, img)
	// Simulate setting ID and timestamps like a real database would
	if args.Error(0) == nil {
		img.ID = 1
		img.CreatedAt = time.Now()
		img.UpdatedAt = time.Now()
		img.UploadedAt = time.Now()
	}
	return args.Error(0)
}

func (m *MockDatabaseImageRepository) GetByID(ctx context.Context, id int) (*database.Image, error) {
	args := m.Called(ctx, id)
	return args.Get(0).(*database.Image), args.Error(1)
}

func (m *MockDatabaseImageRepository) GetByFilename(ctx context.Context, filename string) (*database.Image, error) {
	args := m.Called(ctx, filename)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*database.Image), args.Error(1)
}

func (m *MockDatabaseImageRepository) GetByStoragePath(ctx context.Context, path string) (*database.Image, error) {
	args := m.Called(ctx, path)
	return args.Get(0).(*database.Image), args.Error(1)
}

func (m *MockDatabaseImageRepository) Update(ctx context.Context, img *database.Image) error {
	args := m.Called(ctx, img)
	// Simulate updating timestamp
	if args.Error(0) == nil {
		img.UpdatedAt = time.Now()
	}
	return args.Error(0)
}

func (m *MockDatabaseImageRepository) UpdateThumbnail(ctx context.Context, id int, thumbnailPath string) error {
	args := m.Called(ctx, id, thumbnailPath)
	return args.Error(0)
}

func (m *MockDatabaseImageRepository) UpdateStatus(ctx context.Context, id int, status string, processingError *string) error {
	args := m.Called(ctx, id, status, processingError)
	return args.Error(0)
}

func (m *MockDatabaseImageRepository) CompleteProcessing(ctx context.Context, id int, p database.ProcessingResult) error {
	args := m.Called(ctx, id, p)
	return args.Error(0)
}

func (m *MockDatabaseImageRepository) Delete(ctx context.Context, id int) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockDatabaseImageRepository) DeleteByStoragePath(ctx context.Context, path string) error {
	args := m.Called(ctx, path)
	return args.Error(0)
}

func (m *MockDatabaseImageRepository) List(ctx context.Context, pagination database.PaginationParams, sort database.SortParams) ([]*database.Image, error) {
	args := m.Called(ctx, pagination, sort)
	return args.Get(0).([]*database.Image), args.Error(1)
}

func (m *MockDatabaseImageRepository) ListByContentType(ctx context.Context, contentType string, pagination database.PaginationParams) ([]*database.Image, error) {
	args := m.Called(ctx, contentType, pagination)
	return args.Get(0).([]*database.Image), args.Error(1)
}

func (m *MockDatabaseImageRepository) Search(ctx context.Context, filters database.SearchFilters, pagination database.PaginationParams, sort database.SortParams) ([]*database.Image, error) {
	args := m.Called(ctx, filters, pagination, sort)
	return args.Get(0).([]*database.Image), args.Error(1)
}

func (m *MockDatabaseImageRepository) GetByDateRange(ctx context.Context, start, end time.Time, pagination database.PaginationParams) ([]*database.Image, error) {
	args := m.Called(ctx, start, end, pagination)
	return args.Get(0).([]*database.Image), args.Error(1)
}

func (m *MockDatabaseImageRepository) GetRecent(ctx context.Context, since time.Time, limit int) ([]*database.Image, error) {
	args := m.Called(ctx, since, limit)
	return args.Get(0).([]*database.Image), args.Error(1)
}

func (m *MockDatabaseImageRepository) GetLargest(ctx context.Context, pagination database.PaginationParams) ([]*database.Image, error) {
	args := m.Called(ctx, pagination)
	return args.Get(0).([]*database.Image), args.Error(1)
}

func (m *MockDatabaseImageRepository) Count(ctx context.Context) (int, error) {
	args := m.Called(ctx)
	return args.Int(0), args.Error(1)
}

func (m *MockDatabaseImageRepository) CountByContentType(ctx context.Context, contentType string) (int, error) {
	args := m.Called(ctx, contentType)
	return args.Int(0), args.Error(1)
}

func (m *MockDatabaseImageRepository) GetStats(ctx context.Context) (*database.ImageStats, error) {
	args := m.Called(ctx)
	return args.Get(0).(*database.ImageStats), args.Error(1)
}

func (m *MockDatabaseImageRepository) GetContentTypeCounts(ctx context.Context) (map[string]int64, error) {
	args := m.Called(ctx)
	return args.Get(0).(map[string]int64), args.Error(1)
}

func (m *MockDatabaseImageRepository) GetWithTags(ctx context.Context, pagination database.PaginationParams, sort database.SortParams) ([]*database.Image, error) {
	args := m.Called(ctx, pagination, sort)
	return args.Get(0).([]*database.Image), args.Error(1)
}

func (m *MockDatabaseImageRepository) GetByTags(ctx context.Context, tags []string, matchAll bool, pagination database.PaginationParams) ([]*database.Image, error) {
	args := m.Called(ctx, tags, matchAll, pagination)
	return args.Get(0).([]*database.Image), args.Error(1)
}

func TestImageRepositoryAdapter_Create(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		// Given: A mock database repository and adapter
		mockDB := &MockDatabaseImageRepository{}
		adapter := NewImageRepositoryAdapter(mockDB)
		ctx := context.Background()

		domainImage := &image.Image{
			Filename:         "test-image.jpg",
			OriginalFilename: "original.jpg",
			ContentType:      "image/jpeg",
			FileSize:         1024,
			StoragePath:      "images/test-image.jpg",
			Width:            intPtr(800),
			Height:           intPtr(600),
		}

		// Expect the Create call on the database repository
		mockDB.On("Create", ctx, mock.AnythingOfType("*database.Image")).Return(nil)

		// When: Creating an image through the adapter
		err := adapter.Create(ctx, domainImage)

		// Then: Should succeed and update the domain image with generated values
		assert.NoError(t, err)
		assert.NotZero(t, domainImage.ID)
		assert.NotZero(t, domainImage.CreatedAt)
		assert.NotZero(t, domainImage.UpdatedAt)
		assert.NotZero(t, domainImage.UploadedAt)
		mockDB.AssertExpectations(t)
	})

	t.Run("DatabaseError", func(t *testing.T) {
		// Given: A mock database repository that returns an error
		mockDB := &MockDatabaseImageRepository{}
		adapter := NewImageRepositoryAdapter(mockDB)
		ctx := context.Background()

		domainImage := &image.Image{
			Filename:         "test-image.jpg",
			OriginalFilename: "original.jpg",
			ContentType:      "image/jpeg",
			FileSize:         1024,
			StoragePath:      "images/test-image.jpg",
		}

		expectedError := errors.New("database connection failed")
		mockDB.On("Create", ctx, mock.AnythingOfType("*database.Image")).Return(expectedError)

		// When: Creating an image through the adapter
		err := adapter.Create(ctx, domainImage)

		// Then: Should return the database error
		assert.Error(t, err)
		assert.Equal(t, expectedError, err)
		assert.Zero(t, domainImage.ID) // Should not be set on error
		mockDB.AssertExpectations(t)
	})
}

func TestImageRepositoryAdapter_GetByID(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		// Given: A mock database repository with an image
		mockDB := &MockDatabaseImageRepository{}
		adapter := NewImageRepositoryAdapter(mockDB)
		ctx := context.Background()
		imageID := 1

		now := time.Now()
		dbImage := &database.Image{
			ID:               imageID,
			Filename:         "test-image.jpg",
			OriginalFilename: "original.jpg",
			ContentType:      "image/jpeg",
			FileSize:         1024,
			StoragePath:      "images/test-image.jpg",
			Width:            intPtr(800),
			Height:           intPtr(600),
			CreatedAt:        now,
			UpdatedAt:        now,
			UploadedAt:       now,
			Metadata:         database.Metadata{},
		}

		mockDB.On("GetByID", ctx, imageID).Return(dbImage, nil)

		// When: Getting an image by ID
		result, err := adapter.GetByID(ctx, imageID)

		// Then: Should return the converted domain image
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, imageID, result.ID)
		assert.Equal(t, "test-image.jpg", result.Filename)
		assert.Equal(t, "original.jpg", result.OriginalFilename)
		assert.Equal(t, "image/jpeg", result.ContentType)
		assert.Equal(t, int64(1024), result.FileSize)
		assert.Equal(t, "images/test-image.jpg", result.StoragePath)
		assert.Equal(t, 800, *result.Width)
		assert.Equal(t, 600, *result.Height)
		mockDB.AssertExpectations(t)
	})

	t.Run("NotFound", func(t *testing.T) {
		// Given: A mock database repository that returns not found error
		mockDB := &MockDatabaseImageRepository{}
		adapter := NewImageRepositoryAdapter(mockDB)
		ctx := context.Background()
		imageID := 999

		expectedError := errors.New("image with ID 999 not found")
		mockDB.On("GetByID", ctx, imageID).Return((*database.Image)(nil), expectedError)

		// When: Getting a non-existent image
		result, err := adapter.GetByID(ctx, imageID)

		// Then: Should return the error
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Equal(t, expectedError, err)
		mockDB.AssertExpectations(t)
	})
}

func TestImageRepositoryAdapter_List(t *testing.T) {
	t.Run("Success_WithoutTag", func(t *testing.T) {
		// Given: A mock database repository with images
		mockDB := &MockDatabaseImageRepository{}
		adapter := NewImageRepositoryAdapter(mockDB)
		ctx := context.Background()

		req := &image.ListImagesRequest{
			Page:     1,
			PageSize: 10,
		}

		now := time.Now()
		dbImages := []*database.Image{
			{
				ID:               1,
				Filename:         "image1.jpg",
				OriginalFilename: "original1.jpg",
				ContentType:      "image/jpeg",
				FileSize:         1024,
				CreatedAt:        now,
				UpdatedAt:        now,
			},
			{
				ID:               2,
				Filename:         "image2.jpg",
				OriginalFilename: "original2.jpg",
				ContentType:      "image/png",
				FileSize:         2048,
				CreatedAt:        now,
				UpdatedAt:        now,
			},
		}

		expectedPagination := database.PaginationParams{
			Limit:  req.PageSize,
			Offset: req.GetOffset(),
		}
		expectedSort := database.SortParams{
			Field: "uploaded_at",
			Order: "DESC",
		}

		mockDB.On("GetWithTags", ctx, expectedPagination, expectedSort).Return(dbImages, nil)
		mockDB.On("Count", ctx).Return(2, nil)

		// When: Listing images without tag filter
		response, err := adapter.List(ctx, req)

		// Then: Should return paginated results
		assert.NoError(t, err)
		assert.NotNil(t, response)
		assert.Len(t, response.Images, 2)
		assert.Equal(t, 2, response.TotalCount)
		assert.Equal(t, 1, response.Page)
		assert.Equal(t, 10, response.PageSize)
		assert.Equal(t, 1, response.TotalPages)

		// Verify first image conversion
		assert.Equal(t, 1, response.Images[0].ID)
		assert.Equal(t, "image1.jpg", response.Images[0].Filename)
		assert.Equal(t, "image/jpeg", response.Images[0].ContentType)

		mockDB.AssertExpectations(t)
	})

	t.Run("Success_WithTag", func(t *testing.T) {
		// Given: A mock database repository with tag filtering
		mockDB := &MockDatabaseImageRepository{}
		adapter := NewImageRepositoryAdapter(mockDB)
		ctx := context.Background()

		req := &image.ListImagesRequest{
			Page:     1,
			PageSize: 10,
			Tag:      "nature",
		}

		now := time.Now()
		dbImages := []*database.Image{
			{
				ID:               1,
				Filename:         "nature1.jpg",
				OriginalFilename: "nature.jpg",
				ContentType:      "image/jpeg",
				FileSize:         1024,
				CreatedAt:        now,
				UpdatedAt:        now,
			},
		}

		expectedPagination := database.PaginationParams{
			Limit:  req.PageSize,
			Offset: req.GetOffset(),
		}
		expectedTags := []string{"nature"}

		// Mock GetByTags call (only one call now - count is derived from results)
		mockDB.On("GetByTags", ctx, expectedTags, false, expectedPagination).Return(dbImages, nil)

		// When: Listing images with tag filter
		response, err := adapter.List(ctx, req)

		// Then: Should return filtered results
		assert.NoError(t, err)
		assert.NotNil(t, response)
		assert.Len(t, response.Images, 1)
		assert.Equal(t, 1, response.Images[0].ID)
		assert.Equal(t, "nature1.jpg", response.Images[0].Filename)

		mockDB.AssertExpectations(t)
	})
}

func TestImageRepositoryAdapter_Delete(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		// Given: A mock database repository
		mockDB := &MockDatabaseImageRepository{}
		adapter := NewImageRepositoryAdapter(mockDB)
		ctx := context.Background()
		imageID := 1

		mockDB.On("Delete", ctx, imageID).Return(nil)

		// When: Deleting an image
		err := adapter.Delete(ctx, imageID)

		// Then: Should succeed
		assert.NoError(t, err)
		mockDB.AssertExpectations(t)
	})

	t.Run("NotFound", func(t *testing.T) {
		// Given: A mock database repository that returns not found error
		mockDB := &MockDatabaseImageRepository{}
		adapter := NewImageRepositoryAdapter(mockDB)
		ctx := context.Background()
		imageID := 999

		expectedError := errors.New("image with ID 999 not found")
		mockDB.On("Delete", ctx, imageID).Return(expectedError)

		// When: Deleting a non-existent image
		err := adapter.Delete(ctx, imageID)

		// Then: Should return the error
		assert.Error(t, err)
		assert.Equal(t, expectedError, err)
		mockDB.AssertExpectations(t)
	})
}

// mockCacheInvalidator is a mock implementation of CacheInvalidator.
type mockCacheInvalidator struct {
	mock.Mock
}

func (m *mockCacheInvalidator) InvalidateImageLists(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func TestImageRepositoryAdapter_CacheInvalidation(t *testing.T) {
	t.Run("UpdateStatus success invalidates the list cache", func(t *testing.T) {
		mockDB := &MockDatabaseImageRepository{}
		mockCache := &mockCacheInvalidator{}
		adapter := NewImageRepositoryAdapter(mockDB).(*ImageRepositoryAdapter)
		adapter.SetCacheInvalidator(mockCache)
		ctx := context.Background()

		mockDB.On("UpdateStatus", ctx, 1, "ready", (*string)(nil)).Return(nil)
		mockCache.On("InvalidateImageLists", ctx).Return(nil)

		err := adapter.UpdateStatus(ctx, 1, "ready", nil)

		assert.NoError(t, err)
		mockDB.AssertExpectations(t)
		mockCache.AssertExpectations(t)
	})

	t.Run("UpdateStatus failure does not invalidate the list cache", func(t *testing.T) {
		mockDB := &MockDatabaseImageRepository{}
		mockCache := &mockCacheInvalidator{}
		adapter := NewImageRepositoryAdapter(mockDB).(*ImageRepositoryAdapter)
		adapter.SetCacheInvalidator(mockCache)
		ctx := context.Background()

		dbErr := errors.New("db unavailable")
		mockDB.On("UpdateStatus", ctx, 1, "ready", (*string)(nil)).Return(dbErr)

		err := adapter.UpdateStatus(ctx, 1, "ready", nil)

		assert.ErrorIs(t, err, dbErr)
		mockDB.AssertExpectations(t)
		mockCache.AssertNotCalled(t, "InvalidateImageLists", mock.Anything)
	})

	t.Run("CompleteProcessing success invalidates the list cache", func(t *testing.T) {
		mockDB := &MockDatabaseImageRepository{}
		mockCache := &mockCacheInvalidator{}
		adapter := NewImageRepositoryAdapter(mockDB).(*ImageRepositoryAdapter)
		adapter.SetCacheInvalidator(mockCache)
		ctx := context.Background()

		result := image.ProcessingResult{ThumbnailPath: "thumbnails/1.jpg", Width: 100, Height: 200}
		mockDB.On("CompleteProcessing", ctx, 1, mock.AnythingOfType("database.ProcessingResult")).Return(nil)
		mockCache.On("InvalidateImageLists", ctx).Return(nil)

		err := adapter.CompleteProcessing(ctx, 1, result)

		assert.NoError(t, err)
		mockDB.AssertExpectations(t)
		mockCache.AssertExpectations(t)
	})

	t.Run("no invalidator wired is a no-op", func(t *testing.T) {
		mockDB := &MockDatabaseImageRepository{}
		adapter := NewImageRepositoryAdapter(mockDB).(*ImageRepositoryAdapter)
		ctx := context.Background()

		mockDB.On("UpdateStatus", ctx, 1, "ready", (*string)(nil)).Return(nil)

		err := adapter.UpdateStatus(ctx, 1, "ready", nil)

		assert.NoError(t, err)
		mockDB.AssertExpectations(t)
	})
}

func TestImageRepositoryAdapter_ExistsByFilename(t *testing.T) {
	t.Run("Exists", func(t *testing.T) {
		// Given: A mock database repository with an existing image
		mockDB := &MockDatabaseImageRepository{}
		adapter := NewImageRepositoryAdapter(mockDB)
		ctx := context.Background()
		filename := "existing.jpg"

		dbImage := &database.Image{
			ID:       1,
			Filename: filename,
		}

		mockDB.On("GetByFilename", ctx, filename).Return(dbImage, nil)

		// When: Checking if image exists by filename
		exists, err := adapter.ExistsByFilename(ctx, filename)

		// Then: Should return true
		assert.NoError(t, err)
		assert.True(t, exists)
		mockDB.AssertExpectations(t)
	})

	t.Run("DoesNotExist", func(t *testing.T) {
		// Given: A mock database repository that returns not found error
		mockDB := &MockDatabaseImageRepository{}
		adapter := NewImageRepositoryAdapter(mockDB)
		ctx := context.Background()
		filename := "nonexistent.jpg"

		expectedError := errors.New("image with filename nonexistent.jpg not found")
		mockDB.On("GetByFilename", ctx, filename).Return((*database.Image)(nil), expectedError)

		// When: Checking if image exists by filename
		exists, err := adapter.ExistsByFilename(ctx, filename)

		// Then: Should return false without error
		assert.NoError(t, err)
		assert.False(t, exists)
		mockDB.AssertExpectations(t)
	})
}

// Helper function
func intPtr(i int) *int {
	return &i
}
