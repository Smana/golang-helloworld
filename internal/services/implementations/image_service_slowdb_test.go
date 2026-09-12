package implementations

import (
	"context"
	"errors"
	"testing"

	"image-gallery/internal/domain/image"
)

// listCache serves or misses image lists; ListImages uses no other method.
type listCache struct {
	image.CacheService
	hit bool
}

func (c listCache) GetImageList(context.Context, string) (*image.ListImagesResponse, error) {
	if c.hit {
		return &image.ListImagesResponse{}, nil
	}
	return nil, errors.New("miss")
}

func (listCache) SetImageList(context.Context, string, *image.ListImagesResponse, int64) error {
	return nil
}

func TestListImagesSlowDBHookRunsOnlyWhenTheDatabaseIsQueried(t *testing.T) {
	for name, tc := range map[string]struct {
		cache image.CacheService
		want  int
	}{
		"cache hit":  {listCache{hit: true}, 0},
		"cache miss": {listCache{}, 1},
		"no cache":   {nil, 1},
	} {
		t.Run(name, func(t *testing.T) {
			s := NewImageService(newFakeRepo(), nil, nil, nil, nil, nil, tc.cache).(*ImageServiceImpl)
			calls := 0
			s.SetSlowDB(func(context.Context) { calls++ })
			if _, err := s.ListImages(context.Background(), &image.ListImagesRequest{}); err != nil {
				t.Fatal(err)
			}
			if calls != tc.want {
				t.Fatalf("slow-DB hook ran %d times, want %d", calls, tc.want)
			}
		})
	}
}
