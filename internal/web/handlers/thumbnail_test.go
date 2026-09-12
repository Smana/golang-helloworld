package handlers

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"image-gallery/internal/domain/image"
)

type stubImages struct{ img *image.Image }

func (s stubImages) GetImage(context.Context, int) (*image.Image, error) {
	if s.img == nil {
		return nil, errors.New("not found")
	}
	return s.img, nil
}
func (stubImages) CreateImage(context.Context, *image.CreateImageRequest, io.Reader) (*image.Image, error) {
	return nil, errors.New("unused")
}
func (stubImages) ListImages(context.Context, *image.ListImagesRequest) (*image.ListImagesResponse, error) {
	return nil, errors.New("unused")
}
func (stubImages) UpdateImage(context.Context, int, *image.UpdateImageRequest) (*image.Image, error) {
	return nil, errors.New("unused")
}
func (stubImages) DeleteImage(context.Context, int) error { return errors.New("unused") }
func (stubImages) DownloadImage(context.Context, int) (io.ReadCloser, string, error) {
	return nil, "", errors.New("unused")
}
func (stubImages) GetImageStats(context.Context) (*image.ImageStats, error) {
	return nil, errors.New("unused")
}

type stubStorage struct{ objs map[string][]byte }

func (s stubStorage) Retrieve(_ context.Context, p string) (io.ReadCloser, error) {
	b, ok := s.objs[p]
	if !ok {
		return nil, errors.New("missing")
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}
func (stubStorage) Store(context.Context, string, string, io.Reader, int64) (string, error) {
	return "", nil
}
func (stubStorage) StoreAt(context.Context, string, string, io.Reader, int64) error { return nil }
func (stubStorage) Delete(context.Context, string) error                            { return nil }
func (stubStorage) Exists(context.Context, string) (bool, error)                    { return true, nil }
func (stubStorage) GetFileInfo(context.Context, string) (*image.FileInfo, error)    { return nil, nil }

func serveThumb(t *testing.T, img *image.Image) *httptest.ResponseRecorder {
	t.Helper()
	h := &Handler{imageService: stubImages{img: img}, storageService: stubStorage{objs: map[string][]byte{
		"orig.png": []byte("ORIGINAL"), "thumbnails/1.jpg": []byte("THUMB"),
	}}}
	r := chi.NewRouter()
	r.Get("/api/images/{id}/thumbnail", h.thumbnailImageHandler)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/images/1/thumbnail", http.NoBody))
	return rec
}

func TestThumbnailServesThumbnailWhenReady(t *testing.T) {
	thumb := "thumbnails/1.jpg"
	rec := serveThumb(t, &image.Image{ID: 1, StoragePath: "orig.png", ContentType: "image/png", ThumbnailPath: &thumb, Status: image.StatusReady})
	if rec.Code != 200 || rec.Body.String() != "THUMB" || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("got %d %q %q", rec.Code, rec.Body.String(), rec.Header().Get("Content-Type"))
	}
}

func TestThumbnailFallsBackToOriginalWhilePending(t *testing.T) {
	rec := serveThumb(t, &image.Image{ID: 1, StoragePath: "orig.png", ContentType: "image/png", Status: image.StatusPending})
	if rec.Code != 200 || rec.Body.String() != "ORIGINAL" || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("got %d %q %q", rec.Code, rec.Body.String(), rec.Header().Get("Content-Type"))
	}
}
