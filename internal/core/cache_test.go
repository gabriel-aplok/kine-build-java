package core

import (
	"context"
	"testing"
)

type memoryCache struct {
	entries map[string]struct{}
}

func newMemoryCache() *memoryCache {
	return &memoryCache{entries: make(map[string]struct{})}
}

func (c *memoryCache) Lookup(ctx context.Context, fingerprint string, outputs []Output) (bool, error) {
	_, ok := c.entries[fingerprint]
	return ok, nil
}

func (c *memoryCache) Store(ctx context.Context, fingerprint string, outputs []Output) error {
	c.entries[fingerprint] = struct{}{}
	return nil
}

var (
	_ Cache = (*memoryCache)(nil)
	_ Cache = NoopCache{}
)

func TestCacheContract(t *testing.T) {
	cache := newMemoryCache()
	outputs := []Output{{Path: "app/out", Mode: ModeTree}}

	hit, err := cache.Lookup(context.Background(), "abc", outputs)
	if err != nil {
		t.Fatalf("Lookup() error = %v", err)
	}
	if hit {
		t.Fatal("Lookup() = true, want false on empty cache")
	}

	if err := cache.Store(context.Background(), "abc", outputs); err != nil {
		t.Fatalf("Store() error = %v", err)
	}
	hit, err = cache.Lookup(context.Background(), "abc", outputs)
	if err != nil {
		t.Fatalf("Lookup() error = %v", err)
	}
	if !hit {
		t.Error("Lookup() = false, want true after Store")
	}
	if outputs[0].Mode != ModeTree {
		t.Errorf("Mode = %v, want ModeTree", outputs[0].Mode)
	}
}
