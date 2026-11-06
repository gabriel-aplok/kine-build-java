package deps

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLockRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kine.lock")
	repos := []Repository{{ID: "test", URL: "https://example.com"}}
	roots := []Dependency{{
		Coordinate: Coordinate{Group: "com.example", Artifact: "app", Version: "1.0", Extension: "jar"},
		Scope:      ScopeCompile,
	}}
	lock := &lockFile{
		Version:      lockVersion,
		Repositories: repos,
		Roots:        lockRoots(roots),
		Artifacts: []lockArtifact{{
			Group:      "com.example",
			Artifact:   "app",
			Version:    "1.0",
			Extension:  "jar",
			Scope:      "compile",
			SHA256:     strings.Repeat("a", 64),
			Size:       1,
			Repository: "test",
		}},
	}

	if err := writeLock(path, lock); err != nil {
		t.Fatalf("writeLock() error = %v", err)
	}
	got, err := readLock(path)
	if err != nil {
		t.Fatalf("readLock() error = %v", err)
	}
	if !reflect.DeepEqual(got, lock) {
		t.Errorf("lock = %+v, want %+v", got, lock)
	}
	if !lockReusable(got, repos, roots) {
		t.Error("lockReusable() = false, want true")
	}

	changed := []Dependency{{
		Coordinate: Coordinate{Group: "com.example", Artifact: "other", Version: "1.0", Extension: "jar"},
		Scope:      ScopeCompile,
	}}
	if lockReusable(got, repos, changed) {
		t.Error("lockReusable() = true for changed roots")
	}
	if lockReusable(got, []Repository{{ID: "other", URL: "https://example.com"}}, roots) {
		t.Error("lockReusable() = true for changed repositories")
	}
}

func TestLockRejectsUnknownVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kine.lock")
	if err := os.WriteFile(path, []byte(`{"version":2,"repositories":[],"roots":[],"artifacts":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := readLock(path); err == nil {
		t.Fatal("readLock() error = nil, want error")
	}
}
