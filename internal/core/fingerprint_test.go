package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFingerprintStable(t *testing.T) {
	root, task := fingerprintTask(t)
	deps := map[string]string{"lib:jar": "dep"}

	first := mustFingerprint(t, root, task, deps)
	second := mustFingerprint(t, root, task, deps)

	if first != second {
		t.Errorf("Fingerprint() is not stable: %s != %s", first, second)
	}
	if len(first) != 64 {
		t.Errorf("len(Fingerprint()) = %d, want 64", len(first))
	}
}

func TestFingerprintChangesWithContent(t *testing.T) {
	root, task := fingerprintTask(t)
	deps := map[string]string{"lib:jar": "dep"}

	before := mustFingerprint(t, root, task, deps)
	if err := os.WriteFile(filepath.Join(root, "input.txt"), []byte("two"), 0o600); err != nil {
		t.Fatal(err)
	}
	after := mustFingerprint(t, root, task, deps)

	if before == after {
		t.Error("Fingerprint() did not change after input content change")
	}
}

func TestFingerprintChangesWithKey(t *testing.T) {
	root, task := fingerprintTask(t)
	deps := map[string]string{"lib:jar": "dep"}

	before := mustFingerprint(t, root, task, deps)
	task.Key = "javac 22"
	after := mustFingerprint(t, root, task, deps)

	if before == after {
		t.Error("Fingerprint() did not change after key change")
	}
}

func TestFingerprintChangesWithDependency(t *testing.T) {
	root, task := fingerprintTask(t)

	before := mustFingerprint(t, root, task, map[string]string{"lib:jar": "one"})
	after := mustFingerprint(t, root, task, map[string]string{"lib:jar": "two"})

	if before == after {
		t.Error("Fingerprint() did not change after dependency change")
	}
}

func TestFingerprintInputOrder(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"), "a")
	writeFile(t, filepath.Join(root, "b.txt"), "b")
	task := &Task{
		Key: "k",
		Inputs: []Input{
			{Path: "a.txt", Mode: ModeFile},
			{Path: "b.txt", Mode: ModeFile},
		},
	}

	first := mustFingerprint(t, root, task, nil)
	task.Inputs = []Input{
		{Path: "b.txt", Mode: ModeFile},
		{Path: "a.txt", Mode: ModeFile},
	}
	second := mustFingerprint(t, root, task, nil)

	if first != second {
		t.Error("Fingerprint() depends on input order")
	}
}

func TestFingerprintDependencyOrder(t *testing.T) {
	root, task := fingerprintTask(t)
	task.Deps = []string{"a:jar", "b:jar"}
	deps := map[string]string{"a:jar": "one", "b:jar": "two"}

	first := mustFingerprint(t, root, task, deps)
	task.Deps = []string{"b:jar", "a:jar"}
	second := mustFingerprint(t, root, task, deps)

	if first != second {
		t.Error("Fingerprint() depends on dependency order")
	}
}

func TestFingerprintMissingDependency(t *testing.T) {
	task := &Task{Key: "k", Deps: []string{"missing:jar"}}

	_, err := Fingerprint(t.TempDir(), task, nil)
	if err == nil {
		t.Fatal("Fingerprint() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "missing:jar") {
		t.Errorf("error %q does not name the dependency", err)
	}
}

func TestFingerprintInvalidMode(t *testing.T) {
	task := &Task{
		Key:    "k",
		Inputs: []Input{{Path: "a", Mode: PathMode(99)}},
	}

	_, err := Fingerprint(t.TempDir(), task, nil)
	if err == nil {
		t.Fatal("Fingerprint() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "mode") {
		t.Errorf("error %q does not mention mode", err)
	}
}

func TestFingerprintMissingInput(t *testing.T) {
	task := &Task{
		Key:    "k",
		Inputs: []Input{{Path: "missing.txt", Mode: ModeFile}},
	}

	_, err := Fingerprint(t.TempDir(), task, nil)
	if err == nil {
		t.Fatal("Fingerprint() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "missing.txt") {
		t.Errorf("error %q does not name the input", err)
	}
}

func TestFingerprintTree(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "src")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(source, "a.txt"), "one")

	task := &Task{
		Key:    "k",
		Inputs: []Input{{Path: "src", Mode: ModeTree}},
	}
	before := mustFingerprint(t, root, task, nil)

	writeFile(t, filepath.Join(source, "a.txt"), "two")
	after := mustFingerprint(t, root, task, nil)

	if before == after {
		t.Error("Fingerprint() did not change after tree content change")
	}
}

func TestFingerprintUsesInputPath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.txt"), "same")
	writeFile(t, filepath.Join(root, "b.txt"), "same")

	task := &Task{
		Key:    "k",
		Inputs: []Input{{Path: "a.txt", Mode: ModeFile}},
	}
	first := mustFingerprint(t, root, task, nil)

	task.Inputs[0].Path = "b.txt"
	second := mustFingerprint(t, root, task, nil)

	if first == second {
		t.Error("Fingerprint() ignores the input path")
	}
}

func fingerprintTask(t *testing.T) (string, *Task) {
	t.Helper()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "input.txt"), "one")
	return root, &Task{
		ID:     "app:compile",
		Inputs: []Input{{Path: "input.txt", Mode: ModeFile}},
		Key:    "javac 21",
		Deps:   []string{"lib:jar"},
	}
}

func mustFingerprint(t *testing.T, root string, task *Task, deps map[string]string) string {
	t.Helper()

	sum, err := Fingerprint(root, task, deps)
	if err != nil {
		t.Fatalf("Fingerprint() error = %v", err)
	}
	return sum
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
