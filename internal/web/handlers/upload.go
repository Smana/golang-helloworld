package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"image-gallery/internal/domain/image"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	maxUploadSize      = 10 << 20 // 10MB total per request (reduced from 50MB to prevent memory exhaustion)
	maxFileSize        = 10 << 20 // 10MB per file
	maxMemoryPerUpload = 1 << 20  // 1MB in-memory buffer per upload (rest spills to disk to prevent OOMKills)
)

// UploadResponse represents the response for a successful upload
type UploadResponse struct {
	Images []UploadedImageInfo `json:"images"`
	Count  int                 `json:"count"`
	Errors []UploadError       `json:"errors,omitempty"`
}

// UploadedImageInfo represents information about an uploaded image
type UploadedImageInfo struct {
	ID               int      `json:"id"`
	Filename         string   `json:"filename"`
	OriginalFilename string   `json:"original_filename"`
	Size             int64    `json:"size"`
	ContentType      string   `json:"content_type"`
	Width            *int     `json:"width,omitempty"`
	Height           *int     `json:"height,omitempty"`
	Tags             []string `json:"tags,omitempty"`
	URL              string   `json:"url,omitempty"`
}

// UploadError represents an error that occurred during upload
type UploadError struct {
	Filename string `json:"filename"`
	Error    string `json:"error"`
}

// processedFileResult holds the result of processing a single uploaded file
type processedFileResult struct {
	uploadedImage *UploadedImageInfo
	uploadError   *UploadError
	bytesUploaded int64
}

// uploadImagesHandler handles POST requests to upload one or more images
func (h *Handler) uploadImagesHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Start tracing span
	ctx, span := h.tracer.Start(ctx, "UploadImages")
	defer span.End()

	// Log upload initiation
	h.logger.Info(ctx).
		Str("user_agent", r.UserAgent()).
		Str("content_type", r.Header.Get("Content-Type")).
		Msg("Starting image upload request")

	// Limit request body size: parsing stops at maxUploadSize, whatever the client sends
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	// Parse multipart form with limited in-memory buffer
	// maxMemoryPerUpload (1MB) is buffered in RAM per request
	// Files larger than 1MB are written to temporary files in /tmp
	// This prevents OOMKills under high concurrency (10 concurrent uploads = 10MB not 100MB)
	//nolint:gosec // G120: bounded by the MaxBytesReader above, which gosec's taint analysis cannot see
	if err := r.ParseMultipartForm(maxMemoryPerUpload); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to parse multipart form")
		h.logger.Error(ctx).Err(err).Msg("Failed to parse multipart form")
		http.Error(w, fmt.Sprintf("Failed to parse form: %v", err), http.StatusBadRequest)
		return
	}

	// CRITICAL: Clean up multipart form resources to prevent memory leak
	// Without this, up to 50MB per request is leaked until request completes
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll() //nolint:errcheck // Cleanup operation
		}
	}()

	// Get uploaded files
	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		err := fmt.Errorf("no files provided")
		span.RecordError(err)
		span.SetStatus(codes.Error, "no files in request")
		h.logger.Warn(ctx).Msg("Upload request with no files")
		http.Error(w, "No files provided", http.StatusBadRequest)
		return
	}

	// Get tags (comma-separated)
	tagsStr := r.FormValue("tags")
	tags := parseTags(tagsStr)

	// Add span attributes
	span.SetAttributes(
		attribute.Int("upload.file_count", len(files)),
		attribute.Int("upload.tag_count", len(tags)),
	)

	h.logger.Info(ctx).
		Int("file_count", len(files)).
		Int("tag_count", len(tags)).
		Interface("tags", tags).
		Msg("Processing uploaded files")

	// Process each file
	uploadedImages := make([]UploadedImageInfo, 0, len(files))
	var uploadErrors []UploadError
	var totalBytes int64

	for _, fileHeader := range files {
		result := h.processUploadedFile(ctx, fileHeader, tags)

		if result.uploadedImage != nil {
			uploadedImages = append(uploadedImages, *result.uploadedImage)
			totalBytes += result.bytesUploaded
		}

		if result.uploadError != nil {
			uploadErrors = append(uploadErrors, *result.uploadError)
		}
	}

	// Set overall span attributes
	span.SetAttributes(
		attribute.Int("upload.success_count", len(uploadedImages)),
		attribute.Int("upload.error_count", len(uploadErrors)),
		attribute.Int64("upload.total_bytes", totalBytes),
	)

	// Determine response status
	statusCode := http.StatusCreated
	if len(uploadedImages) == 0 {
		statusCode = http.StatusBadRequest
		span.SetStatus(codes.Error, "all uploads failed")
	} else if len(uploadErrors) > 0 {
		statusCode = http.StatusMultiStatus // 207 - some succeeded, some failed
		span.SetStatus(codes.Ok, "partial success")
	} else {
		span.SetStatus(codes.Ok, "all uploads successful")
	}

	h.logger.Info(ctx).
		Int("success_count", len(uploadedImages)).
		Int("error_count", len(uploadErrors)).
		Int64("total_bytes", totalBytes).
		Int("status_code", statusCode).
		Msg("Upload request completed")

	// Build response
	response := UploadResponse{
		Images: uploadedImages,
		Count:  len(uploadedImages),
		Errors: uploadErrors,
	}

	// Send JSON response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Error(ctx).Err(err).Msg("Failed to encode response")
	}
}

