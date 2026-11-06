package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigMissing(t *testing.T) {
	jars, err := LoadConfig(t.TempDir())
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if len(jars) != 0 {
		t.Errorf("jars = %v, want empty", jars)
	}
}

func TestLoadConfig(t *testing.T) {
	root := t.TempDir()
	jar := filepath.Join(root, "plugins", "example.jar")
	writeTestFile(t, jar, "jar")
	writeTestFile(t, filepath.Join(root, configFileName), `jars = ["plugins/example.jar"]`)

	jars, err := LoadConfig(root)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if len(jars) != 1 || jars[0] != jar {
		t.Errorf("jars = %v, want [%s]", jars, jar)
	}
}

func TestLoadConfigUnknownKey(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, configFileName), "jars = []\nextra = true\n")

	_, err := LoadConfig(root)
	if err == nil {
		t.Fatal("LoadConfig() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "strict mode") {
		t.Errorf("error %q does not report the unknown key", err)
	}
}

func TestLoadConfigEmptyJars(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, configFileName), `jars = []`)

	_, err := LoadConfig(root)
	if err == nil {
		t.Fatal("LoadConfig() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error %q does not mention the empty list", err)
	}
}

func TestLoadConfigAbsolutePath(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, configFileName), `jars = ["/tmp/example.jar"]`)

	_, err := LoadConfig(root)
	if err == nil {
		t.Fatal("LoadConfig() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "relative") {
		t.Errorf("error %q does not mention relative paths", err)
	}
}

func TestLoadConfigEscape(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, configFileName), `jars = ["../outside.jar"]`)

	_, err := LoadConfig(root)
	if err == nil {
		t.Fatal("LoadConfig() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "escape") {
		t.Errorf("error %q does not mention the escape", err)
	}
}

func TestLoadConfigMissingJar(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, configFileName), `jars = ["plugins/missing.jar"]`)

	_, err := LoadConfig(root)
	if err == nil {
		t.Fatal("LoadConfig() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "missing.jar") {
		t.Errorf("error %q does not name the jar", err)
	}
}

func TestLoadConfigDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "plugins", "example.jar"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, configFileName), `jars = ["plugins/example.jar"]`)

	_, err := LoadConfig(root)
	if err == nil {
		t.Fatal("LoadConfig() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "regular file") {
		t.Errorf("error %q does not mention a regular file", err)
	}
}

func TestLoadConfigDuplicate(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "plugins", "example.jar"), "jar")
	writeTestFile(t, filepath.Join(root, configFileName),
		`jars = ["plugins/example.jar", "./plugins/example.jar"]`)

	_, err := LoadConfig(root)
	if err == nil {
		t.Fatal("LoadConfig() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error %q does not mention the duplicate", err)
	}
}

func TestLoadConfigEmptyPath(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, configFileName), `jars = [""]`)

	_, err := LoadConfig(root)
	if err == nil {
		t.Fatal("LoadConfig() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error %q does not mention the empty path", err)
	}
}

func TestLoadConfigSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.jar")
	writeTestFile(t, outside, "jar")
	link := filepath.Join(root, "linked.jar")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks are not available: %v", err)
	}
	writeTestFile(t, filepath.Join(root, configFileName), `jars = ["linked.jar"]`)

	_, err := LoadConfig(root)
	if err == nil {
		t.Fatal("LoadConfig() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "escape") {
		t.Errorf("error %q does not mention the escape", err)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
