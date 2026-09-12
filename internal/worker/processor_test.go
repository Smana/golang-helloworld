package worker

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"image-gallery/internal/config"
	domain "image-gallery/internal/domain/image"
	"image-gallery/internal/platform/queue"
	"image-gallery/internal/platform/storage"
	"image-gallery/internal/platform/storage/storetest"
	"image-gallery/internal/services/implementations"
)

type fakeImages struct {
	img    domain.Image
	result *domain.ProcessingResult
	errMsg *string
}

func (f *fakeImages) GetByID(context.Context, int) (*domain.Image, error) {
	cp := f.img
	return &cp, nil
}
func (f *fakeImages) UpdateStatus(_ context.Context, _ int, s string, msg *string) error {
	f.img.Status, f.errMsg = s, msg
	return nil
}
func (f *fakeImages) CompleteProcessing(_ context.Context, _ int, r domain.ProcessingResult) error {
	f.img.Status, f.result = domain.StatusReady, &r
	return nil
}
func (f *fakeImages) Create(context.Context, *domain.Image) error { return nil }
func (f *fakeImages) List(context.Context, *domain.ListImagesRequest) (*domain.ListImagesResponse, error) {
	return nil, nil
}
func (f *fakeImages) Update(context.Context, *domain.Image) error                  { return nil }
func (f *fakeImages) Delete(context.Context, int) error                            { return nil }
func (f *fakeImages) GetByFilename(context.Context, string) (*domain.Image, error) { return nil, nil }
func (f *fakeImages) ExistsByFilename(context.Context, string) (bool, error)       { return false, nil }
func (f *fakeImages) CountByTag(context.Context, string) (int, error)              { return 0, nil }
func (f *fakeImages) GetStats(context.Context) (*domain.ImageStats, error)         { return nil, nil }

type faultFn func(context.Context) error

func (f faultFn) WorkerFault(ctx context.Context) error { return f(ctx) }

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{G: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func setup(t *testing.T, status string, faults Faults) (*Processor, *fakeImages, *storetest.MemStore, *tracetest.InMemoryExporter) {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp)))
	mem := storetest.NewMemStore()
	data := pngBytes(t, 64, 48)
	if err := mem.Put(context.Background(), "aa/bb/cat.png", "image/png", bytes.NewReader(data), int64(len(data)), nil); err != nil {
		t.Fatal(err)
	}
	svc, err := storage.NewService(&config.StorageConfig{BucketName: "b", MaxUploadSize: 10 << 20}, mem)
	if err != nil {
		t.Fatal(err)
	}
	imgs := &fakeImages{img: domain.Image{ID: 1, StoragePath: "aa/bb/cat.png", ContentType: "image/png", Status: status}}
	return NewProcessor(imgs, implementations.NewStorageService(svc), faults, nil), imgs, mem, exp
}

func job() queue.Job {
	return queue.Job{Type: queue.JobTypeProcessImage, ImageID: 1, ObjectKey: "aa/bb/cat.png"}
}

func TestHandleBuildsThumbnailAndMarksReady(t *testing.T) {
	p, imgs, mem, exp := setup(t, domain.StatusPending, nil)
	if err := p.Handle(context.Background(), job()); err != nil {
		t.Fatal(err)
	}
	r := imgs.result
	if r == nil || r.Width != 64 || r.Height != 48 || r.ThumbnailPath != "thumbnails/1.png" || r.Format != "png" {
		t.Fatalf("result = %+v", r)
	}
	if _, err := mem.Stat(context.Background(), "thumbnails/1.png"); err != nil {
		t.Fatalf("thumbnail not stored: %v", err)
	}
	steps := map[string]bool{}
	for _, s := range exp.GetSpans() {
		steps[s.Name] = true
	}
	for _, want := range []string{"image.fetch", "image.decode", "image.thumbnail", "image.store"} {
		if !steps[want] {
			t.Errorf("missing span %s (have %v)", want, steps)
		}
	}
}

func TestHandleSkipsReadyImage(t *testing.T) {
	p, _, _, _ := setup(t, domain.StatusReady, nil)
	if err := p.Handle(context.Background(), job()); !errors.Is(err, queue.ErrSkip) {
		t.Fatalf("want ErrSkip, got %v", err)
	}
}

func TestHandleRejectsOversizedImageAsPermanent(t *testing.T) {
	p, _, _, _ := setup(t, domain.StatusPending, nil)
	p.maxPixels = 100 // 64x48 = 3072 pixels
	if err := p.Handle(context.Background(), job()); !queue.IsPermanent(err) {
		t.Fatalf("want a permanent error, got %v", err)
	}
}

func TestInjectedFaultIsRetryable(t *testing.T) {
	boom := errors.New("demo fault: injected worker failure")
	p, _, _, _ := setup(t, domain.StatusPending, faultFn(func(context.Context) error { return boom }))
	err := p.Handle(context.Background(), job())
	if !errors.Is(err, boom) || queue.IsPermanent(err) {
		t.Fatalf("want the retryable fault, got %v", err)
	}
}

func TestOnDeadLetterMarksFailed(t *testing.T) {
	p, imgs, _, _ := setup(t, domain.StatusProcessing, nil)
	p.OnDeadLetter(context.Background(), job(), errors.New("gave up"))
	if imgs.img.Status != domain.StatusFailed || imgs.errMsg == nil || *imgs.errMsg != "gave up" {
		t.Fatalf("status %q msg %v", imgs.img.Status, imgs.errMsg)
	}
}
