package main

import (
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
)

func TestRegistryIncludesBuiltins(t *testing.T) {
	workspace := &core.Workspace{
		Root:     t.TempDir(),
		Projects: map[string]*core.Project{},
	}

	registry, err := buildRegistry(workspace, nil, discardTestLogger(), taskRequest{}, nil)
	if err != nil {
		t.Fatalf("buildRegistry() error = %v", err)
	}
	for _, name := range []string{"jvm", "desktop", "android", "minecraft"} {
		provider, err := registry.Provider(name)
		if err != nil {
			t.Errorf("provider %q: %v", name, err)
			continue
		}
		if provider.Name() != name {
			t.Errorf("provider %q has name %q", name, provider.Name())
		}
	}
}
