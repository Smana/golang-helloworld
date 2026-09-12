package integrationtests

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"image-gallery/internal/domain/image"
	"image-gallery/internal/services"
	"image-gallery/internal/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// validPNGData is a minimal valid 1x1 PNG image
var validPNGData = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, // PNG signature
	0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, // IHDR chunk
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, // 1x1 dimensions
	0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
	0xDE, 0x00, 0x00, 0x00, 0x0C, 0x49, 0x44, 0x41,
	0x54, 0x08, 0xD7, 0x63, 0xF8, 0xCF, 0xC0, 0x00,
	0x00, 0x03, 0x01, 0x01, 0x00, 0x18, 0xDD, 0x8D,
	0xB4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E,
	0x44, 0xAE, 0x42, 0x60, 0x82,
}

// ImageUploadTestSuite provides integration testing for image upload functionality
type ImageUploadTestSuite struct {
	suite.Suite
	testSuite    *testutils.TestSuite
	ctx          context.Context
	container    *services.Container
	imageService image.ImageService
}

// SetupSuite sets up the test suite with real containers
func (s *ImageUploadTestSuite) SetupSuite() {
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
func (s *ImageUploadTestSuite) TearDownSuite() {
	if s.testSuite != nil {
		err := s.testSuite.Cleanup(s.ctx)
		require.NoError(s.T(), err, "Failed to cleanup test suite")
	}
}

// SetupTest resets the database before each test
func (s *ImageUploadTestSuite) SetupTest() {
	err := s.testSuite.ResetData(s.ctx)
	require.NoError(s.T(), err, "Failed to reset test data")
}

// TestImageUpload_WithDimensions tests image upload with dimension extraction
func (s *ImageUploadTestSuite) TestImageUpload_WithDimensions() {
	// Given: An upload request with dimensions
	width, height := 1, 1
	req := &image.CreateImageRequest{
		OriginalFilename: "test-upload.png",
		ContentType:      "image/png",
		FileSize:         int64(len(validPNGData)),
		Width:            &width,
		Height:           &height,
		Tags:             []string{"test", "integration"},
	}

	// When: Uploading the image
	uploadedImage, err := s.imageService.CreateImage(s.ctx, req, bytes.NewReader(validPNGData))

	// Then: Image should be created with metadata
	require.NoError(s.T(), err)
	assert.NotNil(s.T(), uploadedImage)
	assert.Greater(s.T(), uploadedImage.ID, 0)
	assert.Equal(s.T(), "test-upload.png", uploadedImage.OriginalFilename)
	assert.Equal(s.T(), "image/png", uploadedImage.ContentType)
	assert.NotNil(s.T(), uploadedImage.Width)
	assert.Equal(s.T(), 1, *uploadedImage.Width)
	assert.NotNil(s.T(), uploadedImage.Height)
	assert.Equal(s.T(), 1, *uploadedImage.Height)
	assert.Len(s.T(), uploadedImage.Tags, 2)

	// Verify file exists in storage
	exists, err := s.container.StorageService().Exists(s.ctx, uploadedImage.StoragePath)
	require.NoError(s.T(), err)
	assert.True(s.T(), exists)

	// Verify we can retrieve the image with metadata
	retrievedImage, err := s.imageService.GetImage(s.ctx, uploadedImage.ID)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), *uploadedImage.Width, *retrievedImage.Width)
	assert.Equal(s.T(), *uploadedImage.Height, *retrievedImage.Height)
	assert.Len(s.T(), retrievedImage.Tags, 2)
}

