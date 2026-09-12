// Package storetest is the behavioral contract every storage.ObjectStore
// backend must pass, so S3 and GCS cannot drift apart.
package storetest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"image-gallery/internal/platform/storage"
)

// testFilename is the original-filename metadata value put on the contract
// object; it is asserted lower-cased on the way back out.
const testFilename = "a b.txt"

// Run exercises Put/Stat/Get/List/Delete/Health against s.
func Run(t *testing.T, s storage.ObjectStore) {
	t.Helper()
	ctx := context.Background()
	key := fmt.Sprintf("contract/%d.txt", time.Now().UnixNano())
	body := []byte("hello object store")

	runPut(t, ctx, s, key, body)
	runStat(t, ctx, s, key, body)
	runGet(t, ctx, s, key, body)
	runList(t, ctx, s, key)
	runMissing(t, ctx, s)
	runDeleteAndHealth(t, ctx, s, key)
}

func runPut(t *testing.T, ctx context.Context, s storage.ObjectStore, key string, body []byte) {
	t.Helper()
	if err := s.Put(ctx, key, "text/plain", bytes.NewReader(body), int64(len(body)),
		map[string]string{"original-filename": testFilename}); err != nil {
		t.Fatalf("Put: %v", err)
	}
}

func runStat(t *testing.T, ctx context.Context, s storage.ObjectStore, key string, body []byte) {
	t.Helper()
	info, err := s.Stat(ctx, key)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Size != int64(len(body)) || info.ContentType != "text/plain" {
		t.Errorf("Stat = size %d type %q", info.Size, info.ContentType)
	}
	if got := info.UserMetadata["original-filename"]; got != testFilename {
		t.Errorf("metadata original-filename = %q (keys must be lower-cased)", got)
	}
}

func runGet(t *testing.T, ctx context.Context, s storage.ObjectStore, key string, body []byte) {
	t.Helper()
	rc, err := s.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if err := rc.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("Get body = %q", got)
	}
}

func runList(t *testing.T, ctx context.Context, s storage.ObjectStore, key string) {
	t.Helper()
	objs, err := s.List(ctx, "contract/", 100)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, o := range objs {
		found = found || o.Key == key
	}
	if !found {
		t.Errorf("List did not return %s", key)
	}
}

func runMissing(t *testing.T, ctx context.Context, s storage.ObjectStore) {
	t.Helper()
	if _, err := s.Stat(ctx, "contract/missing"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Stat(missing) = %v, want ErrNotFound", err)
	}
	if _, err := s.Get(ctx, "contract/missing"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Get(missing) = %v, want ErrNotFound", err)
	}
}

func runDeleteAndHealth(t *testing.T, ctx context.Context, s storage.ObjectStore, key string) {
	t.Helper()
	if err := s.Delete(ctx, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Stat(ctx, key); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("Stat after Delete = %v, want ErrNotFound", err)
	}
	if err := s.Delete(ctx, key); err != nil {
		t.Errorf("Delete must be idempotent, got %v", err)
	}
	if err := s.Health(ctx); err != nil {
		t.Errorf("Health: %v", err)
	}
}
