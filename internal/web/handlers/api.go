package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"image-gallery/internal/domain/image"
	"image-gallery/internal/domain/settings"
	"image-gallery/internal/platform/database"
	"image-gallery/internal/services/implementations"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	scenarioError  = "error"
	statusSuccess  = "success"
	statusError    = "error"
	queryParamTrue = "true"
	defaultUserID  = "default"
)

//nolint:gocyclo // Handler with multiple response formats and filter logic
func (h *Handler) listImagesHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse tag filters from query parameters
	tagFilters := r.URL.Query()["tags"]
	matchAll := r.URL.Query().Get("match_all") == queryParamTrue

	// Create child span for this handler
	var span trace.Span
	if h.tracer != nil {
		ctx, span = h.tracer.Start(ctx, "listImagesHandler",
			trace.WithAttributes(
				attribute.String("handler", "list_images"),
				attribute.Bool("htmx_request", r.Header.Get("HX-Request") != ""),
				attribute.Int("tag_filters.count", len(tagFilters)),
				attribute.Bool("tag_filters.match_all", matchAll),
			),
		)
		defer span.End()
	}

	if len(tagFilters) > 0 && span != nil {
		span.SetAttributes(attribute.StringSlice("tag_filters.tags", tagFilters))
	}

	images, err := h.getStorageImages(ctx, tagFilters, matchAll)
	if err != nil {
		if span != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to get images")
		}
		if h.logger != nil {
			h.logger.Error(ctx).Err(err).Msg("Failed to list images")
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if span != nil {
		span.SetAttributes(attribute.Int("images.count", len(images)))
		span.AddEvent("images_retrieved", trace.WithAttributes(
			attribute.Int("count", len(images)),
		))
	}

	// Check if this is an HTMX request for HTML or regular JSON request
	if r.Header.Get("HX-Request") != "" {
		// Check if this is from our JavaScript filtering UI (has filtered parameter)
		if r.URL.Query().Get("filtered") == queryParamTrue {
			// Return JSON with separate HTML chunks for filters and gallery
			h.renderFilteredJSONResponse(w, images, tagFilters, matchAll)
		} else {
			// Initial page load - return plain HTML
			h.renderHTMLResponse(w, images, tagFilters, matchAll)
		}
	} else {
		h.renderJSONResponse(w, images, tagFilters, matchAll)
	}

	if span != nil {
		span.SetStatus(codes.Ok, "")
	}
}

// getStorageImages fetches images from database with metadata
func (h *Handler) getStorageImages(ctx context.Context, tagFilters []string, matchAll bool) ([]ImageResponse, error) {
	ctx, span := h.startSpan(ctx, "getStorageImages",
		attribute.Int("tag_filters.count", len(tagFilters)),
		attribute.Bool("tag_filters.match_all", matchAll),
	)
	defer h.endSpan(span)

	// Try ImageService first (preferred path with full metadata)
	if h.imageService != nil {
		return h.getImagesFromService(ctx, span, tagFilters, matchAll)
	}

	// Fallback to storage-only approach
	return h.getImagesFromStorage(ctx, span)
}

// getImagesFromService fetches images using the ImageService
func (h *Handler) getImagesFromService(ctx context.Context, span trace.Span, tagFilters []string, matchAll bool) ([]ImageResponse, error) {
	listReq := &image.ListImagesRequest{
		Page:     1,
		PageSize: 100,
		Tags:     tagFilters,
		MatchAll: matchAll,
	}

	listResp, err := h.imageService.ListImages(ctx, listReq)
	if err != nil {
		h.setSpanStatus(span, codes.Error, "failed to list images from service")
		if span != nil {
			span.RecordError(err)
		}
		return nil, fmt.Errorf("failed to list images: %v", err)
	}

	h.setSpanAttributes(span, attribute.Int("images.count", len(listResp.Images)))

	// Convert domain images to response format
	images := h.convertDomainImagesToResponse(listResp.Images)

	h.setSpanStatus(span, codes.Ok, "")
	return images, nil
}

