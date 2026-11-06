package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMissingConfig(t *testing.T) {
	plugins, err := Load(t.TempDir())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(plugins) != 0 {
		t.Errorf("plugins = %v, want empty", plugins)
	}
}

func TestLoadPlugins(t *testing.T) {
	root := t.TempDir()
	writePluginJars(t, root,
		`{"api":1,"id":"first","version":"1.0","main":"a.First","providers":["alpha"]}`,
		`{"api":1,"id":"second","version":"2.0","main":"b.Second","providers":["beta","gamma"]}`,
	)

	plugins, err := Load(root)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(plugins) != 2 {
		t.Fatalf("plugins = %d, want 2", len(plugins))
	}
	if plugins[0].Manifest.ID != "first" || plugins[1].Manifest.ID != "second" {
		t.Errorf("order = %q, %q", plugins[0].Manifest.ID, plugins[1].Manifest.ID)
	}
	for _, plugin := range plugins {
		if !strings.HasPrefix(plugin.Jar, root) {
			t.Errorf("jar %q is not under the workspace", plugin.Jar)
		}
	}
}

func TestLoadDuplicatePluginID(t *testing.T) {
	root := t.TempDir()
	writePluginJars(t, root,
		`{"api":1,"id":"same","version":"1.0","main":"a.First","providers":["alpha"]}`,
		`{"api":1,"id":"same","version":"2.0","main":"b.Second","providers":["beta"]}`,
	)

	_, err := Load(root)
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "duplicate plugin id") {
		t.Errorf("error %q does not mention the duplicate id", err)
	}
}

func TestLoadDuplicateProvider(t *testing.T) {
	root := t.TempDir()
	writePluginJars(t, root,
		`{"api":1,"id":"first","version":"1.0","main":"a.First","providers":["shared"]}`,
		`{"api":1,"id":"second","version":"2.0","main":"b.Second","providers":["shared"]}`,
	)

	_, err := Load(root)
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "already provided") {
		t.Errorf("error %q does not mention the duplicate provider", err)
	}
}

func TestLoadBadManifest(t *testing.T) {
	root := t.TempDir()
	writePluginJars(t, root,
		`{"api":1,"id":"first","version":"1.0","main":"a.First","providers":["jvm"]}`,
	)

	_, err := Load(root)
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "built in") {
		t.Errorf("error %q does not mention the built-in provider", err)
	}
}

func writePluginJars(t *testing.T, root string, manifests ...string) {
	t.Helper()

	jars := make([]string, 0, len(manifests))
	for i, manifest := range manifests {
		name := "plugins/p" + fmt.Sprintf("%d", i)
		dir := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		writeManifestJar(t, dir, manifest)
		jars = append(jars, name+"/plugin.jar")
	}

	content := "jars = ["
	for i, jar := range jars {
		if i > 0 {
			content += ", "
		}
		content += `"` + jar + `"`
	}
	content += "]\n"
	writeTestFile(t, filepath.Join(root, configFileName), content)
}
