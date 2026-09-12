package storetest

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"image-gallery/internal/platform/storage"
)

type memObject struct {
	data []byte
	info storage.ObjectInfo
}

// MemStore is an in-memory storage.ObjectStore for unit tests.
type MemStore struct {
	mu   sync.Mutex
	objs map[string]memObject
}

// NewMemStore returns an empty store whose provider is "memory".
func NewMemStore() *MemStore { return &MemStore{objs: map[string]memObject{}} }

func (m *MemStore) Provider() string { return "memory" }

func (m *MemStore) Put(_ context.Context, key, contentType string, r io.Reader, _ int64, md map[string]string) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	lower := map[string]string{}
	for k, v := range md {
		lower[strings.ToLower(k)] = v
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objs[key] = memObject{data: b, info: storage.ObjectInfo{Key: key, Size: int64(len(b)), ContentType: contentType, LastModified: time.Now(), UserMetadata: lower}}
	return nil
}

func (m *MemStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.objs[key]
	if !ok {
		return nil, fmt.Errorf("%w: %s", storage.ErrNotFound, key)
	}
	return io.NopCloser(bytes.NewReader(o.data)), nil
}

func (m *MemStore) Stat(_ context.Context, key string) (storage.ObjectInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.objs[key]
	if !ok {
		return storage.ObjectInfo{}, fmt.Errorf("%w: %s", storage.ErrNotFound, key)
	}
	return o.info, nil
}

func (m *MemStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objs, key)
	return nil
}

func (m *MemStore) List(_ context.Context, prefix string, maxKeys int) ([]storage.ObjectInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	keys := make([]string, 0, len(m.objs))
	for k := range m.objs {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	out := make([]storage.ObjectInfo, 0, len(keys))
	for _, k := range keys {
		if maxKeys > 0 && len(out) == maxKeys {
			break
		}
		out = append(out, m.objs[k].info)
	}
	return out, nil
}

func (m *MemStore) Health(context.Context) error { return nil }
