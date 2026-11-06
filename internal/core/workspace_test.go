package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSingleProject(t *testing.T) {
	root := filepath.Join(t.TempDir(), "demo")
	writeFiles(t, root, map[string]string{
		"kine.toml": `[project]
provider = "jvm"
`,
	})

	ws, err := Load(root)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if ws.Root != resolved {
		t.Errorf("Root = %q, want %q", ws.Root, resolved)
	}
	if ws.Name != "demo" {
		t.Errorf("Name = %q, want demo", ws.Name)
	}
	if len(ws.Order) != 1 || ws.Order[0] != "demo" {
		t.Fatalf("Order = %v, want [demo]", ws.Order)
	}
	project, ok := ws.Projects["demo"]
	if !ok {
		t.Fatal("Projects[demo] missing")
	}
	if project.Provider != "jvm" {
		t.Errorf("Provider = %q, want jvm", project.Provider)
	}
	if project.Dir != resolved {
		t.Errorf("Dir = %q, want %q", project.Dir, resolved)
	}
}

func TestLoadWorkspace(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	writeFiles(t, root, map[string]string{
		"kine.toml": `[workspace]
name = "demo"
members = ["lib", "app"]
`,
		"lib/kine.toml": `[project]
provider = "jvm"
`,
		"app/kine.toml": `[project]
provider = "jvm"
depends_on = ["lib"]
`,
	})

	ws, err := Load(root)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if ws.Name != "demo" {
		t.Errorf("Name = %q, want demo", ws.Name)
	}
	if len(ws.Order) != 2 || ws.Order[0] != "lib" || ws.Order[1] != "app" {
		t.Fatalf("Order = %v, want [lib app]", ws.Order)
	}
	if len(ws.Projects) != 2 {
		t.Fatalf("len(Projects) = %d, want 2", len(ws.Projects))
	}
	app, ok := ws.Projects["app"]
	if !ok {
		t.Fatal("Projects[app] missing")
	}
	if len(app.DependsOn) != 1 || app.DependsOn[0] != "lib" {
		t.Errorf("DependsOn = %v, want [lib]", app.DependsOn)
	}
}

func TestLoadProjectNameOverride(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	writeFiles(t, root, map[string]string{
		"kine.toml": `[workspace]
name = "demo"
members = ["app"]
`,
		"app/kine.toml": `[project]
name = "application"
provider = "jvm"
`,
	})

	ws, err := Load(root)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if _, ok := ws.Projects["application"]; !ok {
		t.Error("Projects[application] missing")
	}
	if _, ok := ws.Projects["app"]; ok {
		t.Error("Projects[app] present, want override")
	}
}

func TestLoadRejectsDuplicateIDs(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	writeFiles(t, root, map[string]string{
		"kine.toml": `[workspace]
name = "demo"
members = ["one/app", "two/app"]
`,
		"one/app/kine.toml": `[project]
provider = "jvm"
`,
		"two/app/kine.toml": `[project]
provider = "jvm"
`,
	})

	_, err := Load(root)
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error %q does not mention duplicate", err)
	}
}

func TestLoadRejectsDuplicateMembers(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	writeFiles(t, root, map[string]string{
		"kine.toml": `[workspace]
name = "demo"
members = ["lib", "lib"]
`,
		"lib/kine.toml": `[project]
provider = "jvm"
`,
	})

	_, err := Load(root)
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error %q does not mention duplicate", err)
	}
}

func TestLoadRejectsMissingMember(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	writeFiles(t, root, map[string]string{
		"kine.toml": `[workspace]
name = "demo"
members = ["missing"]
`,
	})

	_, err := Load(root)
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("error %q does not name the member", err)
	}
}

func TestLoadRejectsRootMember(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	writeFiles(t, root, map[string]string{
		"kine.toml": `[workspace]
name = "demo"
members = ["."]
`,
	})

	_, err := Load(root)
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "root") {
		t.Errorf("error %q does not mention the root", err)
	}
}

func TestLoadRejectsMemberEscape(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	writeFiles(t, root, map[string]string{
		"kine.toml": `[workspace]
name = "demo"
members = ["../outside"]
`,
	})

	_, err := Load(root)
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "escape") {
		t.Errorf("error %q does not mention escape", err)
	}
}

func TestLoadRejectsAbsoluteMember(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	outside := t.TempDir()
	writeFiles(t, root, map[string]string{
		"kine.toml": fmt.Sprintf(`[workspace]
name = "demo"
members = [%q]
`, outside),
	})

	_, err := Load(root)
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "relative") {
		t.Errorf("error %q does not mention relative", err)
	}
}

func TestLoadRejectsSymlinkEscape(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	outside := filepath.Join(base, "outside")
	writeFiles(t, root, map[string]string{
		"kine.toml": `[workspace]
name = "demo"
members = ["link"]
`,
	})
	writeFiles(t, outside, map[string]string{
		"kine.toml": `[project]
provider = "jvm"
`,
	})
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	_, err := Load(root)
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "outside") {
		t.Errorf("error %q does not mention outside", err)
	}
}

func TestLoadRejectsNestedWorkspace(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	writeFiles(t, root, map[string]string{
		"kine.toml": `[workspace]
name = "demo"
members = ["app"]
`,
		"app/kine.toml": `[workspace]
name = "nested"
members = ["inner"]
`,
	})

	_, err := Load(root)
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "project") {
		t.Errorf("error %q does not mention project", err)
	}
}

func TestLoadRejectsBadID(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	writeFiles(t, root, map[string]string{
		"kine.toml": `[project]
name = "Bad_ID"
provider = "jvm"
`,
	})

	_, err := Load(root)
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Errorf("error %q does not mention invalid", err)
	}
}

func TestLoadMissingConfig(t *testing.T) {
	_, err := Load(t.TempDir())
	if err == nil {
		t.Fatal("Load() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "kine.toml") {
		t.Errorf("error %q does not mention kine.toml", err)
	}
}

func TestProjectLookup(t *testing.T) {
	root := filepath.Join(t.TempDir(), "demo")
	writeFiles(t, root, map[string]string{
		"kine.toml": `[project]
provider = "jvm"
`,
	})

	ws, err := Load(root)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if _, err := ws.Project("demo"); err != nil {
		t.Errorf("Project(demo) error = %v", err)
	}
	if _, err := ws.Project("missing"); !errors.Is(err, ErrProjectNotFound) {
		t.Errorf("Project(missing) error = %v, want ErrProjectNotFound", err)
	}
}

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()

	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
