package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
	"path/filepath"
	"strings"
	"time"

	"image-gallery/internal/config"
)

// Constants for repeated string literals
const (
	noSuchKeyError = "NoSuchKey"
)

// Service implements the domain StorageService interface
type Service struct {
	store  ObjectStore
	config *config.StorageConfig
}

// NewService wraps an ObjectStore with the upload validation rules. The store
// owns connectivity and bucket setup.
func NewService(cfg *config.StorageConfig, store ObjectStore) (*Service, error) {
	if cfg == nil || store == nil {
		return nil, errors.New("storage config and store are required")
	}
	return &Service{store: store, config: cfg}, nil
}

// Provider is the backend name (s3, gcs, memory).
func (s *Service) Provider() string { return s.store.Provider() }

// Store implements StorageService.Store
func (s *Service) Store(ctx context.Context, filename string, contentType string, data io.Reader, size int64) (string, error) {
	if filename == "" {
		return "", errors.New("filename cannot be empty")
	}

	if contentType == "" {
		return "", errors.New("content type cannot be empty")
	}

	if data == nil {
		return "", errors.New("data cannot be nil")
	}

	if size < 0 {
		return "", errors.New("size must be non-negative")
	}

	// Security validations
	if err := s.validateFileSecurity(filename, contentType); err != nil {
		return "", fmt.Errorf("security validation failed: %w", err)
	}

	// Validate content type
	if !s.isValidContentType(contentType) {
		return "", fmt.Errorf("unsupported content type: %s", contentType)
	}

	// Create a buffered reader for magic number validation
	bufferedData, err := s.validateFileContent(data, contentType)
	if err != nil {
		return "", fmt.Errorf("file content validation failed: %w", err)
	}

	// Generate unique storage path
	storagePath := s.generateStoragePath(filename)

	// Calculate file size and hash with limits
	sizeReader := &sizeCountingReader{reader: bufferedData, maxSize: s.getMaxFileSize()}
	hashReader := &hashingReader{reader: sizeReader, hasher: sha256.New()}

	err = s.store.Put(ctx, storagePath, contentType, hashReader, size, map[string]string{
		"original-filename": filename,
		"upload-time":       time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return "", fmt.Errorf("failed to upload file: %w", err)
	}
	if sizeReader.size == 0 {
		_ = s.store.Delete(ctx, storagePath) //nolint:errcheck // cleanup in error path
		return "", errors.New("uploaded file has zero size")
	}
	return storagePath, nil
}

// StoreAt writes to a caller-chosen key (worker thumbnails:
// thumbnails/<id>.<ext>). Rewriting the same key is how reprocessing stays idempotent.
func (s *Service) StoreAt(ctx context.Context, key, contentType string, data io.Reader, size int64) error {
	if key == "" || data == nil {
		return errors.New("key and data are required")
	}
	if !s.isValidContentType(contentType) {
		return fmt.Errorf("unsupported content type: %s", contentType)
	}
	return s.store.Put(ctx, key, contentType, data, size, nil)
}

// Retrieve implements StorageService.Retrieve
func (s *Service) Retrieve(ctx context.Context, path string) (io.ReadCloser, error) {
	if path == "" {
		return nil, errors.New("path cannot be empty")
	}

	rc, err := s.store.Get(ctx, path)
	if errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("file not found: %s: %w", path, err)
	}
	return rc, err
}

// Delete implements StorageService.Delete
func (s *Service) Delete(ctx context.Context, path string) error {
	if path == "" {
		return errors.New("path cannot be empty")
	}

	// Check if object exists before deletion
	exists, err := s.Exists(ctx, path)
	if err != nil {
		return fmt.Errorf("failed to check if object exists: %w", err)
	}

	if !exists {
		return fmt.Errorf("file not found: %s", path)
	}

	if err := s.store.Delete(ctx, path); err != nil {
		return fmt.Errorf("failed to delete object: %w", err)
	}

	return nil
}

// Exists implements StorageService.Exists
func (s *Service) Exists(ctx context.Context, path string) (bool, error) {
	if path == "" {
		return false, errors.New("path cannot be empty")
	}

	_, err := s.store.Stat(ctx, path)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

// FileInfo represents metadata about a stored file
type FileInfo struct {
	Path         string
	Size         int64
	ContentType  string
	LastModified int64
	ETag         string
}

// GetFileInfo implements StorageService.GetFileInfo
func (s *Service) GetFileInfo(ctx context.Context, path string) (*FileInfo, error) {
	if path == "" {
		return nil, errors.New("path cannot be empty")
	}

	info, err := s.store.Stat(ctx, path)
	if errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("file not found: %s: %w", path, err)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to stat object: %w", err)
	}

	return &FileInfo{
		Path:         path,
		Size:         info.Size,
		ContentType:  info.ContentType,
		LastModified: info.LastModified.Unix(),
		ETag:         info.ETag,
	}, nil
}

