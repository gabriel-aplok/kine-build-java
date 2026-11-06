package jvm

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

func TestProviderName(t *testing.T) {
	provider := newTestProvider(t, t.TempDir())
	if got := provider.Name(); got != "jvm" {
		t.Errorf("Name() = %q, want jvm", got)
	}
}

func TestPrepareSources(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src/main/java"), 0o755); err != nil {
		t.Fatal(err)
	}

	provider := newTestProvider(t, root)
	project := &core.Project{ID: "app", Dir: root, Provider: "jvm"}
	options := mustOptions(t, nil)

	build, err := provider.prepare(context.Background(), project, options)
	if err != nil {
		t.Fatalf("prepare() error = %v", err)
	}
	if len(build.sources.Java) != 1 || build.sources.Java[0] != filepath.Join(root, "src/main/java") {
		t.Errorf("Java sources = %v", build.sources.Java)
	}
	if len(build.sources.Kotlin) != 0 {
		t.Errorf("Kotlin sources = %v, want empty", build.sources.Kotlin)
	}
	if len(build.sources.TestJava) != 0 {
		t.Errorf("TestJava sources = %v, want empty", build.sources.TestJava)
	}
	if len(build.sources.Resources) != 0 {
		t.Errorf("Resources = %v, want empty", build.sources.Resources)
	}
	if build.jdk == nil || build.jdk.Major != 21 {
		t.Errorf("jdk = %+v, want major 21", build.jdk)
	}
}

func TestPrepareSourceEscape(t *testing.T) {
	root := t.TempDir()
	provider := newTestProvider(t, root)
	project := &core.Project{ID: "app", Dir: root, Provider: "jvm"}
	options := mustOptions(t, optionsFromTOML(t, `kotlin = "2.0.21"
[sources]
java = ["../outside"]
`))

	_, err := provider.prepare(context.Background(), project, options)
	if err == nil {
		t.Fatal("prepare() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "escape") {
		t.Errorf("error %q does not mention escape", err)
	}
}

func TestPrepareKotlinWithoutVersion(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src/main/kotlin"), 0o755); err != nil {
		t.Fatal(err)
	}
	provider := newTestProvider(t, root)
	project := &core.Project{ID: "app", Dir: root, Provider: "jvm"}
	options := mustOptions(t, nil)

	_, err := provider.prepare(context.Background(), project, options)
	if err == nil {
		t.Fatal("prepare() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "kotlin") {
		t.Errorf("error %q does not mention kotlin", err)
	}
}

func TestPrepareJDKNotFound(t *testing.T) {
	root := t.TempDir()
	provider := newTestProvider(t, root)
	provider.findJDK = func(toolchain.Requirement, ...string) (*toolchain.JDK, error) {
		return nil, toolchain.ErrJDKNotFound
	}
	project := &core.Project{ID: "app", Dir: root, Provider: "jvm"}

	_, err := provider.prepare(context.Background(), project, mustOptions(t, nil))
	if !errors.Is(err, toolchain.ErrJDKNotFound) {
		t.Errorf("prepare() error = %v, want ErrJDKNotFound", err)
	}
}

func TestTasksOptionsError(t *testing.T) {
	provider := newTestProvider(t, t.TempDir())
	project := &core.Project{
		ID:       "app",
		Dir:      t.TempDir(),
		Provider: "jvm",
		Options:  map[string]any{"unknown": true},
	}

	if _, err := provider.Tasks(context.Background(), project); err == nil {
		t.Fatal("Tasks() error = nil, want error")
	}
}

func newTestProvider(t *testing.T, workspaceRoot string) *Provider {
	t.Helper()

	provider := NewProvider(Config{
		Workspace: &core.Workspace{
			Root:     workspaceRoot,
			Projects: map[string]*core.Project{},
		},
		Logger: discardLogger(),
	})
	provider.findJDK = func(toolchain.Requirement, ...string) (*toolchain.JDK, error) {
		return &toolchain.JDK{
			Home:    "/fake/jdk",
			Version: "21.0.1",
			Major:   21,
			Java:    "/fake/jdk/bin/java",
			Javac:   "/fake/jdk/bin/javac",
		}, nil
	}
	return provider
}

func mustOptions(t *testing.T, values map[string]any) Options {
	t.Helper()

	options, err := decodeOptions(values, "app")
	if err != nil {
		t.Fatalf("decodeOptions() error = %v", err)
	}
	return options
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
