package core

import "context"

// Cache stores task outputs by fingerprint.
// Lookup returns true only when every declared output exists.
// Implementations must be safe for concurrent use.
type Cache interface {
	Lookup(ctx context.Context, fingerprint string, outputs []Output) (bool, error)
	Store(ctx context.Context, fingerprint string, outputs []Output) error
}

// NoopCache disables caching.
type NoopCache struct{}

func (NoopCache) Lookup(ctx context.Context, fingerprint string, outputs []Output) (bool, error) {
	return false, nil
}

func (NoopCache) Store(ctx context.Context, fingerprint string, outputs []Output) error {
	return nil
}
