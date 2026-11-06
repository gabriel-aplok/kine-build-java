package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
)

func TestDecodeOptionsDefaults(t *testing.T) {
	root := t.TempDir()
	project := &core.Project{ID: "app", Dir: filepath.Join(root, "app")}

	options, err := decodeOptions(project, root)
	if err != nil {
		t.Fatalf("decodeOptions() error = %v", err)
	}
	if options.Package.Name != "app" {
		t.Errorf("Name = %q, want app", options.Package.Name)
	}
	if options.Package.Vendor != defaultVendor {
		t.Errorf("Vendor = %q", options.Package.Vendor)
	}
	if options.Package.Version != defaultVersion {
		t.Errorf("Version = %q", options.Package.Version)
	}
	if options.Package.Type != appImageType {
		t.Errorf("Type = %q", options.Package.Type)
	}
	if options.Package.Dest != defaultDest {
		t.Errorf("Dest = %q", options.Package.Dest)
	}
	if len(options.JVM) != 0 {
		t.Errorf("JVM options = %v, want empty", options.JVM)
	}
}

func TestDecodeOptions(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "app")
	icon := filepath.Join(projectDir, "assets", "icon.png")
	if err := os.MkdirAll(filepath.Dir(icon), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(icon, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}

	project := &core.Project{
		ID:  "app",
		Dir: projectDir,
		Options: map[string]any{
			"java":       21,
			"main_class": "demo.App",
			"package": map[string]any{
				"name":         "Demo",
				"vendor":       "Example",
				"version":      "2.1.0",
				"icon":         "assets/icon.png",
				"type":         "app-image",
				"dest":         "dist",
				"java_options": []any{"-Xmx128m", "-Dfoo=bar"},
			},
		},
	}

	options, err := decodeOptions(project, root)
	if err != nil {
		t.Fatalf("decodeOptions() error = %v", err)
	}
	if options.Package.Name != "Demo" || options.Package.Vendor != "Example" {
		t.Errorf("package = %+v", options.Package)
	}
	if options.Package.Version != "2.1.0" || options.Package.Dest != "dist" {
		t.Errorf("package = %+v", options.Package)
	}
	if options.Package.Icon != icon {
		t.Errorf("Icon = %q, want %q", options.Package.Icon, icon)
	}
	if len(options.Package.JavaOptions) != 2 {
		t.Errorf("JavaOptions = %v", options.Package.JavaOptions)
	}
	if _, ok := options.JVM["package"]; ok {
		t.Error("JVM options still contain the package table")
	}
	if options.JVM["main_class"] != "demo.App" {
		t.Errorf("JVM options = %v", options.JVM)
	}
}

func TestDecodeOptionsUnknownKey(t *testing.T) {
	root := t.TempDir()
	project := &core.Project{
		ID:  "app",
		Dir: filepath.Join(root, "app"),
		Options: map[string]any{
			"package": map[string]any{"extra": true},
		},
	}

	_, err := decodeOptions(project, root)
	if err == nil {
		t.Fatal("decodeOptions() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "strict mode") {
		t.Errorf("error %q does not name the unknown key", err)
	}
}

func TestDecodeOptionsValidation(t *testing.T) {
	tests := []struct {
		name string
		pkg  map[string]any
		want string
	}{
		{name: "bad type", pkg: map[string]any{"type": "installer"}, want: "not supported"},
		{name: "empty name", pkg: map[string]any{"name": " "}, want: "name is empty"},
		{name: "path name", pkg: map[string]any{"name": "a/b"}, want: "plain name"},
		{name: "empty vendor", pkg: map[string]any{"vendor": " "}, want: "vendor is empty"},
		{name: "bad version", pkg: map[string]any{"version": "v1"}, want: "digit"},
		{name: "absolute dest", pkg: map[string]any{"dest": "/tmp/out"}, want: "relative"},
		{name: "escaping dest", pkg: map[string]any{"dest": "../out"}, want: "escapes"},
		{name: "empty java option", pkg: map[string]any{"java_options": []any{" "}}, want: "java option is empty"},
		{name: "absolute icon", pkg: map[string]any{"icon": "/tmp/icon.png"}, want: "relative"},
		{name: "missing icon", pkg: map[string]any{"icon": "missing.png"}, want: "missing.png"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			project := &core.Project{
				ID:      "app",
				Dir:     filepath.Join(root, "app"),
				Options: map[string]any{"package": tt.pkg},
			}
			_, err := decodeOptions(project, root)
			if err == nil {
				t.Fatal("decodeOptions() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not contain %q", err, tt.want)
			}
		})
	}
}

func TestDecodeOptionsIconEscape(t *testing.T) {
	root := t.TempDir()
	projectDir := filepath.Join(root, "app")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "icon.png")
	if err := os.WriteFile(outside, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	relative := filepath.Join("..", "..", filepath.Base(filepath.Dir(outside)), "icon.png")

	project := &core.Project{
		ID:      "app",
		Dir:     projectDir,
		Options: map[string]any{"package": map[string]any{"icon": relative}},
	}
	_, err := decodeOptions(project, root)
	if err == nil {
		t.Fatal("decodeOptions() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "escapes") {
		t.Errorf("error %q does not mention the escape", err)
	}
}
