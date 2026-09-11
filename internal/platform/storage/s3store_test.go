package storage_test

import (
	"context"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	tcminio "github.com/testcontainers/testcontainers-go/modules/minio"

	"image-gallery/internal/config"
	"image-gallery/internal/platform/storage"
	"image-gallery/internal/platform/storage/storetest"
)

func TestS3StoreContract(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx := context.Background()
	c, err := tcminio.Run(ctx, "minio/minio:latest", tcminio.WithUsername("testuser"), tcminio.WithPassword("testpass123")) // pragma: allowlist secret
	testcontainers.CleanupContainer(t, c)
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err := c.ConnectionString(ctx)
	if err != nil {
		t.Fatal(err)
	}
	s, err := storage.NewS3Store(ctx, config.StorageConfig{
		Provider: storage.ProviderS3, Endpoint: endpoint, AccessKeyID: "testuser", SecretAccessKey: "testpass123", // pragma: allowlist secret
		BucketName: "contract", Region: "us-east-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.Provider() != storage.ProviderS3 {
		t.Fatalf("provider = %s", s.Provider())
	}
	storetest.Run(t, s)
}
