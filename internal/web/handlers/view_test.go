package handlers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/trace"

	"image-gallery/internal/domain/image"
)

// ctxImages and ctxStorage record the context each call received.
type ctxImages struct {
	stubImages
	got context.Context
}

func (s *ctxImages) GetImage(ctx context.Context, id int) (*image.Image, error) {
	s.got = ctx
	return s.stubImages.GetImage(ctx, id)
}

type ctxStorage struct {
	stubStorage
	got context.Context
}

func (s *ctxStorage) Retrieve(ctx context.Context, p string) (io.ReadCloser, error) {
	s.got = ctx
	return s.stubStorage.Retrieve(ctx, p)
}

func TestViewImageRunsInTheRequestTrace(t *testing.T) {
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{0x0a, 0x0b},
		SpanID:     trace.SpanID{0x01},
		TraceFlags: trace.FlagsSampled,
	})
	images := &ctxImages{stubImages: stubImages{img: &image.Image{ID: 1, StoragePath: "orig.png", ContentType: "image/png"}}}
	store := &ctxStorage{stubStorage: stubStorage{objs: map[string][]byte{"orig.png": []byte("ORIGINAL")}}}
	h := &Handler{imageService: images, storageService: store}

	r := chi.NewRouter()
	r.Get("/api/images/{id}/view", h.viewImageHandler)
	req := httptest.NewRequest(http.MethodGet, "/api/images/1/view", http.NoBody)
	req = req.WithContext(trace.ContextWithSpanContext(req.Context(), sc))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	for name, ctx := range map[string]context.Context{"GetImage": images.got, "Retrieve": store.got} {
		if got := trace.SpanContextFromContext(ctx); got.TraceID() != sc.TraceID() || got.SpanID() != sc.SpanID() {
			t.Errorf("%s ran outside the request span: trace %s span %s", name, got.TraceID(), got.SpanID())
		}
	}
}
