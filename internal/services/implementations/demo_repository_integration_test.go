package implementations_test

import (
	"context"
	"testing"

	"image-gallery/internal/domain/demo"
	"image-gallery/internal/services/implementations"
	"image-gallery/internal/testutils"
)

func TestDemoRepositoryRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx := context.Background()
	tc, err := testutils.SetupTestContainers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tc.Cleanup(ctx) }()
	r := implementations.NewDemoRepository(tc.DB)
	c, err := r.Get(ctx)
	if err != nil || c.Active() {
		t.Fatalf("defaults must be off: %+v %v", c, err)
	}
	want := demo.Controls{LatencyMS: 800, LatencyProbability: 0.5, LatencyRoutes: []string{"/api/images"}, WorkerDelayMS: 4000}
	if _, err := r.Save(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := r.Get(ctx)
	if err != nil || got.LatencyMS != 800 || got.LatencyProbability != 0.5 || len(got.LatencyRoutes) != 1 || got.WorkerDelayMS != 4000 {
		t.Fatalf("round trip = %+v, %v", got, err)
	}
	if err := implementations.SleepInDB(ctx, tc.DB, 0.01); err != nil {
		t.Fatal(err)
	}
}