// Health checks the health of the storage service
func (s *Service) Health(ctx context.Context) error {
	return s.store.Health(ctx)
}

// ListObjects lists objects in the bucket with pagination
func (s *Service) ListObjects(ctx context.Context, prefix string, maxKeys int) ([]ObjectInfo, error) {
	return s.store.List(ctx, prefix, maxKeys)
}

// Private helper methods

func (s *Service) generateStoragePath(filename string) string {
	// Create a hash-based directory structure for better distribution
	sum := sha256.Sum256([]byte(filename + time.Now().String()))
	hashStr := fmt.Sprintf("%x", sum)

	// Use first 2 characters for directory structure
	dir1 := hashStr[:2]
	dir2 := hashStr[2:4]

	// Add timestamp to ensure uniqueness
	timestamp := time.Now().Unix()
	ext := filepath.Ext(filename)
	base := strings.TrimSuffix(filepath.Base(filename), ext)

	// Clean filename for storage
	cleanBase := s.sanitizeFilename(base)

	return fmt.Sprintf("%s/%s/%s_%d%s", dir1, dir2, cleanBase, timestamp, ext)
}

func (s *Service) sanitizeFilename(filename string) string {
	// Remove or replace problematic characters
	sanitized := strings.ReplaceAll(filename, " ", "_")
	sanitized = strings.ReplaceAll(sanitized, "/", "_")
	sanitized = strings.ReplaceAll(sanitized, "\\", "_")
	sanitized = strings.ReplaceAll(sanitized, "..", "_")

	// Limit length
	if len(sanitized) > 100 {
		sanitized = sanitized[:100]
	}

	return sanitized
}

func (s *Service) isValidContentType(contentType string) bool {
	// Basic supported content types - this could be moved to config
	supportedTypes := map[string]bool{
		contentTypeJPEG:    true,
		contentTypeJPEGAlt: true,
		contentTypePNG:     true,
		contentTypeGIF:     true,
		contentTypeWebP:    true,
	}
	return supportedTypes[contentType]
}

// ObjectInfo represents information about a stored object
type ObjectInfo struct {
	Key          string            `json:"key"`
	Size         int64             `json:"size"`
	ContentType  string            `json:"content_type"`
	LastModified time.Time         `json:"last_modified"`
	ETag         string            `json:"etag"`
	UserMetadata map[string]string `json:"user_metadata,omitempty"`
}

// Helper readers

type sizeCountingReader struct {
	reader  io.Reader
	size    int64
	maxSize int64
}

func (r *sizeCountingReader) Read(p []byte) (n int, err error) {
	n, err = r.reader.Read(p)
	r.size += int64(n)

	// Check if we've exceeded the maximum file size
	if r.maxSize > 0 && r.size > r.maxSize {
		return 0, fmt.Errorf("file size exceeds maximum allowed size of %d bytes", r.maxSize)
	}

	return
}

type hashingReader struct {
	reader io.Reader
	hasher hash.Hash
}

func (r *hashingReader) Read(p []byte) (n int, err error) {
	n, err = r.reader.Read(p)
	if n > 0 {
		r.hasher.Write(p[:n])
	}
	return
}

// Security validation methods

// validateFileSecurity performs comprehensive file security validations
func (s *Service) validateFileSecurity(filename, contentType string) error {
	// Check for path traversal attempts
	if err := s.validateFilename(filename); err != nil {
		return err
	}

	// Validate file extension matches content type
	if err := s.validateExtensionContentType(filename, contentType); err != nil {
		return err
	}

	// Additional security checks can be added here
	// - Blacklisted filename patterns
	// - Suspicious file extensions
	// - Rate limiting per IP/user (would need context)

	return nil
}

// validateFilename checks for security issues in filenames
func (s *Service) validateFilename(filename string) error {
	// Check for path traversal attempts
	if strings.Contains(filename, "..") {
		return errors.New("path traversal attempt detected")
	}

	// Check for absolute paths
	if strings.HasPrefix(filename, "/") || strings.HasPrefix(filename, "\\") {
		return errors.New("absolute paths not allowed")
	}

	// Check for hidden files (starting with .)
	if strings.HasPrefix(filepath.Base(filename), ".") {
		return errors.New("hidden files not allowed")
	}

	// Check for suspicious patterns
	suspiciousPatterns := []string{
		"<script",
		"javascript:",
		"data:",
		"vbscript:",
		"onload=",
		"onerror=",
	}

	lowerFilename := strings.ToLower(filename)
	for _, pattern := range suspiciousPatterns {
		if strings.Contains(lowerFilename, pattern) {
			return fmt.Errorf("suspicious pattern detected: %s", pattern)
		}
	}

	// Validate filename length
	if len(filename) > 255 {
		return errors.New("filename too long")
	}

	// Check for null bytes
	if strings.Contains(filename, "\x00") {
		return errors.New("null bytes not allowed in filename")
	}

	return nil
}

