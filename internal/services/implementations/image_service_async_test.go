package implementations

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"image-gallery/internal/config"
	"image-gallery/internal/domain/image"
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

func newAsyncService(t *testing.T, repo *fakeRepo, pub image.JobPublisher) image.ImageService {
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
