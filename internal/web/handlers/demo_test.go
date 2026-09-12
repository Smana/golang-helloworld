package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"image-gallery/internal/domain/demo"
	"image-gallery/internal/faults"
)

type memDemoRepo struct{ c demo.Controls }

func (m *memDemoRepo) Get(context.Context) (demo.Controls, error) { return m.c, nil }
func (m *memDemoRepo) Save(_ context.Context, c demo.Controls) (demo.Controls, error) {
	m.c = c
	return c, nil
}

func TestDemoEndpoints(t *testing.T) {
	h := &Handler{demo: faults.NewService(&memDemoRepo{}, nil)}
	r := chi.NewRouter()
	r.Get("/api/settings/demo", h.getDemoHandler)
	r.Put("/api/settings/demo", h.updateDemoHandler)
	r.Post("/api/settings/demo/reset", h.resetDemoHandler)
	call := func(method, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		path := "/api/settings/demo"
		if method == http.MethodPost {
			path += "/reset"
		}
		r.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
		return rec
	}
	if rec := call(http.MethodPut, `{"error_probability": 2}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid PUT = %d, want 400", rec.Code)
	}
	if rec := call(http.MethodPut, `{"error_probability": 0.25, "latency_routes": ["/api/images"]}`); rec.Code != 200 {
		t.Fatalf("valid PUT = %d", rec.Code)
	}
	var c demo.Controls
	_ = json.NewDecoder(call(http.MethodGet, "").Body).Decode(&c)
	if c.ErrorProbability != 0.25 || len(c.LatencyRoutes) != 1 {
		t.Fatalf("GET after PUT = %+v", c)
	}
	if rec := call(http.MethodPut, `{"slow_db_ms": 20}`); rec.Code != 200 {
		t.Fatalf("partial PUT = %d", rec.Code)
	}
	_ = json.NewDecoder(call(http.MethodGet, "").Body).Decode(&c)
	if c.SlowDBMS != 20 || c.ErrorProbability != 0.25 || len(c.LatencyRoutes) != 1 {
		t.Fatalf("partial PUT must merge onto existing controls, got %+v", c)
	}
	_ = json.NewDecoder(call(http.MethodPost, "").Body).Decode(&c)
	if c.Active() {
		t.Fatalf("reset left controls on: %+v", c)
	}
}
