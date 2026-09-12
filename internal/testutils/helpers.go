package testutils

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/testcontainers/testcontainers-go"

	"image-gallery/internal/domain/image"
	"image-gallery/internal/platform/database"
)

// TestSuite provides common test utilities for integration tests
type TestSuite struct {
	Containers *TestContainers
	Repos      *database.Repositories
}

// SetupTestSuite initializes a complete test suite with containers and repositories
func SetupTestSuite(ctx context.Context) (*TestSuite, error) {
	containers, err := SetupTestContainers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to setup test containers: %w", err)
	}

	// Initialize repositories
	repos := database.NewRepositories(
		database.NewImageRepository(containers.DB),
		database.NewTagRepository(containers.DB),
		database.NewAlbumRepository(containers.DB),
	)

	return &TestSuite{
		Containers: containers,
		Repos:      repos,
	}, nil
}

// Cleanup cleans up all test resources
func (ts *TestSuite) Cleanup(ctx context.Context) error {
	return ts.Containers.Cleanup(ctx)
}

// ResetData clears all test data and resets the environment
func (ts *TestSuite) ResetData(ctx context.Context) error {
	return ts.Containers.ResetDatabase(ctx)
}

// CreateTestImage creates a test image record in the database
func (ts *TestSuite) CreateTestImage(ctx context.Context, filename string) (*database.Image, error) {
	img := &database.Image{
		Filename:         fmt.Sprintf("test_%s_%d.jpg", filename, secureRandInt()),
		OriginalFilename: filename + ".jpg",
		ContentType:      contentTypeJPEG,
		FileSize:         1024 * (1 + secureRandInt63n(99)), // Random size 1KB to 100KB
		StoragePath:      fmt.Sprintf("images/test_%d.jpg", time.Now().UnixNano()),
		Width:            intPtr(800 + secureRandIntn(400)),
		Height:           intPtr(600 + secureRandIntn(400)),
		Metadata:         database.Metadata{},
	}

	if err := ts.Repos.Images.Create(ctx, img); err != nil {
		return nil, fmt.Errorf("failed to create test image: %w", err)
	}

	return img, nil
}

// CreateTestTag creates a test tag in the database
func (ts *TestSuite) CreateTestTag(ctx context.Context, name string) (*database.Tag, error) {
	tag := &database.Tag{
		Name:        name,
		Description: stringPtr(fmt.Sprintf("Test tag: %s", name)),
		Color:       stringPtr("#" + fmt.Sprintf("%06x", secureRandIntn(0xFFFFFF))),
	}

	if err := ts.Repos.Tags.Create(ctx, tag); err != nil {
		return nil, fmt.Errorf("failed to create test tag: %w", err)
	}

	return tag, nil
}

// CreateTestAlbum creates a test album in the database
func (ts *TestSuite) CreateTestAlbum(ctx context.Context, name string) (*database.Album, error) {
	album := &database.Album{
		Name:        name,
		Description: stringPtr(fmt.Sprintf("Test album: %s", name)),
		IsPublic:    secureRandIntn(2) == 1,
	}

	if err := ts.Repos.Albums.Create(ctx, album); err != nil {
		return nil, fmt.Errorf("failed to create test album: %w", err)
	}

	return album, nil
}

// CreateTestImageWithTags creates a test image with associated tags
func (ts *TestSuite) CreateTestImageWithTags(ctx context.Context, filename string, tagNames []string) (*database.Image, []*database.Tag, error) {
	img, err := ts.CreateTestImage(ctx, filename)
	if err != nil {
		return nil, nil, err
	}

	tags := make([]*database.Tag, 0, len(tagNames))
	for _, tagName := range tagNames {
		tag, err := ts.CreateTestTag(ctx, tagName)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create tag %s: %w", tagName, err)
		}

		// Associate tag with image
		if err := ts.Repos.Tags.AddToImage(ctx, img.ID, tag.ID); err != nil {
			return nil, nil, fmt.Errorf("failed to associate tag %s with image: %w", tagName, err)
		}

		tags = append(tags, tag)
	}

	return img, tags, nil
}

const contentTypeJPEG = "image/jpeg"

// imageMagicNumbers are the header bytes storage.Service's content-type
// sniffing checks for; keep in step with the patterns it validates against.
var imageMagicNumbers = map[string][]byte{
	contentTypeJPEG: {0xFF, 0xD8, 0xFF},
	"image/jpg":     {0xFF, 0xD8, 0xFF},
	"image/png":     {0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A},
	"image/gif":     {0x47, 0x49, 0x46, 0x38, 0x39, 0x61},
}

// GenerateTestImageData creates mock image data for testing. The leading
// bytes are overwritten with contentType's magic number, so the real
// storage.Service upload path (which sniffs content against the declared
// type) accepts it; the remaining bytes stay random.
func GenerateTestImageData(width, height int, contentType string) []byte {
	size := width * height * 3 // RGB
	data := secureRandBytes(size)
	if magic := imageMagicNumbers[contentType]; len(magic) > 0 && len(data) >= len(magic) {
		copy(data, magic)
	}
	return data
}

