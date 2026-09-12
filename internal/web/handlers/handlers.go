package handlers

import (
	"database/sql"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"image-gallery/internal/config"
	"image-gallery/internal/domain/image"
	"image-gallery/internal/faults"
	"image-gallery/internal/observability"
	"image-gallery/internal/services"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel/trace"
)

type Handler struct {
	// Legacy fields for backward compatibility
	db     *sql.DB
	config *config.Config

	// New service-based dependencies
	container      *services.Container
	imageService   image.ImageService
	tagService     image.TagService
	storageService image.StorageService

	// Demo controls (fault injection)
	demo         *faults.Service
	demoInjector *faults.Injector

	// Observability
	tracer      trace.Tracer
	httpMetrics *observability.HTTPMetrics
	logger      *observability.Logger
}

// NewWithContainer creates a handler with the dependency injection container
func NewWithContainer(container *services.Container) *Handler {
	// Initialize observability components
	tracer := observability.GetTracer()
	meter := observability.GetMeter()

	// Create HTTP metrics (ignore error for graceful degradation)
	httpMetrics, err := observability.NewHTTPMetrics(meter)
	if err != nil {
		httpMetrics = nil
	}

	return &Handler{
		// Legacy fields for backward compatibility
		db:     container.DB(),
		config: container.Config(),

		// New service-based dependencies
		container:      container,
		imageService:   container.ImageService(),
		tagService:     container.TagService(),
		storageService: container.StorageService(),

		// Demo controls (fault injection)
		demo:         container.DemoService(),
		demoInjector: container.DemoInjector(),

		// Observability
		tracer:      tracer,
		httpMetrics: httpMetrics,
		logger:      container.Logger(),
	}
}

func (h *Handler) Routes() http.Handler {
	r := chi.NewRouter()

	// One middleware: trace continuation, route-pattern span names, semconv RED metrics.
	r.Use(observability.Middleware(h.tracer, h.httpMetrics))

	if h.demoInjector != nil {
		r.Use(h.demoInjector.Middleware) // inside the server span, so faults are recorded on it
	}

	// Standard Chi middleware
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)

	// Health check endpoints (no additional middleware for performance)
	r.Get("/healthz", h.healthzHandler)
	r.Get("/readyz", h.readyzHandler)

	// Serve static files
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("web/static"))))

	// Web routes
	r.Get("/", h.indexHandler)
	r.Get("/gallery", h.galleryHandler)

	// API routes
	r.Route("/api", func(r chi.Router) {
		r.Route("/images", func(r chi.Router) {
			r.Get("/", h.listImagesHandler)
			r.Post("/", h.uploadImagesHandler) // Upload images endpoint
			r.Get("/{id}", h.getImageHandler)
			r.Get("/{id}/view", h.viewImageHandler)           // Proxy endpoint for viewing images
			r.Get("/{id}/thumbnail", h.thumbnailImageHandler) // Thumbnail, or the original until the worker is done
			r.Delete("/{id}", h.deleteImageHandler)           // Delete image endpoint
		})
		// Settings endpoints
		r.Route("/settings", func(r chi.Router) {
			r.Get("/", h.getSettingsHandler)         // Get user settings
			r.Put("/", h.updateSettingsHandler)      // Update user settings
			r.Post("/reset", h.resetSettingsHandler) // Reset to defaults
			// Demo controls: h.demo is nil when DEMO_CONTROLS_ENABLED=false
			if h.demo != nil {
				r.Get("/demo", h.getDemoHandler)
				r.Put("/demo", h.updateDemoHandler)
				r.Post("/demo/reset", h.resetDemoHandler)
			}
		})
		// Tags endpoints
		r.Route("/tags", func(r chi.Router) {
			r.Get("/predefined", h.getPredefinedTagsHandler) // Get predefined tags
		})
		// Test endpoint for observability validation (generates traces + logs)
		r.Get("/test-db", h.testDatabaseHandler)
	})

	return r
}

// Web handlers for HTML responses

func (h *Handler) indexHandler(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/gallery", http.StatusFound)
}

