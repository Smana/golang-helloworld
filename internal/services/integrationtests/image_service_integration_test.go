package integrationtests

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"image-gallery/internal/domain/image"
	"image-gallery/internal/services"
	"image-gallery/internal/testutils"
)

// ImageServiceIntegrationTestSuite provides integration testing for the ImageService
type ImageServiceIntegrationTestSuite struct {
	suite.Suite
	testSuite    *testutils.TestSuite
	ctx          context.Context
	container    *services.Container
	imageService image.ImageService
}

// SetupSuite sets up the test suite with real containers
func (s *ImageServiceIntegrationTestSuite) SetupSuite() {
	if testing.Short() {
		s.T().Skip("Skipping integration tests in short mode")
	}
	s.ctx = context.Background()

	// Setup test containers
	testSuite, err := testutils.SetupTestSuite(s.ctx)
	require.NoError(s.T(), err, "Failed to setup test suite")
	s.testSuite = testSuite

	// Create services container with test database
	container, err := services.NewContainerForTest(&services.TestConfig{
		DatabaseURL: testSuite.Containers.GetDatabaseURL(),
	}, testSuite.Containers.DB, testSuite.Containers.ObjectStore)
	require.NoError(s.T(), err, "Failed to create services container")
	s.container = container
	s.imageService = container.ImageService()
}

// TearDownSuite cleans up after all tests
func (s *ImageServiceIntegrationTestSuite) TearDownSuite() {
	if s.testSuite != nil {
		err := s.testSuite.Cleanup(s.ctx)
		require.NoError(s.T(), err, "Failed to cleanup test suite")
	}
}

// SetupTest resets the database before each test
func (s *ImageServiceIntegrationTestSuite) SetupTest() {
	err := s.testSuite.ResetData(s.ctx)
	require.NoError(s.T(), err, "Failed to reset test data")
}

// TestCreateImage_Success tests successful image creation using TDD approach
func (s *ImageServiceIntegrationTestSuite) TestCreateImage_Success() {
	// Given: A valid image creation request
	imageData := testutils.GenerateTestImageData(800, 600, "image/jpeg")

	req := &image.CreateImageRequest{
		OriginalFilename: "test-image.jpg",
		ContentType:      "image/jpeg",
		// FileSize must be the real length of the uploaded bytes: the storage
		// service streams exactly FileSize bytes to the object store.
		FileSize: int64(len(imageData)),
		Width:    &[]int{800}[0],
		Height:   &[]int{600}[0],
		Tags:     []string{"nature", "landscape"},
	}

	// When: Creating an image
	createdImage, err := s.imageService.CreateImage(s.ctx, req, strings.NewReader(string(imageData)))

	// Then: Image should be created successfully
	require.NoError(s.T(), err, "Image creation should not fail")
	assert.NotNil(s.T(), createdImage, "Created image should not be nil")
	assert.NotZero(s.T(), createdImage.ID, "Created image should have an ID")
	assert.Equal(s.T(), req.OriginalFilename, createdImage.OriginalFilename)
	assert.Equal(s.T(), req.ContentType, createdImage.ContentType)
	assert.Equal(s.T(), req.FileSize, createdImage.FileSize)
	assert.NotEmpty(s.T(), createdImage.StoragePath, "Storage path should be set")
	assert.NotZero(s.T(), createdImage.CreatedAt, "CreatedAt should be set")

	// Verify tags were associated
	assert.Len(s.T(), createdImage.Tags, 2, "Image should have 2 tags")
	tagNames := make([]string, len(createdImage.Tags))
	for i, tag := range createdImage.Tags {
		tagNames[i] = tag.Name
	}
	assert.Contains(s.T(), tagNames, "nature")
	assert.Contains(s.T(), tagNames, "landscape")
}

// TestCreateImage_ValidationFailure tests image creation with invalid data
func (s *ImageServiceIntegrationTestSuite) TestCreateImage_ValidationFailure() {
	// Given: An invalid image creation request (empty filename)
	req := &image.CreateImageRequest{
		OriginalFilename: "", // Invalid: empty filename
		ContentType:      "image/jpeg",
		FileSize:         1024,
	}

	imageData := testutils.GenerateTestImageData(100, 100, req.ContentType)

	// When: Attempting to create an image
	createdImage, err := s.imageService.CreateImage(s.ctx, req, strings.NewReader(string(imageData)))

	// Then: Creation should fail with validation error
	assert.Error(s.T(), err, "Image creation should fail with invalid data")
	assert.Nil(s.T(), createdImage, "No image should be created")
	assert.Contains(s.T(), err.Error(), "filename", "Error should mention filename validation")
}

