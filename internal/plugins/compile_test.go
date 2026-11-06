package plugins

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

func TestCompileHost(t *testing.T) {
	jdk := requireHostJDK(t)
	root := t.TempDir()

	build, err := CompileHost(context.Background(), root, jdk)
	if err != nil {
		t.Fatalf("CompileHost() error = %v", err)
	}
	mainClass := filepath.Join(build.Classes, "dev", "kine", "plugin", "host", "Main.class")
	if !fileExists(mainClass) {
		t.Errorf("host main class %s is missing", mainClass)
	}
	if !fileExists(build.APIJar) {
		t.Fatalf("api jar %s is missing", build.APIJar)
	}

	reader, err := zip.OpenReader(build.APIJar)
	if err != nil {
		t.Fatalf("open api jar: %v", err)
	}
	defer reader.Close()

	names := make(map[string]bool)
	for _, file := range reader.File {
		names[file.Name] = true
	}
	for _, name := range []string{
		"dev/kine/plugin/Plugin.class",
		"dev/kine/plugin/Provider.class",
		"dev/kine/plugin/Task.class",
		"dev/kine/plugin/Action.class",
		"dev/kine/plugin/Context.class",
	} {
		if !names[name] {
			t.Errorf("api jar does not contain %s", name)
		}
	}
	if names["dev/kine/plugin/host/Main.class"] {
		t.Error("api jar contains host classes")
	}
}

func TestCompileHostReusesCache(t *testing.T) {
	jdk := requireHostJDK(t)
	root := t.TempDir()

	first, err := CompileHost(context.Background(), root, jdk)
	if err != nil {
		t.Fatalf("CompileHost() error = %v", err)
	}
	info, err := os.Stat(filepath.Join(first.Classes, "dev", "kine", "plugin", "host", "Main.class"))
	if err != nil {
		t.Fatal(err)
	}

	second, err := CompileHost(context.Background(), root, jdk)
	if err != nil {
		t.Fatalf("second CompileHost() error = %v", err)
	}
	if second.Classes != first.Classes || second.APIJar != first.APIJar {
		t.Errorf("build = %+v, want %+v", second, first)
	}
	again, err := os.Stat(filepath.Join(second.Classes, "dev", "kine", "plugin", "host", "Main.class"))
	if err != nil {
		t.Fatal(err)
	}
	if !again.ModTime().Equal(info.ModTime()) {
		t.Error("the cached host was recompiled")
	}
}

func TestCompileHostRejectsBadJDK(t *testing.T) {
	tests := []struct {
		name string
		jdk  *toolchain.JDK
		want string
	}{
		{name: "nil", jdk: nil, want: "JDK"},
		{name: "old", jdk: &toolchain.JDK{Major: 8, Javac: "/bin/false"}, want: "17"},
		{name: "no javac", jdk: &toolchain.JDK{Major: 17}, want: "javac"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := CompileHost(context.Background(), t.TempDir(), tt.jdk)
			if err == nil {
				t.Fatal("CompileHost() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not contain %q", err, tt.want)
			}
		})
	}
}

func TestCompileHostCompilerFailure(t *testing.T) {
	script := filepath.Join(t.TempDir(), "javac.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho boom >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := CompileHost(context.Background(), t.TempDir(), &toolchain.JDK{Major: 17, Javac: script})
	if err == nil {
		t.Fatal("CompileHost() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error %q does not include the compiler output", err)
	}
}

func TestReadSources(t *testing.T) {
	sources, hash, err := readSources()
	if err != nil {
		t.Fatalf("readSources() error = %v", err)
	}
	if len(sources) < 5 {
		t.Errorf("sources = %d, want at least 5", len(sources))
	}
	if len(hash) != 64 {
		t.Errorf("hash = %q, want 64 hex characters", hash)
	}
	if !strings.HasSuffix(sources[0].path, ".java") {
		t.Errorf("first source = %q, want a java file", sources[0].path)
	}
}

func requireHostJDK(t *testing.T) *toolchain.JDK {
	t.Helper()

	jdk, err := toolchain.Find(toolchain.Requirement{})
	if err != nil {
		t.Skipf("no JDK installed: %v", err)
	}
	if jdk.Major < MinHostJDK {
		t.Skipf("JDK %d is older than %d", jdk.Major, MinHostJDK)
	}
	return jdk
}
