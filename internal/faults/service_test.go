package faults

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"image-gallery/internal/domain/demo"
)

type memRepo struct {
	c    demo.Controls
	gets int
}

func (m *memRepo) Get(context.Context) (demo.Controls, error) { m.gets++; return m.c, nil }
func (m *memRepo) Save(_ context.Context, c demo.Controls) (demo.Controls, error) {
	m.c = c
	return c, nil
}

type memCache map[string][]byte

func (m memCache) Get(_ context.Context, k string, out interface{}) error {
	b, ok := m[k]
	if !ok {
		return errors.New("miss")
	}
	return json.Unmarshal(b, out)
}
func (m memCache) Set(_ context.Context, k string, v interface{}, _ time.Duration) error {
	b, err := json.Marshal(v)
	m[k] = b
	return err
}
func (m memCache) Delete(_ context.Context, k string) error { delete(m, k); return nil }

func TestServiceCachesValidatesAndInvalidates(t *testing.T) {
	ctx := context.Background()
	repo := &memRepo{}
	s := NewService(repo, memCache{})
	_, _ = s.Get(ctx)
	_, _ = s.Get(ctx)
	if repo.gets != 1 {
		t.Fatalf("repo reads = %d, want 1 (second read from cache)", repo.gets)
	}
	if _, err := s.Update(ctx, demo.Controls{ErrorProbability: 1.5}); !errors.Is(err, demo.ErrInvalidControls) {
		t.Fatalf("want ErrInvalidControls, got %v", err)
	}
	if _, err := s.Update(ctx, demo.Controls{ErrorProbability: 0.2}); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.Get(ctx); c.ErrorProbability != 0.2 {
		t.Fatalf("Update must invalidate the cache, read %v", c.ErrorProbability)
	}
	if c, _ := s.Reset(ctx); c.Active() {
		t.Fatalf("Reset left controls on: %+v", c)
	}
}