// TestGetImage_Success tests successful image retrieval
func (s *ImageServiceIntegrationTestSuite) TestGetImage_Success() {
	// Given: An existing image in the database
	testImage, err := s.testSuite.CreateTestImage(s.ctx, "existing-image")
	require.NoError(s.T(), err, "Failed to create test image")

	// When: Retrieving the image
	retrievedImage, err := s.imageService.GetImage(s.ctx, testImage.ID)

	// Then: Image should be retrieved successfully
	require.NoError(s.T(), err, "Image retrieval should not fail")
	assert.NotNil(s.T(), retrievedImage, "Retrieved image should not be nil")
	assert.Equal(s.T(), testImage.ID, retrievedImage.ID)
	assert.Equal(s.T(), testImage.Filename, retrievedImage.Filename)
	assert.Equal(s.T(), testImage.OriginalFilename, retrievedImage.OriginalFilename)
}

// TestGetImage_NotFound tests image retrieval with non-existent ID
func (s *ImageServiceIntegrationTestSuite) TestGetImage_NotFound() {
	// Given: A non-existent image ID
	nonExistentID := 99999

	// When: Attempting to retrieve the image
	retrievedImage, err := s.imageService.GetImage(s.ctx, nonExistentID)

	// Then: Retrieval should fail with not found error
	assert.Error(s.T(), err, "Image retrieval should fail for non-existent image")
	assert.Nil(s.T(), retrievedImage, "No image should be retrieved")
}

// TestListImages_WithPagination tests image listing with pagination
func (s *ImageServiceIntegrationTestSuite) TestListImages_WithPagination() {
	// Given: Multiple images in the database
	expectedCount := 5
	for i := 0; i < expectedCount; i++ {
		_, err := s.testSuite.CreateTestImage(s.ctx, fmt.Sprintf("image-%d", i))
		require.NoError(s.T(), err, "Failed to create test image %d", i)
	}

	// When: Listing images with pagination
	req := &image.ListImagesRequest{
		Page:     1,
		PageSize: 3,
	}

	response, err := s.imageService.ListImages(s.ctx, req)

	// Then: Images should be listed with correct pagination
	require.NoError(s.T(), err, "Image listing should not fail")
	assert.NotNil(s.T(), response, "Response should not be nil")
	assert.Len(s.T(), response.Images, 3, "Should return 3 images per page")
	assert.Equal(s.T(), expectedCount, response.TotalCount, "Total count should be correct")
	assert.Equal(s.T(), 1, response.Page, "Page should be 1")
	assert.Equal(s.T(), 3, response.PageSize, "Page size should be 3")
	assert.Equal(s.T(), 2, response.TotalPages, "Should have 2 total pages")
}

// TestDeleteImage_Success tests successful image deletion
func (s *ImageServiceIntegrationTestSuite) TestDeleteImage_Success() {
	// Given: An existing image in the database
	testImage, err := s.testSuite.CreateTestImage(s.ctx, "image-to-delete")
	require.NoError(s.T(), err, "Failed to create test image")

	// When: Deleting the image
	err = s.imageService.DeleteImage(s.ctx, testImage.ID)

	// Then: Deletion should succeed
	require.NoError(s.T(), err, "Image deletion should not fail")

	// And: Image should no longer exist
	deletedImage, err := s.imageService.GetImage(s.ctx, testImage.ID)
	assert.Error(s.T(), err, "Getting deleted image should fail")
	assert.Nil(s.T(), deletedImage, "Deleted image should not be retrievable")
}

// TestImageWithTags_Integration tests full image lifecycle with tags
func (s *ImageServiceIntegrationTestSuite) TestImageWithTags_Integration() {
	// Given: Image creation request with tags
	imageData := testutils.GenerateTestImageData(400, 300, "image/jpeg")

	req := &image.CreateImageRequest{
		OriginalFilename: "tagged-image.jpg",
		ContentType:      "image/jpeg",
		FileSize:         int64(len(imageData)),
		Tags:             []string{"integration", "test", "lifecycle"},
	}

	// When: Creating image with tags
	createdImage, err := s.imageService.CreateImage(s.ctx, req, strings.NewReader(string(imageData)))
	require.NoError(s.T(), err, "Image creation with tags should succeed")

	// Then: Image should have all tags
	assert.Len(s.T(), createdImage.Tags, 3, "Image should have 3 tags")

	// When: Retrieving the image
	retrievedImage, err := s.imageService.GetImage(s.ctx, createdImage.ID)
	require.NoError(s.T(), err, "Image retrieval should succeed")

	// Then: Retrieved image should maintain tag associations
	assert.Len(s.T(), retrievedImage.Tags, 3, "Retrieved image should have 3 tags")

	// When: Updating image tags
	updateReq := &image.UpdateImageRequest{
		Tags: []string{"integration", "updated"}, // Remove "test", "lifecycle", add "updated"
	}

	updatedImage, err := s.imageService.UpdateImage(s.ctx, createdImage.ID, updateReq)
	require.NoError(s.T(), err, "Image update should succeed")

	// Then: Image should have updated tags
	assert.Len(s.T(), updatedImage.Tags, 2, "Updated image should have 2 tags")

	tagNames := make([]string, len(updatedImage.Tags))
	for i, tag := range updatedImage.Tags {
		tagNames[i] = tag.Name
	}
	assert.Contains(s.T(), tagNames, "integration")
	assert.Contains(s.T(), tagNames, "updated")
	assert.NotContains(s.T(), tagNames, "test")
	assert.NotContains(s.T(), tagNames, "lifecycle")
}