// detectContentType detects the content type of a file by reading its header
func (h *Handler) detectContentType(ctx context.Context, file multipart.File, fileHeader *multipart.FileHeader, fileSpan trace.Span) (string, error) {
	contentType := fileHeader.Header.Get("Content-Type")
	if contentType != "" {
		return contentType, nil
	}

	// Read only first 512 bytes for content type detection (HTTP sniffing standard)
	buffer := make([]byte, 512)
	n, err := io.ReadFull(file, buffer)
	if err != nil && err != io.ErrUnexpectedEOF {
		fileSpan.RecordError(err)
		fileSpan.SetStatus(codes.Error, "failed to read file for content type detection")
		h.logger.Error(ctx).Err(err).Str("filename", fileHeader.Filename).Msg("Failed to read file for content type detection")
		return "", fmt.Errorf("failed to read file: %w", err)
	}
	contentType = http.DetectContentType(buffer[:n])

	// Seek back to start for upload (multipart.File supports seeking)
	if seeker, ok := file.(io.Seeker); ok {
		if _, err := seeker.Seek(0, io.SeekStart); err != nil {
			fileSpan.RecordError(err)
			fileSpan.SetStatus(codes.Error, "failed to seek file")
			h.logger.Error(ctx).Err(err).Str("filename", fileHeader.Filename).Msg("Failed to seek file")
			return "", fmt.Errorf("failed to seek file: %w", err)
		}
	}

	return contentType, nil
}

