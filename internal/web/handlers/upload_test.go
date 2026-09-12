package handlers

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/trace/noop"

	"image-gallery/internal/observability"
)

// zeros is an endless body; countingReader records how much of it was read.
type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

func TestUploadRejectsAnOversizedBodyWithoutReadingIt(t *testing.T) {
	var head bytes.Buffer
	mw := multipart.NewWriter(&head)
	if _, err := mw.CreateFormFile("files", "big.png"); err != nil {
		t.Fatal(err)
	}
	tail := strings.NewReader("\r\n--" + mw.Boundary() + "--\r\n")
	body := &countingReader{r: io.MultiReader(&head, io.LimitReader(zeros{}, 4*maxUploadSize), tail)}
	req := httptest.NewRequest(http.MethodPost, "/api/images", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h := &Handler{
		imageService: stubImages{},
		tracer:       noop.NewTracerProvider().Tracer("test"),
		logger:       observability.NewLoggerTo(io.Discard, observability.Config{}),
	}

	h.uploadImagesHandler(rec, req)

	assert.GreaterOrEqual(t, rec.Code, http.StatusBadRequest, "an oversized upload must be rejected")
	// A little slack over the cap for the read buffer; parsing the whole body would read 4x the cap.
	assert.LessOrEqual(t, body.n, int64(maxUploadSize+64<<10), "the handler read past the %d-byte upload cap", maxUploadSize)
}

func TestParseTags(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:     "single tag",
			input:    "vacation",
			expected: []string{"vacation"},
		},
		{
			name:     "multiple tags",
			input:    "vacation,sunset,beach",
			expected: []string{"vacation", "sunset", "beach"},
		},
		{
			name:     "tags with spaces",
			input:    "vacation, sunset , beach",
			expected: []string{"vacation", "sunset", "beach"},
		},
		{
			name:     "tags with empty values",
			input:    "vacation,,beach",
			expected: []string{"vacation", "beach"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseTags(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsSupportedImageType(t *testing.T) {
	tests := []struct {
		contentType string
		expected    bool
	}{
		{"image/jpeg", true},
		{"image/jpg", true},
		{"image/png", true},
		{"image/gif", true},
		{"image/webp", true},
		{"image/bmp", false},
		{"text/plain", false},
		{"application/pdf", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.contentType, func(t *testing.T) {
			result := isSupportedImageType(tt.contentType)
			assert.Equal(t, tt.expected, result)
		})
	}
}