// TestImageStats_Integration tests image statistics functionality
func (s *ImageServiceIntegrationTestSuite) TestImageStats_Integration() {
	// Given: Multiple images of different sizes and content types. The
	// dimensions drive the payload length, and FileSize must be the real
	// length of those bytes: the storage service streams exactly FileSize
	// bytes to the object store.
	images := []struct {
		filename      string
		contentType   string
		width, height int
		tags          []string
	}{
		{"jpg-image.jpg", "image/jpeg", 40, 40, []string{"jpg", "small"}},
		{"png-image.png", "image/png", 60, 50, []string{"png", "medium"}},
		{"large-jpg.jpg", "image/jpeg", 100, 80, []string{"jpg", "large"}},
	}

	var expectedTotalSize int64
	for _, img := range images {
		imageData := testutils.GenerateTestImageData(img.width, img.height, img.contentType)
		req := &image.CreateImageRequest{
			OriginalFilename: img.filename,
			ContentType:      img.contentType,
			FileSize:         int64(len(imageData)),
			Tags:             img.tags,
		}
		expectedTotalSize += req.FileSize
		_, err := s.imageService.CreateImage(s.ctx, req, strings.NewReader(string(imageData)))
		require.NoError(s.T(), err, "Failed to create test image %s", img.filename)
	}

	// When: Getting image statistics
	stats, err := s.imageService.GetImageStats(s.ctx)

	// Then: Statistics should reflect the created images
	require.NoError(s.T(), err, "Getting image stats should not fail")
	assert.NotNil(s.T(), stats, "Stats should not be nil")
	assert.Equal(s.T(), int64(3), stats.TotalImages, "Should have 3 total images")
	assert.Equal(s.T(), expectedTotalSize, stats.TotalSize, "Total size should be sum of all images")

	// Verify content type distribution
	assert.Contains(s.T(), stats.ContentTypes, "image/jpeg")
	assert.Contains(s.T(), stats.ContentTypes, "image/png")
	assert.Equal(s.T(), int64(2), stats.ContentTypes["image/jpeg"]) // 2 JPEG images
	assert.Equal(s.T(), int64(1), stats.ContentTypes["image/png"])  // 1 PNG image
}

// TestRunner function to run the integration test suite
func TestImageServiceIntegration(t *testing.T) {
	suite.Run(t, new(ImageServiceIntegrationTestSuite))
}

// BenchmarkImageService_CreateImage benchmarks image creation performance
func BenchmarkImageService_CreateImage(b *testing.B) {
	ctx := context.Background()
	testSuite, err := testutils.SetupTestSuite(ctx)
	if err != nil {
		b.Fatalf("Failed to setup test suite: %v", err)
	}
	defer func() { _ = testSuite.Cleanup(ctx) }()

	container, err := services.NewContainerForTest(&services.TestConfig{
		DatabaseURL: testSuite.Containers.GetDatabaseURL(),
	}, testSuite.Containers.DB, testSuite.Containers.ObjectStore)
	if err != nil {
		b.Fatalf("Failed to create services container: %v", err)
	}

	imageService := container.ImageService()
	imageData := testutils.GenerateTestImageData(800, 600, "image/jpeg")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			req := &image.CreateImageRequest{
				OriginalFilename: fmt.Sprintf("bench-image-%d.jpg", i),
				ContentType:      "image/jpeg",
				FileSize:         int64(len(imageData)),
				Tags:             []string{"benchmark"},
			}

			_, err := imageService.CreateImage(ctx, req, strings.NewReader(string(imageData)))
			if err != nil {
				b.Fatalf("Image creation failed: %v", err)
			}
			i++
		}
	})
}