// viewImageHandler serves images directly from storage (proxy endpoint)
//
//nolint:gocyclo // Handler with error handling and content type detection
func (h *Handler) viewImageHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	imageIDStr := chi.URLParam(r, "id")

	// Parse image ID
	imageID, err := strconv.Atoi(imageIDStr)
	if err != nil {
		http.Error(w, "Invalid image ID", http.StatusBadRequest)
		return
	}

	// Get image metadata from database
	if h.imageService == nil {
		http.Error(w, "Image service not available", http.StatusInternalServerError)
		return
	}

	img, err := h.imageService.GetImage(ctx, imageID)
	if err != nil {
		http.Error(w, "Image not found", http.StatusNotFound)
		return
	}

	// Get image from storage using storage path
	reader, err := h.storageService.Retrieve(ctx, img.StoragePath)
	if err != nil {
		http.Error(w, "Failed to retrieve image", http.StatusInternalServerError)
		return
	}
	defer func() { _ = reader.Close() }() //nolint:errcheck // Resource cleanup

	// Set content type from database metadata
	if img.ContentType != "" {
		w.Header().Set("Content-Type", img.ContentType)
	} else {
		// Fallback content type based on extension
		ext := strings.ToLower(filepath.Ext(img.StoragePath))
		switch ext {
		case ".jpg", ".jpeg":
			w.Header().Set("Content-Type", contentTypeJPEG)
		case extPNG:
			w.Header().Set("Content-Type", contentTypePNG)
		case extGIF:
			w.Header().Set("Content-Type", contentTypeGIF)
		case ".webp":
			w.Header().Set("Content-Type", "image/webp")
		default:
			w.Header().Set("Content-Type", "application/octet-stream")
		}
	}

	// Set cache headers
	w.Header().Set("Cache-Control", "public, max-age=3600")

	// Copy image data to response
	if _, err := io.Copy(w, reader); err != nil {
		http.Error(w, "Failed to serve image", http.StatusInternalServerError)
		return
	}
}

// thumbnailImageHandler serves the worker's thumbnail, or the original while
// the image is pending, processing or failed.
func (h *Handler) thumbnailImageHandler(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid image id", http.StatusBadRequest)
		return
	}
	img, err := h.imageService.GetImage(r.Context(), id)
	if err != nil {
		http.Error(w, "image not found", http.StatusNotFound)
		return
	}
	path, contentType := img.StoragePath, img.ContentType
	if img.ThumbnailPath != nil && *img.ThumbnailPath != "" {
		path, contentType = *img.ThumbnailPath, thumbnailContentType(*img.ThumbnailPath)
	}
	rc, err := h.storageService.Retrieve(r.Context(), path)
	if err != nil {
		http.Error(w, "image unavailable", http.StatusBadGateway)
		return
	}
	defer func() { _ = rc.Close() }() //nolint:errcheck // Resource cleanup
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = io.Copy(w, rc) //nolint:errcheck // response already committed; nothing actionable on copy failure
}

// Content types thumbnailContentType can return, kept as constants so adding
// this lookup does not push shared MIME-type literals over goconst's threshold.
const (
	contentTypePNG  = "image/png"
	contentTypeGIF  = "image/gif"
	contentTypeJPEG = "image/jpeg"
)

// File extensions matched both here and in viewImageHandler's fallback
// content-type switch above, kept as constants for the same goconst reason.
const (
	extPNG = ".png"
	extGIF = ".gif"
)

func thumbnailContentType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case extPNG:
		return contentTypePNG
	case extGIF:
		return contentTypeGIF
	default:
		return contentTypeJPEG
	}
}

