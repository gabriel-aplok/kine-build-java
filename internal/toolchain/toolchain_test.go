package toolchain

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDiscover(t *testing.T) {
	root := t.TempDir()
	jdk17 := writeFakeJDK(t, root, "jdk-17", "17.0.2", "TestVendor")
	writeFakeJDK(t, root, "jdk-21", "21.0.1", "OtherVendor")
	writeFakeJRE(t, root, "jre-21", "21.0.1")

	jdks, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(jdks) != 2 {
		t.Fatalf("len(jdks) = %d, want 2", len(jdks))
	}
	if jdks[0].Major != 21 || jdks[1].Major != 17 {
		t.Errorf("majors = %d, %d, want 21, 17", jdks[0].Major, jdks[1].Major)
	}
	if jdks[1].Home != resolvedPath(t, jdk17) {
		t.Errorf("Home = %q, want %q", jdks[1].Home, resolvedPath(t, jdk17))
	}
	if jdks[0].Vendor != "OtherVendor" {
		t.Errorf("Vendor = %q, want OtherVendor", jdks[0].Vendor)
	}
	if jdks[0].Java != filepath.Join(jdks[0].Home, "bin", "java") {
		t.Errorf("Java = %q", jdks[0].Java)
	}
	if jdks[0].Javac != filepath.Join(jdks[0].Home, "bin", "javac") {
		t.Errorf("Javac = %q", jdks[0].Javac)
	}
}

func TestDiscoverJavaHome(t *testing.T) {
	fake := writeFakeJDK(t, t.TempDir(), "jdk-17", "17.0.2", "Test")
	t.Setenv("JAVA_HOME", fake)

	jdks, err := Discover()
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	want := resolvedPath(t, fake)
	for _, jdk := range jdks {
		if jdk.Home == want {
			return
		}
	}
	t.Errorf("JAVA_HOME %q was not discovered", fake)
}

func TestDiscoverSkipsNonJDK(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "not-a-jdk", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}

	jdks, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(jdks) != 0 {
		t.Fatalf("len(jdks) = %d, want 0", len(jdks))
	}
}

func TestFind(t *testing.T) {
	root := t.TempDir()
	writeFakeJDK(t, root, "jdk-17", "17.0.2", "Test")
	writeFakeJDK(t, root, "jdk-21a", "21.0.1", "Test")
	writeFakeJDK(t, root, "jdk-21b", "21.0.3", "Test")

	jdk, err := Find(Requirement{Major: 17}, root)
	if err != nil {
		t.Fatalf("Find(17) error = %v", err)
	}
	if jdk.Major != 17 {
		t.Errorf("Major = %d, want 17", jdk.Major)
	}

	highest, err := Find(Requirement{Major: 21}, root)
	if err != nil {
		t.Fatalf("Find(21) error = %v", err)
	}
	if highest.Version != "21.0.3" {
		t.Errorf("Version = %q, want 21.0.3", highest.Version)
	}

	any, err := Find(Requirement{}, root)
	if err != nil {
		t.Fatalf("Find(0) error = %v", err)
	}
	if any.Major != 21 {
		t.Errorf("Major = %d, want 21", any.Major)
	}

	if _, err := Find(Requirement{Major: 99}, root); !errors.Is(err, ErrJDKNotFound) {
		t.Errorf("Find(99) error = %v, want ErrJDKNotFound", err)
	}
}

func TestParseRelease(t *testing.T) {
	values := parseRelease([]byte("JAVA_VERSION=\"1.8.0_402\"\nIMPLEMENTOR=\"Test Vendor\"\n"))
	if values["JAVA_VERSION"] != "1.8.0_402" {
		t.Errorf("JAVA_VERSION = %q", values["JAVA_VERSION"])
	}
	if values["IMPLEMENTOR"] != "Test Vendor" {
		t.Errorf("IMPLEMENTOR = %q", values["IMPLEMENTOR"])
	}
}

func writeFakeJDK(t *testing.T, root, name, version, vendor string) string {
	t.Helper()

	home := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Join(home, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(home, "bin", "javac"), "")
	writeFile(t, filepath.Join(home, "bin", "java"), "")
	writeFile(t, filepath.Join(home, "release"),
		`JAVA_VERSION="`+version+`"`+"\n"+`IMPLEMENTOR="`+vendor+`"`+"\n")
	return home
}

func writeFakeJRE(t *testing.T, root, name, version string) string {
	t.Helper()

	home := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Join(home, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(home, "bin", "java"), "")
	writeFile(t, filepath.Join(home, "release"), `JAVA_VERSION="`+version+`"`+"\n")
	return home
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func resolvedPath(t *testing.T, path string) string {
	t.Helper()

	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func TestToolPath(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	name := executableName("keytool")
	tool := filepath.Join(bin, name)
	if err := os.WriteFile(tool, []byte("tool"), 0o755); err != nil {
		t.Fatal(err)
	}

	jdk := &JDK{Home: home, Version: "21.0.1"}
	path, err := ToolPath(jdk, "keytool")
	if err != nil {
		t.Fatalf("ToolPath() error = %v", err)
	}
	if path != tool {
		t.Errorf("path = %q, want %q", path, tool)
	}
	if _, err := ToolPath(jdk, "missing"); err == nil {
		t.Error("ToolPath() error = nil, want error")
	}
}
