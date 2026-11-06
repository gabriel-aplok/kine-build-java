package core

import (
	"errors"
	"fmt"
)

// ErrProviderNotFound is returned when no provider is registered for a name.
var ErrProviderNotFound = errors.New("provider not found")

// Registry maps provider names to providers.
type Registry struct {
	providers map[string]Provider
}

// NewRegistry returns an empty provider registry.
func NewRegistry() *Registry {
	return &Registry{providers: make(map[string]Provider)}
}

// Register adds a provider. Duplicate names are errors.
func (r *Registry) Register(provider Provider) error {
	if provider == nil {
		return errors.New("provider is nil")
	}
	name := provider.Name()
	if name == "" {
		return errors.New("provider name is empty")
	}
	if r.providers == nil {
		r.providers = make(map[string]Provider)
	}
	if _, ok := r.providers[name]; ok {
		return fmt.Errorf("provider %q is already registered", name)
	}
	r.providers[name] = provider
	return nil
}

// Provider returns the provider registered under name.
func (r *Registry) Provider(name string) (Provider, error) {
	provider, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("provider %q: %w", name, ErrProviderNotFound)
	}
	return provider, nil
}
