package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeWorkspace(t *testing.T) {
	path := writeConfig(t, `[workspace]
name = "demo"
members = ["lib", "app"]
`)

	cfg, err := decodeFile(path)
	if err != nil {
		t.Fatalf("decodeFile() error = %v", err)
	}
	if cfg.Workspace == nil {
		t.Fatal("Workspace = nil, want value")
	}
	if cfg.Workspace.Name != "demo" {
		t.Errorf("Name = %q, want demo", cfg.Workspace.Name)
	}
	if len(cfg.Workspace.Members) != 2 {
		t.Fatalf("Members = %v, want 2 entries", cfg.Workspace.Members)
	}
	if cfg.Project != nil {
		t.Error("Project != nil, want nil")
	}
}

func TestDecodeProject(t *testing.T) {
	path := writeConfig(t, `[project]
provider = "jvm"
name = "app"
depends_on = ["lib"]

[project.options]
main_class = "demo.App"
`)

	cfg, err := decodeFile(path)
	if err != nil {
		t.Fatalf("decodeFile() error = %v", err)
	}
	if cfg.Project == nil {
		t.Fatal("Project = nil, want value")
	}
	if cfg.Project.Provider != "jvm" {
		t.Errorf("Provider = %q, want jvm", cfg.Project.Provider)
	}
	if cfg.Project.Name != "app" {
		t.Errorf("Name = %q, want app", cfg.Project.Name)
	}
	if len(cfg.Project.DependsOn) != 1 || cfg.Project.DependsOn[0] != "lib" {
		t.Errorf("DependsOn = %v, want [lib]", cfg.Project.DependsOn)
	}
	if cfg.Project.Options["main_class"] != "demo.App" {
		t.Errorf("Options[main_class] = %v, want demo.App", cfg.Project.Options["main_class"])
	}
	if cfg.Workspace != nil {
		t.Error("Workspace != nil, want nil")
	}
}

func TestDecodeRejectsBothTables(t *testing.T) {
	path := writeConfig(t, `[workspace]
name = "demo"
members = ["app"]

[project]
provider = "jvm"
`)

	_, err := decodeFile(path)
	if err == nil {
		t.Fatal("decodeFile() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "exactly one") {
		t.Errorf("error %q does not mention exactly one table", err)
	}
}

func TestDecodeRejectsEmptyFile(t *testing.T) {
	path := writeConfig(t, "")

	_, err := decodeFile(path)
	if err == nil {
		t.Fatal("decodeFile() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "exactly one") {
		t.Errorf("error %q does not mention exactly one table", err)
	}
}

func TestDecodeRejectsUnknownKey(t *testing.T) {
	path := writeConfig(t, `[workspace]
name = "demo"
members = ["app"]
extra = true
`)

	_, err := decodeFile(path)
	if err == nil {
		t.Fatal("decodeFile() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "extra") {
		t.Errorf("error %q does not name the unknown key", err)
	}
}

func TestDecodeRejectsUnknownTopLevelKey(t *testing.T) {
	path := writeConfig(t, `[workspace]
name = "demo"
members = ["app"]

[other]
value = 1
`)

	_, err := decodeFile(path)
	if err == nil {
		t.Fatal("decodeFile() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "other") {
		t.Errorf("error %q does not name the unknown table", err)
	}
}

func TestDecodeRejectsEmptyMembers(t *testing.T) {
	path := writeConfig(t, `[workspace]
name = "demo"
`)

	_, err := decodeFile(path)
	if err == nil {
		t.Fatal("decodeFile() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "member") {
		t.Errorf("error %q does not mention members", err)
	}
}

func TestDecodeRejectsEmptyProvider(t *testing.T) {
	path := writeConfig(t, `[project]
name = "app"
`)

	_, err := decodeFile(path)
	if err == nil {
		t.Fatal("decodeFile() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "provider") {
		t.Errorf("error %q does not mention provider", err)
	}
}

func TestDecodeErrorIncludesPathAndPosition(t *testing.T) {
	path := writeConfig(t, `[workspace]
name = 1
members = ["app"]
`)

	_, err := decodeFile(path)
	if err == nil {
		t.Fatal("decodeFile() error = nil, want error")
	}
	if !strings.Contains(err.Error(), path+":2:") {
		t.Errorf("error %q does not include the file path and line number", err)
	}
	if !strings.Contains(err.Error(), "name") {
		t.Errorf("error %q does not include the key", err)
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "kine.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