func (h *Handler) galleryHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	if _, err := w.Write([]byte(`
<!DOCTYPE html>
<html>
<head>
    <title>Image Gallery</title>
    <script src="https://unpkg.com/htmx.org@2.0.3"></script>
    <link href="https://cdn.jsdelivr.net/npm/tailwindcss@2.2.19/dist/tailwind.min.css" rel="stylesheet">
    <style>
        .image-card {
            transition: transform 0.2s ease-in-out;
        }
        .image-card:hover {
            transform: scale(1.02);
        }
        .gallery-image {
            width: 100%;
            height: 200px;
            object-fit: cover;
            border-radius: 0.5rem;
        }
        .modal {
            display: none;
        }
        .modal.active {
            display: flex;
        }
    </style>
</head>
<body class="bg-gray-50" id="pageBody">
    <div class="container mx-auto px-4 py-8">
        <!-- Header with title and action buttons -->
        <div class="flex justify-between items-center mb-8">
            <h1 class="text-4xl font-bold text-gray-800">Image Gallery</h1>
            <div class="flex gap-3">
                <button onclick="openUploadModal()" class="bg-blue-500 hover:bg-blue-700 text-white font-bold py-2 px-4 rounded-lg shadow-md flex items-center gap-2">
                    <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M7 16a4 4 0 01-.88-7.903A5 5 0 1115.9 6L16 6a5 5 0 011 9.9M15 13l-3-3m0 0l-3 3m3-3v12"></path>
                    </svg>
                    Upload
                </button>
                <button onclick="openSettingsModal()" class="bg-gray-600 hover:bg-gray-800 text-white font-bold py-2 px-4 rounded-lg shadow-md flex items-center gap-2">
                    <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z"></path>
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z"></path>
                    </svg>
                    Settings
                </button>
            </div>
        </div>

        <!-- Active Filters Panel -->
        <div id="activeFilters"></div>

        <!-- Gallery Grid -->
        <div id="gallery"
             hx-get="/api/images"
             hx-trigger="load"
             hx-target="this"
             hx-swap="innerHTML"
             class="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 gap-6">
            <div class="col-span-full text-center text-gray-500">Loading images...</div>
        </div>
    </div>

    <!-- Upload Modal -->
    <div id="uploadModal" class="modal fixed inset-0 bg-black bg-opacity-75 items-center justify-center z-50">
        <div class="bg-white rounded-lg shadow-xl max-w-2xl w-full mx-4 max-h-[90vh] overflow-y-auto">
            <div class="p-6">
                <div class="flex justify-between items-center mb-4">
                    <h2 class="text-2xl font-semibold text-gray-800">Upload Images</h2>
                    <button onclick="closeUploadModal()" class="text-gray-500 hover:text-gray-700 text-2xl">&times;</button>
                </div>
                <form id="uploadForm" enctype="multipart/form-data"
                      hx-post="/api/images"
                      hx-encoding="multipart/form-data"
                      hx-target="#uploadStatus"
                      hx-swap="innerHTML">
                    <div class="mb-4">
                        <label class="block text-gray-700 text-sm font-bold mb-2" for="files">
                            Select Images (multiple files supported)
                        </label>
                        <input type="file"
                               name="files"
                               id="files"
                               multiple
                               accept="image/*"
                               required
                               class="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500">
                    </div>
                    <div class="mb-4">
                        <label class="block text-gray-700 text-sm font-bold mb-2">
                            Tags
                        </label>
                        <div id="tagsGrid" class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 gap-2 max-h-64 overflow-y-auto border border-gray-300 rounded-md p-3">
                            <div class="col-span-full text-center text-gray-500 py-4">Loading tags...</div>
                        </div>
                        <p class="text-gray-500 text-xs mt-1">Optional: Select tags for your images</p>
                        <input type="hidden" name="tags" id="selectedTags" value="">
                    </div>
                    <div class="flex items-center justify-between">
                        <button type="submit"
                                class="bg-blue-500 hover:bg-blue-700 text-white font-bold py-2 px-4 rounded focus:outline-none focus:shadow-outline">
                            Upload Images
                        </button>
                        <div id="uploadIndicator" class="htmx-indicator text-blue-500">
                            <svg class="animate-spin h-5 w-5" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
                                <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                                <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                            </svg>
                        </div>
                    </div>
                    <div id="uploadStatus" class="mt-4"></div>
                </form>
            </div>
        </div>
    </div>

    <!-- Settings Modal -->
    <div id="settingsModal" class="modal fixed inset-0 bg-black bg-opacity-75 items-center justify-center z-50">
        <div class="bg-white rounded-lg shadow-xl max-w-3xl w-full mx-4 max-h-[90vh] overflow-y-auto">
            <div class="p-6">
                <div class="flex justify-between items-center mb-6">
                    <h2 class="text-2xl font-semibold text-gray-800">Gallery Settings</h2>
                    <button onclick="closeSettingsModal()" class="text-gray-500 hover:text-gray-700 text-2xl">&times;</button>
                </div>
                <div id="settingsForm">
                    <!-- Background Settings -->
                    <div class="mb-6">
                        <h3 class="text-lg font-medium text-gray-700 mb-3">Background</h3>
                        <div class="mb-4">
                            <label class="block text-gray-700 text-sm font-bold mb-2">Background Image URL</label>
                            <input type="url" id="backgroundUrl" placeholder="https://example.com/image.jpg" class="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500">
                        </div>
                        <div class="grid grid-cols-2 gap-4">
                            <div>
                                <label class="block text-gray-700 text-sm font-bold mb-2">Background Style</label>
                                <select id="backgroundStyle" class="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500">
                                    <option value="cover">Cover</option>
                                    <option value="contain">Contain</option>
                                    <option value="repeat">Repeat</option>
                                </select>
                            </div>
                            <div>
                                <label class="block text-gray-700 text-sm font-bold mb-2">Background Opacity: <span id="opacityValue">0.30</span></label>
                                <input type="range" id="backgroundOpacity" min="0" max="1" step="0.05" value="0.30" class="w-full">
                            </div>
                        </div>
                    </div>

                    <!-- Font Settings -->
                    <div class="mb-6">
                        <h3 class="text-lg font-medium text-gray-700 mb-3">Typography</h3>
                        <div class="grid grid-cols-2 gap-4">
                            <div>
                                <label class="block text-gray-700 text-sm font-bold mb-2">Font Family</label>
                                <select id="fontFamily" class="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500">
                                    <option value="system-ui">System UI</option>
                                    <option value="Arial">Arial</option>
                                    <option value="Roboto">Roboto</option>
                                    <option value="'Open Sans'">Open Sans</option>
                                    <option value="Lato">Lato</option>
                                    <option value="Montserrat">Montserrat</option>
                                </select>
                            </div>
                            <div>
                                <label class="block text-gray-700 text-sm font-bold mb-2">Text Theme</label>
                                <select id="textTheme" class="w-full px-3 py-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-blue-500">
                                    <option value="light">Light</option>
                                    <option value="dark">Dark</option>
                                </select>
                            </div>
                        </div>
                    </div>

                    <!-- Display Settings -->
                    <div class="mb-6">
                        <h3 class="text-lg font-medium text-gray-700 mb-3">Display Options</h3>
                        <div class="space-y-2">
                            <label class="flex items-center">
                                <input type="checkbox" id="showTags" checked class="mr-2">
                                <span class="text-gray-700">Show Tags</span>
                            </label>
                            <label class="flex items-center">
                                <input type="checkbox" id="showDimensions" checked class="mr-2">
                                <span class="text-gray-700">Show Dimensions</span>
                            </label>
                            <label class="flex items-center">
                                <input type="checkbox" id="showContentType" checked class="mr-2">
                                <span class="text-gray-700">Show Content Type</span>
                            </label>
                        </div>
                    </div>

                    <!-- Grid Settings -->
                    <div class="mb-6">
                        <h3 class="text-lg font-medium text-gray-700 mb-3">Grid Layout</h3>
                        <label class="block text-gray-700 text-sm font-bold mb-2">Columns: <span id="columnsValue">5</span></label>
                        <input type="range" id="gridColumns" min="2" max="6" step="1" value="5" class="w-full">
                    </div>

                    <!-- Actions -->
                    <div class="flex justify-between">
                        <button onclick="resetSettings()" class="bg-gray-400 hover:bg-gray-500 text-white font-bold py-2 px-4 rounded">
                            Reset to Defaults
                        </button>
                        <button onclick="saveSettings()" class="bg-blue-500 hover:bg-blue-700 text-white font-bold py-2 px-4 rounded">
                            Save Settings
                        </button>
                    </div>

                    <fieldset id="demoControls" class="border border-amber-300 rounded-lg p-4 mt-6">
                        <legend class="px-2 text-sm font-semibold text-amber-700">Demo controls (fault injection)</legend>
                        <div class="grid grid-cols-2 gap-3 text-sm">
                            <label>Latency (ms)<input id="demoLatencyMs" type="number" min="0" max="30000" class="w-full border rounded px-2 py-1"></label>
                            <label>Latency probability<input id="demoLatencyProb" type="number" min="0" max="1" step="0.05" class="w-full border rounded px-2 py-1"></label>
                            <label class="col-span-2">Latency routes (comma-separated path prefixes, empty = all /api)<input id="demoLatencyRoutes" type="text" class="w-full border rounded px-2 py-1"></label>
                            <label>Error probability (5xx)<input id="demoErrorProb" type="number" min="0" max="1" step="0.05" class="w-full border rounded px-2 py-1"></label>
                            <label>Slow DB list query (ms)<input id="demoSlowDbMs" type="number" min="0" max="30000" class="w-full border rounded px-2 py-1"></label>
                            <label>Worker failure probability<input id="demoWorkerFailProb" type="number" min="0" max="1" step="0.05" class="w-full border rounded px-2 py-1"></label>
                            <label>Worker delay (ms)<input id="demoWorkerDelayMs" type="number" min="0" max="10000" class="w-full border rounded px-2 py-1"></label>
                        </div>
                        <div class="flex gap-2 mt-3">
                            <button onclick="saveDemoControls()" class="bg-amber-600 hover:bg-amber-700 text-white py-1 px-3 rounded">Apply</button>
                            <button onclick="resetDemoControls()" class="bg-gray-500 hover:bg-gray-600 text-white py-1 px-3 rounded">All off</button>
                            <span id="demoStatus" class="text-xs text-gray-500 self-center"></span>
                        </div>
                    </fieldset>
                </div>
            </div>
        </div>
    </div>

    <!-- Image viewer modal -->
    <div id="imageModal" class="modal fixed inset-0 bg-black bg-opacity-75 items-center justify-center z-50">
        <div class="max-w-4xl max-h-full p-4">
            <img id="modalImage" src="" alt="" class="max-w-full max-h-full object-contain">
            <div class="text-white text-center mt-4">
                <p id="modalImageName" class="font-semibold"></p>
                <p id="modalImageSize" class="text-sm text-gray-300"></p>
                <button onclick="closeImageModal()" class="mt-2 px-4 py-2 bg-gray-700 text-white rounded hover:bg-gray-600">Close</button>
            </div>
        </div>
    </div>

    <script>
        let currentSettings = null;

        // Load settings on page load
        async function loadSettings() {
            try {
                const response = await fetch('/api/settings');
                if (response.ok) {
                    currentSettings = await response.json();
                    applySettings(currentSettings);
                }
            } catch (error) {
                console.error('Failed to load settings:', error);
            }
        }

        // Apply settings to the page
        function applySettings(settings) {
            const body = document.getElementById('pageBody');
            const gallery = document.getElementById('gallery');

            // Apply background
            if (settings.background_image_url) {
                body.style.backgroundImage = 'url(' + settings.background_image_url + ')';
                body.style.backgroundSize = settings.background_style;
                body.style.backgroundPosition = 'center';
                body.style.backgroundAttachment = 'fixed';
                body.style.position = 'relative';

                // Add overlay for opacity
                let overlay = document.getElementById('bgOverlay');
                if (!overlay) {
                    overlay = document.createElement('div');
                    overlay.id = 'bgOverlay';
                    overlay.style.position = 'fixed';
                    overlay.style.top = '0';
                    overlay.style.left = '0';
                    overlay.style.right = '0';
                    overlay.style.bottom = '0';
                    overlay.style.backgroundColor = 'rgba(255, 255, 255, ' + (1 - settings.background_opacity) + ')';
                    overlay.style.pointerEvents = 'none';
                    overlay.style.zIndex = '-1';
                    document.body.insertBefore(overlay, document.body.firstChild);
                }
                overlay.style.backgroundColor = 'rgba(255, 255, 255, ' + (1 - settings.background_opacity) + ')';
            }

            // Apply font
            body.style.fontFamily = settings.font_family;

            // Apply text theme
            if (settings.text_theme === 'dark') {
                body.classList.remove('bg-gray-50');
                body.classList.add('bg-gray-900', 'text-white');
            } else {
                body.classList.remove('bg-gray-900', 'text-white');
                body.classList.add('bg-gray-50');
            }

            // Apply grid columns
            const gridClasses = {
                2: 'grid-cols-1 sm:grid-cols-2',
                3: 'grid-cols-1 sm:grid-cols-2 md:grid-cols-3',
                4: 'grid-cols-1 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4',
                5: 'grid-cols-1 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5',
                6: 'grid-cols-1 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4 xl:grid-cols-5 2xl:grid-cols-6'
            };
            gallery.className = 'grid gap-6 ' + (gridClasses[settings.grid_columns] || gridClasses[5]);

            // Update form values
            document.getElementById('backgroundUrl').value = settings.background_image_url || '';
            document.getElementById('backgroundStyle').value = settings.background_style;
            document.getElementById('backgroundOpacity').value = settings.background_opacity;
            document.getElementById('opacityValue').textContent = settings.background_opacity.toFixed(2);
            document.getElementById('fontFamily').value = settings.font_family;
            document.getElementById('textTheme').value = settings.text_theme;
            document.getElementById('showTags').checked = settings.show_tags;
            document.getElementById('showDimensions').checked = settings.show_dimensions;
            document.getElementById('showContentType').checked = settings.show_content_type;
            document.getElementById('gridColumns').value = settings.grid_columns;
            document.getElementById('columnsValue').textContent = settings.grid_columns;
        }

        // Save settings
        async function saveSettings() {
            const settings = {
                background_image_url: document.getElementById('backgroundUrl').value || null,
                background_style: document.getElementById('backgroundStyle').value,
                background_opacity: parseFloat(document.getElementById('backgroundOpacity').value),
                font_family: document.getElementById('fontFamily').value,
                text_theme: document.getElementById('textTheme').value,
                show_tags: document.getElementById('showTags').checked,
                show_dimensions: document.getElementById('showDimensions').checked,
                show_content_type: document.getElementById('showContentType').checked,
                grid_columns: parseInt(document.getElementById('gridColumns').value)
            };

            try {
                const response = await fetch('/api/settings', {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(settings)
                });

                if (response.ok) {
                    currentSettings = await response.json();
                    applySettings(currentSettings);
                    closeSettingsModal();
                    alert('Settings saved successfully!');
                } else {
                    alert('Failed to save settings');
                }
            } catch (error) {
                console.error('Failed to save settings:', error);
                alert('Failed to save settings');
            }
        }

        // Reset settings
        async function resetSettings() {
            if (!confirm('Reset all settings to defaults?')) return;

            try {
                const response = await fetch('/api/settings/reset', { method: 'POST' });
                if (response.ok) {
                    currentSettings = await response.json();
                    applySettings(currentSettings);
                    alert('Settings reset to defaults!');
                } else {
                    alert('Failed to reset settings');
                }
            } catch (error) {
                console.error('Failed to reset settings:', error);
                alert('Failed to reset settings');
            }
        }

        // Modal functions
        function openUploadModal() {
            document.getElementById('uploadModal').classList.add('active');
            loadPredefinedTags();
        }

        function closeUploadModal() {
            document.getElementById('uploadModal').classList.remove('active');
            document.getElementById('uploadForm').reset();
            document.getElementById('uploadStatus').innerHTML = '';
            document.getElementById('selectedTags').value = '';
        }

        // Load predefined tags from API
        async function loadPredefinedTags() {
            const tagsGrid = document.getElementById('tagsGrid');
            try {
                const response = await fetch('/api/tags/predefined');
                if (!response.ok) {
                    throw new Error('Failed to load tags');
                }
                const tags = await response.json();
                renderTagsGrid(tags);
            } catch (error) {
                console.error('Failed to load predefined tags:', error);
                tagsGrid.innerHTML = '<div class="col-span-full text-center text-red-500 py-4">Failed to load tags</div>';
            }
        }

        // Render tags as checkbox grid
        function renderTagsGrid(tags) {
            const tagsGrid = document.getElementById('tagsGrid');
            if (tags.length === 0) {
                tagsGrid.innerHTML = '<div class="col-span-full text-center text-gray-500 py-4">No predefined tags available</div>';
                return;
            }

            let html = '';
            tags.forEach(tag => {
                const desc = tag.description || tag.name;
                html += '<label class="flex items-center gap-2 ' + tag.color_class + ' px-3 py-2 rounded cursor-pointer hover:opacity-80 transition-opacity">' +
                    '<input type="checkbox" class="tag-checkbox" value="' + tag.name + '" onchange="updateSelectedTags()" title="' + desc + '">' +
                    '<span class="text-sm font-medium">' + tag.name + '</span>' +
                    '</label>';
            });
            tagsGrid.innerHTML = html;
        }

        // Update hidden input with selected tags
        function updateSelectedTags() {
            const checkboxes = document.querySelectorAll('.tag-checkbox:checked');
            const selectedTags = Array.from(checkboxes).map(cb => cb.value);
            document.getElementById('selectedTags').value = selectedTags.join(',');
        }

        function openSettingsModal() {
            loadDemoControls();
            document.getElementById('settingsModal').classList.add('active');
        }

        const demoFields = {
            latency_ms: ['demoLatencyMs', Number], latency_probability: ['demoLatencyProb', Number],
            error_probability: ['demoErrorProb', Number], slow_db_ms: ['demoSlowDbMs', Number],
            worker_failure_probability: ['demoWorkerFailProb', Number], worker_delay_ms: ['demoWorkerDelayMs', Number],
        };
        function fillDemoControls(c) {
            for (const [key, [id]] of Object.entries(demoFields)) document.getElementById(id).value = c[key] ?? 0;
            document.getElementById('demoLatencyRoutes').value = (c.latency_routes || []).join(', ');
        }
        async function loadDemoControls() {
            const r = await fetch('/api/settings/demo');
            if (r.ok) fillDemoControls(await r.json());
            else if (r.status === 404) document.getElementById('demoControls').hidden = true; // DEMO_CONTROLS_ENABLED=false
        }
        async function saveDemoControls() {
            const body = { latency_routes: document.getElementById('demoLatencyRoutes').value.split(',').map(s => s.trim()).filter(Boolean) };
            for (const [key, [id, cast]] of Object.entries(demoFields)) body[key] = cast(document.getElementById(id).value || 0);
            const r = await fetch('/api/settings/demo', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
            document.getElementById('demoStatus').textContent = r.ok ? 'Applied' : 'Rejected: ' + await r.text();
            if (r.ok) fillDemoControls(await r.json());
        }
        async function resetDemoControls() {
            const r = await fetch('/api/settings/demo/reset', { method: 'POST' });
            document.getElementById('demoStatus').textContent = r.ok ? 'All off' : 'Reset failed';
            if (r.ok) fillDemoControls(await r.json());
        }

        function closeSettingsModal() {
            document.getElementById('settingsModal').classList.remove('active');
        }

        function openModal(imageUrl, imageName, imageSize) {
            document.getElementById('modalImage').src = imageUrl;
            document.getElementById('modalImageName').textContent = imageName;
            document.getElementById('modalImageSize').textContent = 'Size: ' + formatFileSize(imageSize);
            document.getElementById('imageModal').classList.add('active');
        }

        function closeImageModal() {
            document.getElementById('imageModal').classList.remove('active');
        }

        function formatFileSize(bytes) {
            if (bytes === 0) return '0 Bytes';
            const k = 1024;
            const sizes = ['Bytes', 'KB', 'MB', 'GB'];
            const i = Math.floor(Math.log(bytes) / Math.log(k));
            return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
        }

        // Update slider values
        document.getElementById('backgroundOpacity').addEventListener('input', function(e) {
            document.getElementById('opacityValue').textContent = parseFloat(e.target.value).toFixed(2);
        });

        document.getElementById('gridColumns').addEventListener('input', function(e) {
            document.getElementById('columnsValue').textContent = e.target.value;
        });

        // Close modals on escape key
        document.addEventListener('keydown', function(event) {
            if (event.key === 'Escape') {
                closeImageModal();
                closeUploadModal();
                closeSettingsModal();
            }
        });

        // Close modals on click outside
        ['uploadModal', 'settingsModal', 'imageModal'].forEach(function(modalId) {
            document.getElementById(modalId).addEventListener('click', function(event) {
                if (event.target === this) {
                    this.classList.remove('active');
                }
            });
        });

        // Handle upload response
        document.body.addEventListener('htmx:afterRequest', function(event) {
            if (event.detail.pathInfo.requestPath === '/api/images' && event.detail.xhr.status >= 200 && event.detail.xhr.status < 300) {
                try {
                    const response = JSON.parse(event.detail.xhr.responseText);
                    const statusDiv = document.getElementById('uploadStatus');

                    if (response.count > 0) {
                        statusDiv.innerHTML = '<div class="bg-green-100 border border-green-400 text-green-700 px-4 py-3 rounded relative" role="alert">' +
                            '<strong class="font-bold">Success!</strong> ' +
                            '<span class="block sm:inline">Uploaded ' + response.count + ' image(s).</span>' +
                            '</div>';

                        // Reset form
                        document.getElementById('uploadForm').reset();

                        // Refresh gallery
                        htmx.trigger('#gallery', 'load');

                        // Clear status and close modal after 2 seconds
                        setTimeout(function() {
                            statusDiv.innerHTML = '';
                            closeUploadModal();
                        }, 2000);
                    }

                    if (response.errors && response.errors.length > 0) {
                        let errorHtml = '<div class="bg-yellow-100 border border-yellow-400 text-yellow-700 px-4 py-3 rounded relative mt-2" role="alert">' +
                            '<strong class="font-bold">Some files failed:</strong><ul class="list-disc list-inside">';
                        response.errors.forEach(function(error) {
                            errorHtml += '<li>' + error.filename + ': ' + error.error + '</li>';
                        });
                        errorHtml += '</ul></div>';
                        statusDiv.innerHTML += errorHtml;
                    }
                } catch (e) {
                    console.error('Failed to parse upload response:', e);
                }
            }
        });

        // Handle upload errors
        document.body.addEventListener('htmx:responseError', function(event) {
            if (event.detail.pathInfo.requestPath === '/api/images') {
                const statusDiv = document.getElementById('uploadStatus');
                statusDiv.innerHTML = '<div class="bg-red-100 border border-red-400 text-red-700 px-4 py-3 rounded relative" role="alert">' +
                    '<strong class="font-bold">Error!</strong> ' +
                    '<span class="block sm:inline">Upload failed. Please try again.</span>' +
                    '</div>';
            }
        });

        // Toggle image menu (3-dot menu)
        function toggleImageMenu(imageId) {
            const menu = document.getElementById('menu-' + imageId);
            if (menu) {
                // Close all other menus first
                document.querySelectorAll('[id^="menu-"]').forEach(function(m) {
                    if (m.id !== 'menu-' + imageId) {
                        m.classList.add('hidden');
                    }
                });
                // Toggle this menu
                menu.classList.toggle('hidden');
            }
        }

        // Close menus when clicking outside
        document.addEventListener('click', function(event) {
            if (!event.target.closest('button[onclick^="toggleImageMenu"]') &&
                !event.target.closest('[id^="menu-"]')) {
                document.querySelectorAll('[id^="menu-"]').forEach(function(m) {
                    m.classList.add('hidden');
                });
            }
        });

        // Delete image with confirmation
        async function deleteImage(imageId, imageName) {
            // Close the menu
            const menu = document.getElementById('menu-' + imageId);
            if (menu) {
                menu.classList.add('hidden');
            }

            // Show confirmation dialog
            if (!confirm('Are you sure you want to delete "' + imageName + '"? This action cannot be undone.')) {
                return;
            }

            try {
                const response = await fetch('/api/images/' + imageId, {
                    method: 'DELETE'
                });

                if (response.ok) {
                    // Force refresh the gallery by making a new GET request
                    const galleryElement = document.getElementById('gallery');
                    const galleryResponse = await fetch('/api/images', {
                        headers: {
                            'HX-Request': 'true'
                        }
                    });

                    if (galleryResponse.ok) {
                        const html = await galleryResponse.text();
                        galleryElement.innerHTML = html;
                    }

                    console.log('Image deleted successfully');
                } else {
                    const error = await response.text();
                    alert('Failed to delete image: ' + error);
                }
            } catch (error) {
                console.error('Failed to delete image:', error);
                alert('Failed to delete image. Please try again.');
            }
        }

        // Tag filtering state management
        let activeTagFilters = [];
        let matchAllTags = false;

        // Filter by clicking a tag
        function filterByTag(tagName) {
            if (!activeTagFilters.includes(tagName)) {
                activeTagFilters.push(tagName);
                refreshGalleryWithFilters();
            }
        }

        // Remove a tag from active filters
        function removeTagFilter(tagName) {
            activeTagFilters = activeTagFilters.filter(t => t !== decodeURIComponent(tagName));
            refreshGalleryWithFilters();
        }

        // Clear all filters
        function clearFilters() {
            activeTagFilters = [];
            refreshGalleryWithFilters();
        }

        // Toggle between ANY/ALL match mode
        function toggleMatchMode() {
            matchAllTags = !matchAllTags;
            refreshGalleryWithFilters();
        }

        // Refresh gallery with current filter state
        function refreshGalleryWithFilters() {
            const galleryElement = document.getElementById('gallery');
            const filtersElement = document.getElementById('activeFilters');
            const params = new URLSearchParams();

            // Always add filtered=true to indicate this is from filtering UI
            params.append('filtered', 'true');

            if (activeTagFilters.length > 0) {
                activeTagFilters.forEach(tag => params.append('tags', tag));
                if (matchAllTags) {
                    params.append('match_all', 'true');
                }
            }

            let url = '/api/images?' + params.toString();

            fetch(url, {
                headers: { 'HX-Request': 'true' }
            })
            .then(response => response.json())
            .then(data => {
                // Update filters panel
                if (data.filter_html) {
                    filtersElement.innerHTML = data.filter_html;
                } else {
                    filtersElement.innerHTML = '';
                }

                // Update gallery with images
                if (data.gallery_html) {
                    galleryElement.innerHTML = data.gallery_html;
                } else if (data.images && data.images.length === 0) {
                    const emptyMsg = activeTagFilters.length > 0 ?
                        '<div class="col-span-full text-center text-gray-500 py-8">No images found with the selected tags. <button onclick="clearFilters()" class="text-blue-500 hover:underline ml-2">Clear filters</button></div>' :
                        '<div class="col-span-full text-center text-gray-500 py-8">No images found</div>';
                    galleryElement.innerHTML = emptyMsg;
                }

                // Update URL without reload
                const newUrl = activeTagFilters.length > 0 ?
                    '/gallery?' + new URLSearchParams(activeTagFilters.map(t => ['tags', t])).toString() +
                    (matchAllTags ? '&match_all=true' : '') :
                    '/gallery';
                window.history.pushState({}, '', newUrl);
            });
        }

        // Load filters from URL on page load
        function loadFiltersFromURL() {
            const params = new URLSearchParams(window.location.search);
            activeTagFilters = params.getAll('tags');
            matchAllTags = params.get('match_all') === 'true';

            if (activeTagFilters.length > 0) {
                refreshGalleryWithFilters();
            }
        }

        // Load settings and filters on page load
        loadSettings();
        loadFiltersFromURL();
    </script>
</body>
</html>
	`)); err != nil {
		http.Error(w, "Failed to write response", http.StatusInternalServerError)
		return
	}
}