// convertDomainImagesToResponse converts domain images to API response format
func (h *Handler) convertDomainImagesToResponse(domainImages []image.Image) []ImageResponse {
	images := make([]ImageResponse, 0, len(domainImages))
	for i := range domainImages {
		img := &domainImages[i]
		// Use proxy endpoint for reliable access from browser
		url := fmt.Sprintf("/api/images/%d/view", img.ID)

		// Extract tag names
		var tagNames []string
		for _, tag := range img.Tags {
			tagNames = append(tagNames, tag.Name)
		}

		images = append(images, ImageResponse{
			ID:          fmt.Sprintf("%d", img.ID),
			Name:        img.OriginalFilename,
			URL:         url,
			Size:        img.FileSize,
			UploadTime:  img.UploadedAt.Format("2006-01-02 15:04:05"),
			ContentType: img.ContentType,
			Width:       img.Width,
			Height:      img.Height,
			Tags:        tagNames,
		})
	}
	return images
}

// getImagesFromStorage fetches images from storage service (fallback)
func (h *Handler) getImagesFromStorage(ctx context.Context, span trace.Span) ([]ImageResponse, error) {
	storageServiceImpl, ok := h.storageService.(*implementations.StorageServiceImpl)
	if !ok {
		err := fmt.Errorf("storage service not available")
		h.setSpanStatus(span, codes.Error, "storage service unavailable")
		if span != nil {
			span.RecordError(err)
		}
		return nil, err
	}

	objects, err := storageServiceImpl.ListObjects(ctx, "", 100)
	if err != nil {
		h.setSpanStatus(span, codes.Error, "failed to list objects")
		if span != nil {
			span.RecordError(err)
		}
		return nil, fmt.Errorf("failed to list images: %v", err)
	}

	h.setSpanAttributes(span, attribute.Int("objects.total", len(objects)))

	images := h.filterImageObjects(objects)

	h.setSpanAttributes(span, attribute.Int("images.filtered", len(images)))
	h.setSpanStatus(span, codes.Ok, "")

	return images, nil
}

// filterImageObjects converts storage objects to image responses
func (h *Handler) filterImageObjects(objects []implementations.ObjectInfo) []ImageResponse {
	images := make([]ImageResponse, 0)
	for _, obj := range objects {
		if isImageFile(obj.Key, obj.ContentType) {
			url := fmt.Sprintf("/api/images/%s/view", obj.Key)
			images = append(images, ImageResponse{
				ID:         obj.Key,
				Name:       extractOriginalFilename(obj.UserMetadata, obj.Key),
				URL:        url,
				Size:       obj.Size,
				UploadTime: obj.LastModified.Format("2006-01-02 15:04:05"),
			})
		}
	}
	return images

}