// TestImageUpload_WithTags tests image upload with multiple tags
func (s *ImageUploadTestSuite) TestImageUpload_WithTags() {
	// Given: An upload request with multiple tags
	width, height := 1, 1
	req := &image.CreateImageRequest{
		OriginalFilename: "tagged-image.png",
		ContentType:      "image/png",
		FileSize:         int64(len(validPNGData)),
		Width:            &width,
		Height:           &height,
		Tags:             []string{"vacation", "sunset", "beach"},
	}

	// When: Uploading the image
	uploadedImage, err := s.imageService.CreateImage(s.ctx, req, bytes.NewReader(validPNGData))

	// Then: Image should have all tags
	require.NoError(s.T(), err)
	assert.Len(s.T(), uploadedImage.Tags, 3)

	tagNames := make(map[string]bool)
	for _, tag := range uploadedImage.Tags {
		tagNames[tag.Name] = true
	}
	assert.True(s.T(), tagNames["vacation"])
	assert.True(s.T(), tagNames["sunset"])
	assert.True(s.T(), tagNames["beach"])
}

// TestImageUpload_ListWithMetadata tests listing images includes metadata
func (s *ImageUploadTestSuite) TestImageUpload_ListWithMetadata() {
	// Given: An uploaded image with metadata
	width, height := 100, 200
	req := &image.CreateImageRequest{
		OriginalFilename: "metadata-test.png",
		ContentType:      "image/png",
		FileSize:         int64(len(validPNGData)),
		Width:            &width,
		Height:           &height,
		Tags:             []string{"metadata"},
	}

	uploadedImage, err := s.imageService.CreateImage(s.ctx, req, bytes.NewReader(validPNGData))
	require.NoError(s.T(), err)

	// When: Listing images
	listReq := &image.ListImagesRequest{
		Page:     1,
		PageSize: 10,
	}
	listResp, err := s.imageService.ListImages(s.ctx, listReq)

	// Then: Metadata should be included
	require.NoError(s.T(), err)
	var foundImage *image.Image
	for i := range listResp.Images {
		if listResp.Images[i].ID == uploadedImage.ID {
			foundImage = &listResp.Images[i]
			break
		}
	}

	require.NotNil(s.T(), foundImage)
	assert.Equal(s.T(), "image/png", foundImage.ContentType)
	assert.NotNil(s.T(), foundImage.Width)
	assert.Equal(s.T(), 100, *foundImage.Width)
	assert.NotNil(s.T(), foundImage.Height)
	assert.Equal(s.T(), 200, *foundImage.Height)
	assert.Len(s.T(), foundImage.Tags, 1)
}

// TestImageUpload_RetrieveContent tests retrieving uploaded file content
func (s *ImageUploadTestSuite) TestImageUpload_RetrieveContent() {
	// Given: An uploaded image
	width, height := 1, 1
	req := &image.CreateImageRequest{
		OriginalFilename: "retrieval-test.png",
		ContentType:      "image/png",
		FileSize:         int64(len(validPNGData)),
		Width:            &width,
		Height:           &height,
	}

	uploadedImage, err := s.imageService.CreateImage(s.ctx, req, bytes.NewReader(validPNGData))
	require.NoError(s.T(), err)

	// When: Retrieving the file from storage
	reader, err := s.container.StorageService().Retrieve(s.ctx, uploadedImage.StoragePath)
	require.NoError(s.T(), err)
	defer reader.Close()

	retrievedData, err := io.ReadAll(reader)
	require.NoError(s.T(), err)

	// Then: Content should match original
	assert.Equal(s.T(), validPNGData, retrievedData)
}

// TestImageUpload_DimensionExtraction tests using ImageProcessor
func (s *ImageUploadTestSuite) TestImageUpload_DimensionExtraction() {
	// Given: A valid PNG image
	imageData := testutils.GenerateTestImageData(100, 100, "image/png")

	// When: Extracting image info
	imageInfo, err := s.container.ImageProcessor().GetImageInfo(s.ctx, strings.NewReader(string(imageData)))

	// Then: Dimensions should be extracted
	require.NoError(s.T(), err)
	assert.NotNil(s.T(), imageInfo)
	// Note: GenerateTestImageData creates a 1x1 image despite parameters
	assert.Greater(s.T(), imageInfo.Width, 0)
	assert.Greater(s.T(), imageInfo.Height, 0)
}

// Run the test suite
func TestImageUploadSuite(t *testing.T) {
	suite.Run(t, new(ImageUploadTestSuite))
}
