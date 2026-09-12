package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"image-gallery/internal/config"
	"image-gallery/internal/platform/storage/storetest"
	"image-gallery/internal/services"
)

func demoSwitchRoutes(t *testing.T, enabled bool) (http.Handler, *services.Container) {
	t.Helper()
	cfg := &config.Config{DemoControlsEnabled: enabled, Storage: config.StorageConfig{BucketName: "b", MaxUploadSize: 10 << 20}}
	c, err := services.NewContainer(cfg, nil, storetest.NewMemStore())
	if err != nil {
		t.Fatal(err)
	}
	return NewWithContainer(c).Routes(), c
}

func registeredRoutes(t *testing.T, routes http.Handler) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	_ = chi.Walk(routes.(chi.Routes), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error { //nolint:errcheck // the walk callback never fails
		out[method+" "+route] = true
		return nil
	})
	return out
}

// GET /api/images/{id} looked the segment up as a storage path and answered
// with /api/images/<storage path>/view, a URL the numeric /{id}/view route
// can never match. Nothing called it, so it is gone.
func TestNoStoragePathImageLookupRoute(t *testing.T) {
	routes, _ := demoSwitchRoutes(t, false)
	got := registeredRoutes(t, routes)
	if got["GET /api/images/{id}"] {
		t.Error("GET /api/images/{id} is registered")
	}
	if !got["GET /api/images/{id}/view"] || !got["DELETE /api/images/{id}"] {
		t.Errorf("sibling image routes missing: %v", got)
	}
}

func TestDemoControlsSwitchedOff(t *testing.T) {
	routes, c := demoSwitchRoutes(t, false)
	if c.DemoInjector() != nil || c.DemoService() != nil {
		t.Error("demo controls are off, but the injector or its service was built")
	}
	for _, rq := range [][2]string{
		{http.MethodGet, "/api/settings/demo"},
		{http.MethodPut, "/api/settings/demo"},
		{http.MethodPost, "/api/settings/demo/reset"},
	} {
		rec := httptest.NewRecorder()
		routes.ServeHTTP(rec, httptest.NewRequest(rq[0], rq[1], http.NoBody))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d, want 404", rq[0], rq[1], rec.Code)
		}
	}
}

func TestDemoControlsSwitchedOn(t *testing.T) {
	routes, c := demoSwitchRoutes(t, true)
	if c.DemoInjector() == nil {
		t.Error("demo controls are on, but no injector was built")
	}
	var demoRoutes int
	for r := range registeredRoutes(t, routes) {
		if strings.Contains(r, " /api/settings/demo") {
			demoRoutes++
		}
	}
	if demoRoutes != 3 {
		t.Errorf("registered %d demo routes, want 3", demoRoutes)
	}
}