// processUploadedFile processes a single uploaded file and returns the result
func (h *Handler) processUploadedFile(ctx context.Context, fileHeader *multipart.FileHeader, tags []string) processedFileResult {
	// Create child span for each file
	_, fileSpan := h.tracer.Start(ctx, "ProcessUploadedFile",
		trace.WithAttributes(
			attribute.String("file.name", fileHeader.Filename),
			attribute.Int64("file.size", fileHeader.Size),
		),
	)
	defer fileSpan.End()

	h.logger.Debug(ctx).
		Str("filename", fileHeader.Filename).
		Int64("size", fileHeader.Size).
		Msg("Processing file")

	// Validate file size
	if fileHeader.Size > maxFileSize {
		err := fmt.Errorf("file too large: %d bytes (max %d bytes)", fileHeader.Size, maxFileSize)
		fileSpan.RecordError(err)
		fileSpan.SetStatus(codes.Error, "file too large")
		h.logger.Warn(ctx).Str("filename", fileHeader.Filename).Int64("size", fileHeader.Size).Msg("File too large")
		return processedFileResult{
			uploadError: &UploadError{
				Filename: fileHeader.Filename,
				Error:    err.Error(),
			},
		}
	}

	// Open uploaded file
	file, err := fileHeader.Open()
	if err != nil {
		fileSpan.RecordError(err)
		fileSpan.SetStatus(codes.Error, "failed to open file")
		h.logger.Error(ctx).Err(err).Str("filename", fileHeader.Filename).Msg("Failed to open uploaded file")
		return processedFileResult{
			uploadError: &UploadError{
				Filename: fileHeader.Filename,
				Error:    fmt.Sprintf("Failed to open file: %v", err),
			},
		}
	}

	// Detect and validate content type
	contentType, err := h.detectContentType(ctx, file, fileHeader, fileSpan)
	if err != nil {
		_ = file.Close() //nolint:errcheck // Already returning error, ignore close error
		return processedFileResult{
			uploadError: &UploadError{
				Filename: fileHeader.Filename,
				Error:    fmt.Sprintf("Failed to detect content type: %v", err),
			},
		}
	}

	if !isSupportedImageType(contentType) {
		_ = file.Close() //nolint:errcheck // Already returning error, ignore close error
		err := fmt.Errorf("unsupported image type: %s", contentType)
		fileSpan.RecordError(err)
		fileSpan.SetStatus(codes.Error, "unsupported content type")
		h.logger.Warn(ctx).Str("filename", fileHeader.Filename).Str("content_type", contentType).Msg("Unsupported content type")
		return processedFileResult{
			uploadError: &UploadError{
				Filename: fileHeader.Filename,
				Error:    err.Error(),
			},
		}
	}

	// Dimensions are extracted by the worker once it processes the image.
	createReq := &image.CreateImageRequest{
		OriginalFilename: fileHeader.Filename,
		ContentType:      contentType,
		FileSize:         fileHeader.Size,
		Tags:             tags,
	}

	// Upload image via ImageService (streaming upload - no buffering)
	// File is already open and seeked to start position
	img, err := h.imageService.CreateImage(ctx, createReq, file)
	_ = file.Close() //nolint:errcheck // Close after upload completes
	if err != nil {
		fileSpan.RecordError(err)
		fileSpan.SetStatus(codes.Error, "image creation failed")
		h.logger.Error(ctx).Err(err).Str("filename", fileHeader.Filename).Msg("Failed to create image")
		return processedFileResult{
			uploadError: &UploadError{
				Filename: fileHeader.Filename,
				Error:    fmt.Sprintf("Upload failed: %v", err),
			},
		}
	}

	// Serve the image through the app's own proxy endpoint (no presigned URLs).
	imageURL := fmt.Sprintf("/api/images/%d/view", img.ID)

	// Extract tag names
	tagNames := make([]string, 0, len(img.Tags))
	for _, tag := range img.Tags {
		tagNames = append(tagNames, tag.Name)
	}

	fileSpan.SetStatus(codes.Ok, "file processed successfully")

	h.logger.Info(ctx).
		Int("image_id", img.ID).
		Str("filename", fileHeader.Filename).
		Int64("size", fileHeader.Size).
		Msg("Successfully uploaded image")

	return processedFileResult{
		uploadedImage: &UploadedImageInfo{
			ID:               img.ID,
			Filename:         img.Filename,
			OriginalFilename: img.OriginalFilename,
			Size:             img.FileSize,
			ContentType:      img.ContentType,
			Width:            img.Width,
			Height:           img.Height,
			Tags:             tagNames,
			URL:              imageURL,
		},
		bytesUploaded: fileHeader.Size,
	}
}

// parseTags parses comma-separated tags and returns cleaned, deduplicated tag names
func parseTags(tagsStr string) []string {
	if tagsStr == "" {
		return nil
	}

	// Use map to deduplicate tags (case-insensitive)
	seen := make(map[string]bool)
	var tags []string
	parts := strings.Split(tagsStr, ",")
	for _, part := range parts {
		tag := strings.TrimSpace(part)
		if tag == "" {
			continue
		}
		// Normalize to lowercase for duplicate detection
		normalized := strings.ToLower(tag)
		if !seen[normalized] {
			tags = append(tags, tag)
			seen[normalized] = true
		}
	}
	return tags
}

// isSupportedImageType checks if the content type is a supported image format
func isSupportedImageType(contentType string) bool {
	supportedTypes := map[string]bool{
		"image/jpeg": true,
		"image/jpg":  true,
		"image/png":  true,
		"image/gif":  true,
		"image/webp": true,
	}
	return supportedTypes[contentType]
}