// CreateMultipartFormData creates multipart form data for file upload testing
func CreateMultipartFormData(filename string, data []byte, additionalFields map[string]string) (formData []byte, contentType string, err error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add file
	fileWriter, err := writer.CreateFormFile("image", filename)
	if err != nil {
		return nil, "", err
	}

	if _, err := fileWriter.Write(data); err != nil {
		return nil, "", err
	}

	// Add additional fields
	for key, value := range additionalFields {
		if err := writer.WriteField(key, value); err != nil {
			return nil, "", err
		}
	}

	if err := writer.Close(); err != nil {
		return nil, "", err
	}

	return buf.Bytes(), writer.FormDataContentType(), nil
}

// MakeTestRequest creates an HTTP test request with the given parameters
func MakeTestRequest(method, url string, body io.Reader, headers map[string]string) *http.Request {
	req := httptest.NewRequest(method, url, body)

	for key, value := range headers {
		req.Header.Set(key, value)
	}

	return req
}

// MakeJSONRequest creates an HTTP test request with JSON body
func MakeJSONRequest(method, url string, payload interface{}) *http.Request {
	var body io.Reader
	if payload != nil {
		jsonData, _ := json.Marshal(payload) //nolint:errcheck // Test utility - marshal error would cause nil body
		body = bytes.NewReader(jsonData)
	}

	req := httptest.NewRequest(method, url, body)
	req.Header.Set("Content-Type", "application/json")

	return req
}

// AssertHTTPStatus checks if the HTTP response has the expected status code
func AssertHTTPStatus(t TestingInterface, resp *httptest.ResponseRecorder, expectedStatus int) {
	if resp.Code != expectedStatus {
		t.Errorf("Expected status %d, got %d. Body: %s", expectedStatus, resp.Code, resp.Body.String())
	}
}

// AssertJSONResponse checks if the response contains valid JSON and optionally validates structure
func AssertJSONResponse(t TestingInterface, resp *httptest.ResponseRecorder, target interface{}) error {
	if !strings.Contains(resp.Header().Get("Content-Type"), "application/json") {
		t.Errorf("Expected JSON response, got %s", resp.Header().Get("Content-Type"))
		return fmt.Errorf("not a JSON response")
	}

	if target != nil {
		if err := json.Unmarshal(resp.Body.Bytes(), target); err != nil {
			t.Errorf("Failed to unmarshal JSON response: %v", err)
			return err
		}
	}

	return nil
}

// TestingInterface defines the interface for testing frameworks (compatible with testing.T)
type TestingInterface interface {
	Errorf(format string, args ...interface{})
	Fatalf(format string, args ...interface{})
	Helper()
}

// Utility functions

func intPtr(i int) *int {
	return &i
}

func stringPtr(s string) *string {
	return &s
}

// WaitForContainer waits for a container to be ready with timeout
func WaitForContainer(ctx context.Context, container testcontainers.Container, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for container")
		case <-ticker.C:
			state, err := container.State(ctx)
			if err != nil {
				continue
			}
			if state.Running {
				return nil
			}
		}
	}
}

// RandomString generates a random string of specified length
func RandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[secureRandIntn(len(charset))]
	}
	return string(b)
}

// RandomEmail generates a random email address for testing
func RandomEmail() string {
	return fmt.Sprintf("%s@test.com", RandomString(8))
}

// CreateImageRequest creates a test image creation request
func CreateImageRequest(filename, contentType string, tags []string) *image.CreateImageRequest {
	return &image.CreateImageRequest{
		OriginalFilename: filename,
		ContentType:      contentType,
		FileSize:         1024 * (1 + secureRandInt63n(99)),
		Width:            intPtr(800),
		Height:           intPtr(600),
		Tags:             tags,
	}
}

// Secure random number generators for test utilities

// secureRandInt generates a secure random integer
func secureRandInt() int {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// Fallback to timestamp-based value for tests
		return int(time.Now().UnixNano())
	}
	// #nosec G115 -- Test utility function, overflow acceptable for test randomness
	return int(binary.BigEndian.Uint64(buf[:]))
}

// secureRandInt63n generates a secure random int64 in range [0, n)
func secureRandInt63n(n int64) int64 {
	if n <= 0 {
		return 0
	}
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		// Fallback to timestamp-based value for tests
		return time.Now().UnixNano() % n
	}
	// Reduce modulo n in unsigned space before converting to int64: converting
	// a random uint64 to int64 first can produce a negative value, and Go's `%`
	// preserves the dividend's sign, so the result would fall outside [0, n).
	// #nosec G115 -- Test utility function, overflow acceptable for test randomness
	return int64(binary.BigEndian.Uint64(buf[:]) % uint64(n))
}

// secureRandIntn generates a secure random int in range [0, n)
func secureRandIntn(n int) int {
	if n <= 0 {
		return 0
	}
	return int(secureRandInt63n(int64(n)))
}

// secureRandBytes generates secure random bytes for testing
func secureRandBytes(size int) []byte {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		// Fallback to predictable data for tests if crypto fails
		for i := range data {
			data[i] = byte(i % 256)
		}
	}
	return data
}