// renderFilteredJSONResponse returns JSON with separate HTML chunks for filters and gallery
func (h *Handler) renderFilteredJSONResponse(w http.ResponseWriter, images []ImageResponse, tagFilters []string, matchAll bool) {
	w.Header().Set("Content-Type", "application/json")

	response := map[string]interface{}{
		"total_count": len(images),
		"images":      images,
	}

	// Generate filter panel HTML if filters are active
	if len(tagFilters) > 0 {
		response["filter_html"] = h.renderActiveFilters(tagFilters, matchAll, len(images))
	}

	// Generate gallery HTML
	response["gallery_html"] = h.renderGalleryHTML(images, tagFilters)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

// renderGalleryHTML generates the HTML for image cards
func (h *Handler) renderGalleryHTML(images []ImageResponse, tagFilters []string) string {
	if len(images) == 0 {
		emptyMsg := `<div class="col-span-full text-center text-gray-500 py-8">`
		if len(tagFilters) > 0 {
			emptyMsg += `No images found with the selected tags. <button onclick="clearFilters()" class="text-blue-500 hover:underline ml-2">Clear filters</button>`
		} else {
			emptyMsg += `No images found`
		}
		emptyMsg += `</div>`
		return emptyMsg
	}

	var html strings.Builder
	for i := range images {
		html.WriteString(h.renderImageCard(images[i]))
	}
	return html.String()
}

// renderImageCard generates HTML for a single image card
func (h *Handler) renderImageCard(img ImageResponse) string {
	metadataBadges := h.buildDimensionsBadge(img) + h.buildContentTypeBadge(img)
	tagsBadges := h.buildTagsBadges(img)

	return fmt.Sprintf(`
		<div class="image-card bg-white rounded-lg shadow-md overflow-hidden relative">
			<div class="absolute top-2 right-2 z-10">
				<button onclick="toggleImageMenu('%s')" class="bg-white bg-opacity-90 hover:bg-opacity-100 rounded-full p-1 shadow-md">
					<svg class="w-5 h-5 text-gray-700" fill="currentColor" viewBox="0 0 20 20">
						<path d="M10 6a2 2 0 110-4 2 2 0 010 4zM10 12a2 2 0 110-4 2 2 0 010 4zM10 18a2 2 0 110-4 2 2 0 010 4z"></path>
					</svg>
				</button>
				<div id="menu-%s" class="hidden absolute right-0 mt-1 w-32 bg-white rounded-md shadow-lg z-20">
					<button onclick="deleteImage('%s', '%s')" class="block w-full text-left px-4 py-2 text-sm text-red-600 hover:bg-red-50 rounded-md">
						Delete
					</button>
				</div>
			</div>
			<img src="%s" alt="%s" class="gallery-image cursor-pointer"
				 onclick="openModal('%s', '%s', %d)">
			<div class="p-3">
				<h3 class="font-medium text-gray-900 truncate mb-2">%s</h3>
				<div class="mb-2">
					%s
				</div>
				<p class="text-sm text-gray-500 mb-1">%s</p>
				<p class="text-xs text-gray-400">%s</p>
				%s
			</div>
		</div>`,
		img.ID, img.ID, img.ID, img.Name,
		img.URL, img.Name, img.URL, img.Name, img.Size,
		img.Name,
		metadataBadges,
		formatFileSize(img.Size),
		img.UploadTime,
		tagsBadges)
}

// buildDimensionsBadge creates dimensions badge HTML if width and height are present
func (h *Handler) buildDimensionsBadge(img ImageResponse) string {
	if img.Width != nil && img.Height != nil {
		return fmt.Sprintf(`<span class="inline-block bg-blue-100 text-blue-800 text-xs px-2 py-1 rounded mr-1">%d×%d</span>`, *img.Width, *img.Height)
	}
	return ""
}

// buildContentTypeBadge creates content type badge HTML
func (h *Handler) buildContentTypeBadge(img ImageResponse) string {
	if img.ContentType == "" {
		return ""
	}

	formatLabel := h.getFormatLabel(img.ContentType)
	return fmt.Sprintf(`<span class="inline-block bg-purple-100 text-purple-800 text-xs px-2 py-1 rounded mr-1">%s</span>`, formatLabel)
}

// getFormatLabel returns human-readable format label for content type
func (h *Handler) getFormatLabel(contentType string) string {
	switch contentType {
	case "image/jpeg", "image/jpg":
		return "JPEG"
	case "image/png":
		return "PNG"
	case "image/gif":
		return "GIF"
	case "image/webp":
		return "WebP"
	default:
		return "Image"
	}
}

// buildTagsBadges creates clickable tag badges HTML
func (h *Handler) buildTagsBadges(img ImageResponse) string {
	if len(img.Tags) == 0 {
		return ""
	}

	var badges strings.Builder
	for _, tag := range img.Tags {
		colorClass := settings.GetLightTagColorClass(tag)
		badges.WriteString(fmt.Sprintf(`<button onclick="filterByTag('%s')" class="inline-block %s text-xs px-2 py-1 rounded mr-1 mb-1 cursor-pointer hover:opacity-80 transition-opacity">%s</button>`,
			tag, colorClass, tag))
	}

	return fmt.Sprintf(`<div class="mt-2">%s</div>`, badges.String())
}

// renderHTMLResponse renders images as HTML for HTMX requests (initial page load)
func (h *Handler) renderHTMLResponse(w http.ResponseWriter, images []ImageResponse, tagFilters []string, matchAll bool) {
	w.Header().Set("Content-Type", "text/html")

	if len(images) == 0 {
		emptyMsg := `<div class="col-span-full text-center text-gray-500 py-8">No images found</div>`
		if _, err := w.Write([]byte(emptyMsg)); err != nil {
			http.Error(w, "Failed to write response", http.StatusInternalServerError)
		}
		return
	}

	for i := range images {
		html := h.renderImageCard(images[i])
		if _, err := w.Write([]byte(html)); err != nil {
			http.Error(w, "Failed to write response", http.StatusInternalServerError)
			return
		}
	}
}

// renderActiveFilters generates HTML for the active filters panel
func (h *Handler) renderActiveFilters(tagFilters []string, matchAll bool, resultCount int) string {
	var filterChips string
	for _, tag := range tagFilters {
		colorClass := settings.GetTagColorClass(tag)
		filterChips += fmt.Sprintf(`
			<span class="inline-flex items-center gap-1 %s px-3 py-1 rounded-full text-sm">
				%s
				<button onclick="removeTagFilter('%s')" class="hover:bg-white hover:bg-opacity-20 rounded-full p-0.5">
					<svg class="w-3 h-3" fill="currentColor" viewBox="0 0 20 20">
						<path fill-rule="evenodd" d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z" clip-rule="evenodd"></path>
					</svg>
				</button>
			</span>
		`, colorClass, tag, url.QueryEscape(tag))
	}

	matchLogic := "ANY"
	if matchAll {
		matchLogic = "ALL"
	}

	return fmt.Sprintf(`
		<div id="active-filters" class="mb-6 p-4 bg-gray-50 rounded-lg border border-gray-200">
			<div class="flex items-center justify-between flex-wrap gap-3">
				<div class="flex items-center gap-3 flex-wrap">
					<span class="text-sm font-medium text-gray-700">Filters:</span>
					%s
					<button onclick="clearFilters()" class="text-sm text-blue-600 hover:text-blue-800 hover:underline">
						Clear all
					</button>
				</div>
				<div class="flex items-center gap-3">
					<div class="text-sm text-gray-600">
						Showing <span class="font-semibold">%d</span> images with <span class="font-semibold">%s</span> tags
					</div>
					<div class="flex items-center gap-2 bg-white rounded-lg px-3 py-1.5 border border-gray-300">
						<span class="text-xs text-gray-600">Match:</span>
						<button onclick="toggleMatchMode()" class="text-xs font-medium px-2 py-0.5 rounded %s">
							%s
						</button>
					</div>
				</div>
			</div>
		</div>
	`, filterChips, resultCount, matchLogic,
		func() string {
			if matchAll {
				return "bg-blue-100 text-blue-800"
			}
			return "bg-gray-100 text-gray-700"
		}(), matchLogic)
}

// renderJSONResponse renders images as JSON for API requests
func (h *Handler) renderJSONResponse(w http.ResponseWriter, images []ImageResponse, tagFilters []string, matchAll bool) {
	w.Header().Set("Content-Type", "application/json")
	response := map[string]any{
		"images":      images,
		"total_count": len(images),
	}

	if len(tagFilters) > 0 {
		response["active_filters"] = tagFilters
		response["match_all"] = matchAll
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

func (h *Handler) getImageHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	imagePath := chi.URLParam(r, "id")

	// Create child span for this handler
	ctx, span := h.startSpan(ctx, "getImageHandler",
		attribute.String("handler", "get_image"),
		attribute.String("image.path", imagePath),
	)
	defer h.endSpan(span)

	// Check if image exists
	exists, err := h.storageService.Exists(ctx, imagePath)
	if err != nil {
		h.handleError(ctx, span, err, "Error checking image existence", "failed to check existence", imagePath)
		http.Error(w, "Error checking image", http.StatusInternalServerError)
		return
	}

	if !exists {
		h.setSpanStatus(span, codes.Error, "image not found", attribute.Bool("image.found", false))
		http.Error(w, "Image not found", http.StatusNotFound)
		return
	}

	h.setSpanAttributes(span, attribute.Bool("image.found", true))

	// Serve the image through the app's own proxy endpoint (no presigned URLs).
	viewURL := fmt.Sprintf("/api/images/%s/view", imagePath)

	// Get image metadata
	fileInfo, err := h.storageService.GetFileInfo(ctx, imagePath)
	if err != nil {
		h.handleError(ctx, span, err, "Failed to get image info", "failed to get file info", imagePath)
		http.Error(w, "Failed to get image info", http.StatusInternalServerError)
		return
	}

	h.setSpanAttributes(span,
		attribute.Int64("image.size", fileInfo.Size),
		attribute.String("image.content_type", fileInfo.ContentType),
	)
	h.addSpanEvent(span, "image_info_retrieved")
	h.setSpanStatus(span, codes.Ok, "")

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(ImageResponse{
		ID:         imagePath,
		URL:        viewURL,
		Size:       fileInfo.Size,
		UploadTime: time.Unix(fileInfo.LastModified, 0).Format("2006-01-02 15:04:05"),
	}); err != nil {
		h.handleError(ctx, span, err, "Failed to encode response", "", "")
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// Helper methods to reduce cyclomatic complexity in handlers

func (h *Handler) startSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	if h.tracer != nil {
		return h.tracer.Start(ctx, name, trace.WithAttributes(attrs...))
	}
	return ctx, nil
}

func (h *Handler) endSpan(span trace.Span) {
	if span != nil {
		span.End()
	}
}

func (h *Handler) setSpanAttributes(span trace.Span, attrs ...attribute.KeyValue) {
	if span != nil {
		span.SetAttributes(attrs...)
	}
}

func (h *Handler) addSpanEvent(span trace.Span, name string) {
	if span != nil {
		span.AddEvent(name)
	}
}

func (h *Handler) setSpanStatus(span trace.Span, code codes.Code, description string, attrs ...attribute.KeyValue) {
	if span != nil {
		if len(attrs) > 0 {
			span.SetAttributes(attrs...)
		}
		span.SetStatus(code, description)
	}
}

func (h *Handler) handleError(ctx context.Context, span trace.Span, err error, logMsg, spanMsg, imagePath string) {
	if span != nil {
		span.RecordError(err)
		if spanMsg != "" {
			span.SetStatus(codes.Error, spanMsg)
		}
	}
	if h.logger != nil && logMsg != "" {
		logger := h.logger.Error(ctx).Err(err)
		if imagePath != "" {
			logger = logger.Str("image_path", imagePath)
		}
		logger.Msg(logMsg)
	}
}

type ImageResponse struct {
	ID          string   `json:"id"`
	Name        string   `json:"name,omitempty"`
	URL         string   `json:"url"`
	Size        int64    `json:"size"`
	UploadTime  string   `json:"upload_time"`
	ContentType string   `json:"content_type,omitempty"`
	Width       *int     `json:"width,omitempty"`
	Height      *int     `json:"height,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

func isImageContentType(contentType string) bool {
	supportedTypes := map[string]bool{
		"image/jpeg": true,
		"image/jpg":  true,
		"image/png":  true,
		"image/gif":  true,
		"image/webp": true,
	}
	return supportedTypes[contentType]
}

func isImageFile(filename, contentType string) bool {
	// First check content type if it exists
	if contentType != "" && isImageContentType(contentType) {
		return true
	}

	// If content type is missing or not recognized, check file extension
	ext := strings.ToLower(filepath.Ext(filename))
	supportedExtensions := map[string]string{
		".jpg":  "image/jpeg",
		".jpeg": "image/jpeg",
		".png":  "image/png",
		".gif":  "image/gif",
		".webp": "image/webp",
	}
	_, isSupported := supportedExtensions[ext]
	return isSupported
}

func extractOriginalFilename(metadata map[string]string, objectKey string) string {
	if filename, exists := metadata["original-filename"]; exists {
		return filename
	}
	// Fallback to the object key (filename) if no metadata
	return filepath.Base(objectKey)
}

func formatFileSize(bytes int64) string {
	if bytes == 0 {
		return "0 Bytes"
	}
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	units := []string{"KB", "MB", "GB", "TB"}
	return fmt.Sprintf("%.1f %s", float64(bytes)/float64(div), units[exp])
}

// testDatabaseHandler is a test endpoint to validate observability:
// - Creates trace spans for HTTP request and database queries
// - Logs at different levels including errors
// - Demonstrates full stack tracing from HTTP → Database
func (h *Handler) testDatabaseHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Create span for this handler
	ctx, span := h.startSpan(ctx, "testDatabaseHandler",
		attribute.String("handler", "test_database"),
		attribute.String("test.type", "observability_validation"),
	)
	defer h.endSpan(span)

	// Get scenario parameter (normal, error, slow)
	scenario := r.URL.Query().Get("scenario")
	if scenario == "" {
		scenario = "normal"
	}

	h.setSpanAttributes(span, attribute.String("test.scenario", scenario))

	response := make(map[string]any)
	response["scenario"] = scenario

	switch scenario {
	case scenarioError:
		// Simulate a database error for testing error logs and traces
		h.handleErrorScenario(ctx, span, w, response)
		return
	case "slow":
		// Simulate a slow database query
		h.handleSlowScenario(ctx, span, w, response)
		return
	default:
		// Normal scenario - successful database query
		h.handleNormalScenario(ctx, span, w, response)
		return
	}
}

func (h *Handler) handleErrorScenario(ctx context.Context, span trace.Span, w http.ResponseWriter, response map[string]any) {
	// Try to query a non-existent table to trigger a database error
	var count int
	query := "SELECT COUNT(*) FROM non_existent_table_for_testing"

	h.addSpanEvent(span, "executing_error_query")

	err := h.db.QueryRowContext(ctx, query).Scan(&count)
	if err != nil {
		// This is expected - log the error and record in span
		h.handleError(ctx, span, err, "Database error (expected for testing)", "database_error_scenario", "")

		response["status"] = statusError
		response["error"] = err.Error()
		response["message"] = "Successfully generated database error for testing"

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		if encodeErr := json.NewEncoder(w).Encode(response); encodeErr != nil {
			h.logger.Error(ctx).Err(encodeErr).Msg("Failed to encode error response")
		}
		return
	}
}

func (h *Handler) handleSlowScenario(ctx context.Context, span trace.Span, w http.ResponseWriter, response map[string]any) {
	// Execute a slow query using pg_sleep
	// IMPORTANT: pg_sleep() is a void function but still needs to be scanned
	// to properly consume the result and close the connection
	query := "SELECT pg_sleep(2), NOW() as current_time"

	h.addSpanEvent(span, "executing_slow_query")

	var sleepResult any // pg_sleep returns void but must be scanned
	var currentTime string
	if err := h.db.QueryRowContext(ctx, query).Scan(&sleepResult, &currentTime); err != nil {
		h.handleError(ctx, span, err, "Slow query failed", "slow_query_error", "")
		http.Error(w, "Slow query failed", http.StatusInternalServerError)
		return
	}

	h.setSpanAttributes(span,
		attribute.String("query.type", "slow"),
		attribute.Int("query.duration_seconds", 2),
	)

	response["status"] = statusSuccess
	response["message"] = "Completed slow query (2 seconds)"
	response["database_time"] = currentTime

	h.setSpanStatus(span, codes.Ok, "")

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Error(ctx).Err(err).Msg("Failed to encode response")
	}
}

func (h *Handler) handleNormalScenario(ctx context.Context, span trace.Span, w http.ResponseWriter, response map[string]any) {
	// Execute a normal query
	query := "SELECT version(), NOW() as current_time"

	h.addSpanEvent(span, "executing_version_query")

	var version, currentTime string
	if err := h.db.QueryRowContext(ctx, query).Scan(&version, &currentTime); err != nil {
		h.handleError(ctx, span, err, "Version query failed", "version_query_error", "")
		http.Error(w, "Version query failed", http.StatusInternalServerError)
		return
	}

	h.setSpanAttributes(span,
		attribute.String("database.version", version),
		attribute.String("query.type", "normal"),
	)

	response["status"] = statusSuccess
	response["database_version"] = version
	response["database_time"] = currentTime
	response["message"] = "Database query successful"

	h.setSpanStatus(span, codes.Ok, "")

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Error(ctx).Err(err).Msg("Failed to encode response")
	}
}

// deleteImageHandler deletes an image from both database and storage
func (h *Handler) deleteImageHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Create child span for this handler
	ctx, span := h.startSpan(ctx, "DeleteImageHandler",
		attribute.String("handler", "delete_image"),
	)
	defer h.endSpan(span)

	// Parse image ID from URL
	imageIDStr := chi.URLParam(r, "id")
	imageID, err := strconv.Atoi(imageIDStr)
	if err != nil {
		h.handleError(ctx, span, err, "Invalid image ID", "invalid_id", "")
		if h.logger != nil {
			h.logger.Warn(ctx).Str("image_id", imageIDStr).Msg("Invalid image ID format")
		}
		http.Error(w, "Invalid image ID", http.StatusBadRequest)
		return
	}

	h.setSpanAttributes(span, attribute.Int("image.id", imageID))
	h.addSpanEvent(span, "deleting_image")

	if h.logger != nil {
		h.logger.Info(ctx).Int("image_id", imageID).Msg("Starting image deletion")
	}

	// Get image info before deletion for logging
	img, err := h.imageService.GetImage(ctx, imageID)
	if err != nil {
		h.handleError(ctx, span, err, "Image not found", "image_not_found", "")
		if h.logger != nil {
			h.logger.Error(ctx).Err(err).Int("image_id", imageID).Msg("Failed to find image for deletion")
		}
		http.Error(w, "Image not found", http.StatusNotFound)
		return
	}

	h.setSpanAttributes(span,
		attribute.String("image.filename", img.Filename),
		attribute.String("image.storage_path", img.StoragePath),
	)

	// Delete the image
	if err := h.imageService.DeleteImage(ctx, imageID); err != nil {
		h.handleError(ctx, span, err, "Failed to delete image", "delete_failed", "")
		if h.logger != nil {
			h.logger.Error(ctx).Err(err).
				Int("image_id", imageID).
				Str("filename", img.Filename).
				Msg("Failed to delete image")
		}
		http.Error(w, "Failed to delete image", http.StatusInternalServerError)
		return
	}

	h.setSpanStatus(span, codes.Ok, "")
	h.addSpanEvent(span, "image_deleted_successfully")

	if h.logger != nil {
		h.logger.Info(ctx).
			Int("image_id", imageID).
			Str("filename", img.Filename).
			Str("storage_path", img.StoragePath).
			Msg("Image deleted successfully")
	}

	// Return success response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"status":   "success",
		"message":  "Image deleted successfully",
		"image_id": imageID,
	}); err != nil {
		h.handleError(ctx, span, err, "Failed to encode response", "", "")
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

// getPredefinedTagsHandler returns the list of predefined tags
func (h *Handler) getPredefinedTagsHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Create child span for this handler
	ctx, span := h.startSpan(ctx, "GetPredefinedTagsHandler",
		attribute.String("handler", "get_predefined_tags"),
	)
	defer h.endSpan(span)

	if h.logger != nil {
		h.logger.Debug(ctx).Msg("Fetching predefined tags")
	}

	// Get predefined tags directly from database repository to access description field
	dbTagRepo := database.NewTagRepository(h.db)
	tags, err := dbTagRepo.GetPredefined(ctx)
	if err != nil {
		h.handleError(ctx, span, err, "Failed to get predefined tags", "fetch_failed", "")
		if h.logger != nil {
			h.logger.Error(ctx).Err(err).Msg("Failed to fetch predefined tags")
		}
		http.Error(w, "Failed to get predefined tags", http.StatusInternalServerError)
		return
	}

	h.setSpanAttributes(span, attribute.Int("tags.count", len(tags)))
	h.setSpanStatus(span, codes.Ok, "")

	if h.logger != nil {
		h.logger.Debug(ctx).Int("count", len(tags)).Msg("Predefined tags fetched successfully")
	}

	// Convert database tags to response format with colors
	type TagResponse struct {
		ID          int    `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		Color       string `json:"color"`
		ColorClass  string `json:"color_class"`
	}

	response := make([]TagResponse, len(tags))
	for i, tag := range tags {
		desc := ""
		if tag.Description != nil {
			desc = *tag.Description
		}
		response[i] = TagResponse{
			ID:          tag.ID,
			Name:        tag.Name,
			Description: desc,
			Color:       settings.GetTagColor(tag.Name),
			ColorClass:  settings.GetLightTagColorClass(tag.Name),
		}
	}

	// Return JSON response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.handleError(ctx, span, err, "Failed to encode response", "", "")
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}
