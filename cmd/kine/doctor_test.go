package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/android"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

func TestDoctorPasses(t *testing.T) {
	if _, err := toolchain.Find(toolchain.Requirement{}); err != nil {
		t.Skip("no JDK installed")
	}

	root := writeDoctorWorkspace(t, `[project]
name = "app"
provider = "jvm"
`)
	chdir(t, root)

	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor", "--cache-dir", t.TempDir()}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() = %d, stdout = %s, stderr = %s", code, stdout.String(), stderr.String())
	}
	for _, name := range []string{"workspace", "graph", "jdk", "cache", "repositories"} {
		if !strings.Contains(stdout.String(), name) {
			t.Errorf("stdout %q does not report %q", stdout.String(), name)
		}
	}
	if strings.Contains(stdout.String(), "fail") {
		t.Errorf("stdout %q contains a failure", stdout.String())
	}
}

func TestDoctorFailsWithoutConfig(t *testing.T) {
	chdir(t, t.TempDir())

	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor", "--cache-dir", t.TempDir()}, &stdout, &stderr)
	if code != exitBuildFailure {
		t.Fatalf("run() = %d, want %d", code, exitBuildFailure)
	}
	output := stdout.String()
	if !strings.Contains(output, "workspace") || !strings.Contains(output, "fail") {
		t.Errorf("stdout %q does not report the workspace failure", output)
	}
	if !strings.Contains(output, "repositories") || !strings.Contains(output, "ok") {
		t.Errorf("stdout %q does not report the remaining checks", output)
	}
}

func TestDoctorFailsOnGraph(t *testing.T) {
	root := writeDoctorWorkspace(t, `[project]
name = "app"
provider = "jvm"
depends_on = ["missing"]
`)
	chdir(t, root)

	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor", "--cache-dir", t.TempDir()}, &stdout, &stderr)
	if code != exitBuildFailure {
		t.Fatalf("run() = %d, want %d", code, exitBuildFailure)
	}
	if !strings.Contains(stdout.String(), "graph") || !strings.Contains(stdout.String(), "fail") {
		t.Errorf("stdout %q does not report the graph failure", stdout.String())
	}
}

func TestDoctorFailsOnRepositories(t *testing.T) {
	root := writeDoctorWorkspace(t, `[project]
name = "app"
provider = "jvm"
`)
	if err := os.WriteFile(filepath.Join(root, "repositories.toml"), []byte("[[repository]]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	chdir(t, root)

	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor", "--cache-dir", t.TempDir()}, &stdout, &stderr)
	if code != exitBuildFailure {
		t.Fatalf("run() = %d, want %d", code, exitBuildFailure)
	}
	if !strings.Contains(stdout.String(), "repositories") || !strings.Contains(stdout.String(), "fail") {
		t.Errorf("stdout %q does not report the repository failure", stdout.String())
	}
}

func writeDoctorWorkspace(t *testing.T, projectConfig string) string {
	t.Helper()

	root := t.TempDir()
	writeConfig(t, root, `[workspace]
name = "demo"
members = ["app"]
`)
	appDir := filepath.Join(root, "app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeConfig(t, appDir, projectConfig)
	return root
}

func TestDoctorReportsAndroid(t *testing.T) {
	if _, err := android.FindSDK(android.Requirement{}); err != nil {
		t.Skipf("no Android SDK: %v", err)
	}

	root := writeDoctorWorkspace(t, `[project]
name = "app"
provider = "android"
`)
	chdir(t, root)

	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor", "--cache-dir", t.TempDir()}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() = %d, stdout = %s, stderr = %s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "android") || !strings.Contains(stdout.String(), "ok") {
		t.Errorf("stdout %q does not report the android check", stdout.String())
	}
}

func TestDoctorFailsOnMinecraft(t *testing.T) {
	root := writeDoctorWorkspace(t, `[project]
name = "app"
provider = "minecraft"

[project.options]
version = "1.20.1"
minecraft_jar = "missing.jar"
`)
	chdir(t, root)

	var stdout, stderr bytes.Buffer
	code := run([]string{"doctor", "--cache-dir", t.TempDir()}, &stdout, &stderr)
	if code != exitBuildFailure {
		t.Fatalf("run() = %d, want %d", code, exitBuildFailure)
	}
	if !strings.Contains(stdout.String(), "minecraft") || !strings.Contains(stdout.String(), "fail") {
		t.Errorf("stdout %q does not report the minecraft failure", stdout.String())
	}
}
