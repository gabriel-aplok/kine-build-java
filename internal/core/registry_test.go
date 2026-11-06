package core

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeProvider struct {
	name  string
	tasks []*Task
	err   error
}

func (p *fakeProvider) Name() string {
	return p.name
}

func (p *fakeProvider) Tasks(ctx context.Context, project *Project) ([]*Task, error) {
	if p.err != nil {
		return nil, p.err
	}

	tasks := make([]*Task, len(p.tasks))
	for i, task := range p.tasks {
		copied := *task
		copied.Inputs = append([]Input(nil), task.Inputs...)
		copied.Outputs = append([]Output(nil), task.Outputs...)
		copied.Deps = append([]string(nil), task.Deps...)
		tasks[i] = &copied
	}
	return tasks, nil
}

func TestRegistryRegisterAndGet(t *testing.T) {
	registry := NewRegistry()
	provider := &fakeProvider{name: "jvm"}

	if err := registry.Register(provider); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	got, err := registry.Provider("jvm")
	if err != nil {
		t.Fatalf("Provider() error = %v", err)
	}
	if got != provider {
		t.Error("Provider() returned a different provider")
	}
}

func TestRegistryRejectsDuplicate(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(&fakeProvider{name: "jvm"}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	err := registry.Register(&fakeProvider{name: "jvm"})
	if err == nil {
		t.Fatal("Register() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "already") {
		t.Errorf("error %q does not mention already registered", err)
	}
}

func TestRegistryProviderNotFound(t *testing.T) {
	_, err := NewRegistry().Provider("missing")
	if !errors.Is(err, ErrProviderNotFound) {
		t.Errorf("Provider() error = %v, want ErrProviderNotFound", err)
	}
}

func TestRegistryRejectsEmptyName(t *testing.T) {
	err := NewRegistry().Register(&fakeProvider{})
	if err == nil {
		t.Fatal("Register() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "name") {
		t.Errorf("error %q does not mention name", err)
	}
}

func TestRegistryRejectsNilProvider(t *testing.T) {
	err := NewRegistry().Register(nil)
	if err == nil {
		t.Fatal("Register() error = nil, want error")
	}
}
