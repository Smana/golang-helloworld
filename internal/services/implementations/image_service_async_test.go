package implementations

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"image-gallery/internal/config"
	"image-gallery/internal/domain/image"
	"image-gallery/internal/observability"
	"image-gallery/internal/platform/storage"
	"image-gallery/internal/platform/storage/storetest"
)

type fakeRepo struct {
	byID   map[int]*image.Image
	nextID int
}

func newFakeRepo() *fakeRepo { return &fakeRepo{byID: map[int]*image.Image{}} }

func (f *fakeRepo) Create(_ context.Context, img *image.Image) error {
	f.nextID++
	img.ID = f.nextID
	cp := *img
	f.byID[img.ID] = &cp
	return nil
}
func (f *fakeRepo) GetByID(_ context.Context, id int) (*image.Image, error) {
	if img, ok := f.byID[id]; ok {
		cp := *img
		return &cp, nil
	}
	return nil, errors.New("not found")
}
func (f *fakeRepo) UpdateStatus(_ context.Context, id int, status string, msg *string) error {
	f.byID[id].Status, f.byID[id].ProcessingError = status, msg
	return nil
}
func (f *fakeRepo) CompleteProcessing(context.Context, int, image.ProcessingResult) error { return nil }
func (f *fakeRepo) List(context.Context, *image.ListImagesRequest) (*image.ListImagesResponse, error) {
	return &image.ListImagesResponse{}, nil
}
func (f *fakeRepo) Update(context.Context, *image.Image) error { return nil }
func (f *fakeRepo) Delete(context.Context, int) error          { return nil }
func (f *fakeRepo) GetByFilename(context.Context, string) (*image.Image, error) {
	return nil, errors.New("nf")
}
func (f *fakeRepo) ExistsByFilename(context.Context, string) (bool, error) { return false, nil }
func (f *fakeRepo) CountByTag(context.Context, string) (int, error)        { return 0, nil }
func (f *fakeRepo) GetStats(context.Context) (*image.ImageStats, error)    { return nil, errors.New("nf") }

type fakePublisher struct {
	err    error
	gotID  int
	gotKey string
}

func (p *fakePublisher) PublishProcessImage(_ context.Context, id int, key string) error {
	p.gotID, p.gotKey = id, key
	return p.err
}

// statusDownRepo cannot update a status, as when Postgres fails mid-request.
type statusDownRepo struct{ *fakeRepo }

func (statusDownRepo) UpdateStatus(context.Context, int, string, *string) error {
	return errors.New("postgres down")
}

func newAsyncService(t *testing.T, repo image.Repository, pub image.JobPublisher) image.ImageService {
	t.Helper()
	svc, err := storage.NewService(&config.StorageConfig{BucketName: "b", MaxUploadSize: 10 << 20}, storetest.NewMemStore())
	if err != nil {
		t.Fatal(err)
	}
	s := NewImageService(repo, nil, NewStorageService(svc), NewImageProcessor(), NewValidationService(), nil, nil)
	s.(*ImageServiceImpl).SetJobPublisher(pub)
	return s
}

func createReq(data []byte) *image.CreateImageRequest {
	return &image.CreateImageRequest{OriginalFilename: "cat.png", ContentType: "image/png", FileSize: int64(len(data))}
}

func TestCreateImageIsPendingAndEnqueued(t *testing.T) {
	repo, pub := newFakeRepo(), &fakePublisher{}
	data := tinyPNG(t)
	img, err := newAsyncService(t, repo, pub).CreateImage(context.Background(), createReq(data), bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if img.Status != image.StatusPending || repo.byID[img.ID].Status != image.StatusPending {
		t.Fatalf("status = %q (stored %q), want pending", img.Status, repo.byID[img.ID].Status)
	}
	if pub.gotID != img.ID || pub.gotKey != img.StoragePath {
		t.Fatalf("published (%d, %q), want (%d, %q)", pub.gotID, pub.gotKey, img.ID, img.StoragePath)
	}
}

// TestCreateImageMarksFailedWithoutJobPublisher pins the contract R29 relies
// on: a web role bootstrapped with no Valkey (CACHE_ENABLED=false, or Valkey
// simply unreachable) must still accept uploads. No job is ever published for
// such an image and nothing rescans pending rows, so it is marked failed, the
// same as when a publish fails, rather than left pending forever.
func TestCreateImageMarksFailedWithoutJobPublisher(t *testing.T) {
	repo := newFakeRepo()
	data := tinyPNG(t)
	img, err := newAsyncService(t, repo, nil).CreateImage(context.Background(), createReq(data), bytes.NewReader(data))
	if err != nil {
		t.Fatalf("upload with no job publisher configured should still succeed, got %v", err)
	}
	stored := repo.byID[img.ID]
	if img.Status != image.StatusFailed || stored.Status != image.StatusFailed || stored.ProcessingError == nil {
		t.Fatalf("want failed with an error message, got %+v", stored)
	}
}

func TestCreateImageMarksFailedWhenEnqueueFails(t *testing.T) {
	repo, pub := newFakeRepo(), &fakePublisher{err: errors.New("valkey down")}
	data := tinyPNG(t)
	img, err := newAsyncService(t, repo, pub).CreateImage(context.Background(), createReq(data), bytes.NewReader(data))
	if err != nil {
		t.Fatalf("the upload itself succeeded, got %v", err)
	}
	stored := repo.byID[img.ID]
	if img.Status != image.StatusFailed || stored.Status != image.StatusFailed || stored.ProcessingError == nil {
		t.Fatalf("want failed with an error message, got %+v", stored)
	}
}

// TestCreateImageReportsAnUnrecordedFailure covers the error path of the error path: the
// publish fails and marking the image failed fails too, so the row stays pending. That
// second error must reach the span and the log rather than vanish.
func TestCreateImageReportsAnUnrecordedFailure(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp)))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	var logs bytes.Buffer

	s := newAsyncService(t, statusDownRepo{newFakeRepo()}, &fakePublisher{err: errors.New("valkey down")})
	s.(*ImageServiceImpl).SetLogger(observability.NewLoggerTo(&logs, observability.Config{}))
	data := tinyPNG(t)
	if _, err := s.CreateImage(context.Background(), createReq(data), bytes.NewReader(data)); err != nil {
		t.Fatalf("the upload itself succeeded, got %v", err)
	}

	var recorded bool
	for _, span := range exp.GetSpans() {
		for _, ev := range span.Events {
			for _, kv := range ev.Attributes {
				if kv.Key == "exception.message" && strings.Contains(kv.Value.AsString(), "postgres down") {
					recorded = true
				}
			}
		}
	}
	if !recorded {
		t.Errorf("the failed status update is not recorded on any span")
	}
	if !strings.Contains(logs.String(), "postgres down") {
		t.Errorf("the failed status update is not logged; log output: %q", logs.String())
	}
}
