package database_test

import (
	"context"
	"testing"

	"image-gallery/internal/platform/database"
	"image-gallery/internal/testutils"
)

func TestImageProcessingStatusRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx := context.Background()
	tc, err := testutils.SetupTestContainers(ctx) // applies every migration, 004 included
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tc.Cleanup(ctx) }()
	repo := database.NewImageRepository(tc.DB)

	img := &database.Image{Filename: "a.png", OriginalFilename: "a.png", ContentType: "image/png",
		FileSize: 10, StoragePath: "aa/bb/a.png", Status: "pending", Metadata: database.Metadata{}}
	if err := repo.Create(ctx, img); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetByID(ctx, img.ID)
	if err != nil || got.Status != "pending" {
		t.Fatalf("after Create: status %q err %v", got.Status, err)
	}
	if err := repo.UpdateStatus(ctx, img.ID, "processing", nil); err != nil {
		t.Fatal(err)
	}
	if err := repo.CompleteProcessing(ctx, img.ID, database.ProcessingResult{
		ThumbnailPath: "thumbnails/1.png", Width: 640, Height: 480, Metadata: database.Metadata{"format": "png"},
	}); err != nil {
		t.Fatal(err)
	}
	got, err = repo.GetByID(ctx, img.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "ready" || got.ThumbnailPath == nil || *got.ThumbnailPath != "thumbnails/1.png" ||
		got.Width == nil || *got.Width != 640 || got.ProcessedAt == nil || got.ProcessingError != nil {
		t.Fatalf("after CompleteProcessing: %+v", got)
	}
	list, err := repo.List(ctx, database.PaginationParams{Limit: 10}, database.SortParams{})
	if err != nil || len(list) != 1 || list[0].Status != "ready" {
		t.Fatalf("List status: %v %v", list, err)
	}
	var rows int
	if err := tc.DB.QueryRowContext(ctx, "SELECT count(*) FROM demo_controls").Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("demo_controls rows = %d err %v", rows, err)
	}
}
