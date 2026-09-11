package storage_test

import (
	"context"
	"strings"
	"testing"

	gcs "cloud.google.com/go/storage"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"image-gallery/internal/platform/storage"
	"image-gallery/internal/platform/storage/storetest"
)

const fakeGCSImage = "fsouza/fake-gcs-server:1.52.2"

func TestGCSStoreContract(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx := context.Background()
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        fakeGCSImage,
			ExposedPorts: []string{"4443/tcp"},
			Cmd:          []string{"-scheme", "http", "-port", "4443", "-backend", "memory"},
			WaitingFor:   wait.ForHTTP("/storage/v1/b").WithPort("4443/tcp"),
		},
		Started: true,
	})
	testcontainers.CleanupContainer(t, c)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := c.PortEndpoint(ctx, "4443/tcp", "http")
	if err != nil {
		t.Fatal(err)
	}
	// The Go client routes every call to the emulator, unauthenticated, when this is set.
	t.Setenv("STORAGE_EMULATOR_HOST", strings.TrimPrefix(endpoint, "http://"))

	admin, err := gcs.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if err := admin.Bucket("contract").Create(ctx, "test-project", nil); err != nil {
		t.Fatal(err)
	}
	s, err := storage.NewGCSStore(ctx, "contract", gcs.WithJSONReads())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Provider() != storage.ProviderGCS {
		t.Fatalf("provider = %s", s.Provider())
	}
	storetest.Run(t, s)
}