// validateExtensionContentType ensures file extension matches declared content type
func (s *Service) validateExtensionContentType(filename, contentType string) error {
	ext := strings.ToLower(filepath.Ext(filename))

	// Map of extensions to expected content types
	expectedTypes := map[string][]string{
		".jpg":  {contentTypeJPEG, contentTypeJPEGAlt},
		".jpeg": {contentTypeJPEG, contentTypeJPEGAlt},
		".png":  {contentTypePNG},
		".gif":  {contentTypeGIF},
		".webp": {contentTypeWebP},
	}

	if expected, exists := expectedTypes[ext]; exists {
		for _, expectedType := range expected {
			if contentType == expectedType {
				return nil
			}
		}
		return fmt.Errorf("content type %s does not match file extension %s", contentType, ext)
	}

	return fmt.Errorf("unsupported file extension: %s", ext)
}

// validateFileContent validates the actual file content against declared content type
func (s *Service) validateFileContent(data io.Reader, contentType string) (io.Reader, error) {
	// Read first 512 bytes for magic number validation
	buffer := make([]byte, 512)
	n, err := io.ReadFull(data, buffer)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, fmt.Errorf("failed to read file header: %w", err)
	}

	// Validate magic numbers (file signatures)
	if err := s.validateMagicNumber(buffer[:n], contentType); err != nil {
		return nil, err
	}

	// Create new reader that includes the header we already read
	return io.MultiReader(bytes.NewReader(buffer[:n]), data), nil
}

// validateMagicNumber checks file magic numbers against content type
func (s *Service) validateMagicNumber(header []byte, contentType string) error {
	if err := s.validateHeaderSize(header); err != nil {
		return err
	}

	patterns, err := s.getMagicNumberPatterns(contentType)
	if err != nil {
		return err
	}

	return s.checkMagicNumberPatterns(header, contentType, patterns)
}

// validateHeaderSize checks if header is large enough for validation
func (s *Service) validateHeaderSize(header []byte) error {
	if len(header) < 4 {
		return errors.New("file too small to validate")
	}
	return nil
}

// getMagicNumberPatterns returns the magic number patterns for a content type
func (s *Service) getMagicNumberPatterns(contentType string) ([][]byte, error) {
	magicNumbers := map[string][][]byte{
		contentTypeJPEG: {
			{0xFF, 0xD8, 0xFF}, // JPEG
		},
		contentTypeJPEGAlt: {
			{0xFF, 0xD8, 0xFF}, // JPEG (alternative content type)
		},
		contentTypePNG: {
			{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}, // PNG
		},
		contentTypeGIF: {
			{0x47, 0x49, 0x46, 0x38, 0x37, 0x61}, // GIF87a
			{0x47, 0x49, 0x46, 0x38, 0x39, 0x61}, // GIF89a
		},
		contentTypeWebP: {
			{0x52, 0x49, 0x46, 0x46}, // RIFF (WebP container)
		},
	}

	patterns, exists := magicNumbers[contentType]
	if !exists {
		return nil, fmt.Errorf("magic number validation not supported for content type: %s", contentType)
	}
	return patterns, nil
}

// checkMagicNumberPatterns validates header against all patterns for content type
func (s *Service) checkMagicNumberPatterns(header []byte, contentType string, patterns [][]byte) error {
	for _, pattern := range patterns {
		if s.matchesPattern(header, pattern) {
			if err := s.validateSpecialCases(header, contentType); err != nil {
				continue // Try next pattern if special validation fails
			}
			return nil // Valid match found
		}
	}
	return fmt.Errorf("file content does not match declared content type %s", contentType)
}

// matchesPattern checks if header matches a specific magic number pattern
func (s *Service) matchesPattern(header []byte, pattern []byte) bool {
	if len(header) < len(pattern) {
		return false
	}

	for i, b := range pattern {
		if header[i] != b {
			return false
		}
	}
	return true
}

// validateSpecialCases handles content type specific additional validations
func (s *Service) validateSpecialCases(header []byte, contentType string) error {
	if contentType == contentTypeWebP {
		return s.validateWebPSignature(header)
	}
	return nil
}

// validateWebPSignature validates the WebP signature at offset 8
func (s *Service) validateWebPSignature(header []byte) error {
	if len(header) < 12 {
		return errors.New("header too short for WebP validation")
	}

	// Check for WEBP signature at offset 8
	webpSignature := []byte{0x57, 0x45, 0x42, 0x50} // "WEBP"
	for i, b := range webpSignature {
		if header[8+i] != b {
			return errors.New("invalid WebP signature")
		}
	}
	return nil
}

// getMaxFileSize returns the maximum allowed file size in bytes
func (s *Service) getMaxFileSize() int64 {
	// Default: 10MB, could be made configurable
	return 10 * 1024 * 1024
}
