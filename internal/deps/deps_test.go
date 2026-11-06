package deps

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/store"
)

func TestResolveEndToEnd(t *testing.T) {
	workspace := t.TempDir()
	cache := newEndToEndCache(t, workspace)
	contents := endToEndContents()
	server, requests := newChecksumServer(t, contents, nil)
	repos := []Repository{{ID: "test", URL: server.URL}}
	roots := endToEndRoots()

	first, err := Resolve(context.Background(), workspace, cache, roots, Options{
		Repositories: repos,
		Logger:       discardLogger(),
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if !first.Resolved {
		t.Error("Resolved = false, want true")
	}
	if len(first.Artifacts) != 2 {
		t.Fatalf("artifacts = %d, want 2", len(first.Artifacts))
	}
	for _, artifact := range first.Artifacts {
		data, err := os.ReadFile(artifact.Path)
		if err != nil {
			t.Fatalf("read %s: %v", artifact.Path, err)
		}
		if string(data) != "app" && string(data) != "lib" {
			t.Errorf("artifact content = %q", data)
		}
		blobPath, err := cache.BlobPath(artifact.SHA256)
		if err != nil {
			t.Fatal(err)
		}
		blobInfo, err := os.Stat(blobPath)
		if err != nil {
			t.Fatal(err)
		}
		destInfo, err := os.Stat(artifact.Path)
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(blobInfo, destInfo) {
			t.Errorf("%s is not a hardlink", artifact.Path)
		}
	}
	if _, err := os.Stat(filepath.Join(workspace, "kine.lock")); err != nil {
		t.Fatalf("lock file missing: %v", err)
	}

	before := requests.Load()
	second, err := Resolve(context.Background(), workspace, cache, roots, Options{
		Repositories: repos,
		Logger:       discardLogger(),
	})
	if err != nil {
		t.Fatalf("second Resolve() error = %v", err)
	}
	if second.Resolved {
		t.Error("Resolved = true on a lock hit, want false")
	}
	if requests.Load() != before {
		t.Errorf("requests = %d, want %d on a lock hit", requests.Load(), before)
	}

	third, err := Resolve(context.Background(), workspace, cache, roots, Options{
		Repositories: repos,
		Logger:       discardLogger(),
		Update:       true,
	})
	if err != nil {
		t.Fatalf("update Resolve() error = %v", err)
	}
	if !third.Resolved {
		t.Error("Resolved = false on update, want true")
	}
	if requests.Load() <= before {
		t.Error("update made no requests")
	}
}

func TestResolveGradleModuleMetadata(t *testing.T) {
	workspace := t.TempDir()
	cache := newEndToEndCache(t, workspace)
	contents := map[string]string{
		"/com/example/app/1.0/app-1.0.module": `{
  "formatVersion": "1.1",
  "component": {"group": "com.example", "module": "app", "version": "1.0"},
  "variants": [
    {
      "name": "debugApi",
      "attributes": {"org.gradle.usage": "java-api", "com.android.build.api.attributes.BuildTypeAttr": "debug"},
      "files": [{"name": "app-debug.jar", "url": "app-debug.jar"}]
    },
    {
      "name": "releaseRuntime",
      "attributes": {"org.gradle.usage": "java-runtime", "com.android.build.api.attributes.BuildTypeAttr": "release", "org.jetbrains.kotlin.platform.type": "androidJvm"},
      "files": [{"name": "app-1.0.aar", "url": "app-1.0.aar"}],
      "dependencies": [{"group": "com.example", "module": "lib", "version": {"requires": "1.0"}}]
    }
  ]
}`,
		"/com/example/app/1.0/app-1.0.aar":   "app-aar",
		"/com/example/app/1.0/app-debug.jar": "app-debug",
		pomPath("com.example", "lib", "1.0"): projectXML("com.example", "lib", "1.0", ""),
		"/com/example/lib/1.0/lib-1.0.jar":   "lib",
	}
	server, _ := newChecksumServer(t, contents, nil)
	result, err := Resolve(context.Background(), workspace, cache, []Dependency{{
		Coordinate: Coordinate{Group: "com.example", Artifact: "app", Version: "1.0", Extension: "jar"},
		Scope:      ScopeCompile,
	}}, Options{
		Repositories: []Repository{{ID: "test", URL: server.URL}},
		Logger:       discardLogger(),
		Attributes: map[string]string{
			"org.gradle.usage": "java-runtime",
			"com.android.build.api.attributes.BuildTypeAttr": "release",
			"org.jetbrains.kotlin.platform.type":             "androidJvm",
		},
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	byArtifact := make(map[string]Artifact)
	for _, artifact := range result.Artifacts {
		byArtifact[artifact.Artifact] = artifact
	}
	app := byArtifact["app"]
	if app.Extension != "aar" || app.FileName() != "app-1.0.aar" {
		t.Errorf("app artifact = %+v", app.Coordinate)
	}
	if byArtifact["lib"].Extension != "jar" {
		t.Errorf("lib artifact = %+v", byArtifact["lib"].Coordinate)
	}
	if _, ok := byArtifact["app-debug"]; ok {
		t.Error("debug variant was selected")
	}
}

func TestResolveGradleModuleMetadataAvailableAt(t *testing.T) {
	workspace := t.TempDir()
	cache := newEndToEndCache(t, workspace)
	contents := map[string]string{
		"/com/example/app/1.0/app-1.0.module": `{
  "formatVersion": "1.1",
  "variants": [{
    "name": "releaseRuntime",
    "attributes": {"org.gradle.usage": "java-runtime", "com.android.build.api.attributes.BuildTypeAttr": "release"},
    "available-at": {"group": "com.example", "module": "app-android", "version": "1.0"}
  }]
}`,
		"/com/example/app-android/1.0/app-android-1.0.module": `{
  "formatVersion": "1.1",
  "variants": [{
    "name": "releaseRuntime",
    "attributes": {"org.gradle.usage": "java-runtime", "com.android.build.api.attributes.BuildTypeAttr": "release"},
    "files": [{"name": "app-android-1.0.aar", "url": "app-android-1.0.aar"}]
  }]
}`,
		"/com/example/app-android/1.0/app-android-1.0.aar": "app-android",
	}
	server, _ := newChecksumServer(t, contents, nil)
	result, err := Resolve(context.Background(), workspace, cache, []Dependency{{
		Coordinate: Coordinate{Group: "com.example", Artifact: "app", Version: "1.0", Extension: "jar"},
		Scope:      ScopeCompile,
	}}, Options{
		Repositories: []Repository{{ID: "test", URL: server.URL}},
		Logger:       discardLogger(),
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(result.Artifacts) != 1 {
		t.Fatalf("artifacts = %d, want 1", len(result.Artifacts))
	}
	artifact := result.Artifacts[0]
	if artifact.Group != "com.example" || artifact.Artifact != "app-android" || artifact.Extension != "aar" {
		t.Errorf("artifact = %+v", artifact.Coordinate)
	}
}

func TestResolveDependencyConstraintRaisesVersion(t *testing.T) {
	workspace := t.TempDir()
	cache := newEndToEndCache(t, workspace)
	contents := map[string]string{
		"/com/example/app/1.0/app-1.0.module": `{
  "formatVersion": "1.1",
  "variants": [{
    "name": "runtime",
    "attributes": {"org.gradle.usage": "java-runtime"},
    "files": [{"name": "app-1.0.jar", "url": "app-1.0.jar"}],
    "dependencies": [{"group": "com.example", "module": "lib", "version": {"requires": "1.0"}}],
    "dependencyConstraints": [{"group": "com.example", "module": "lib", "version": {"requires": "2.0"}}]
  }]
}`,
		"/com/example/app/1.0/app-1.0.jar":   "app",
		pomPath("com.example", "lib", "2.0"): projectXML("com.example", "lib", "2.0", ""),
		"/com/example/lib/2.0/lib-2.0.jar":   "lib2",
	}
	server, _ := newChecksumServer(t, contents, nil)
	result, err := Resolve(context.Background(), workspace, cache, []Dependency{{
		Coordinate: Coordinate{Group: "com.example", Artifact: "app", Version: "1.0", Extension: "jar"},
		Scope:      ScopeCompile,
	}}, Options{
		Repositories: []Repository{{ID: "test", URL: server.URL}},
		Logger:       discardLogger(),
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	for _, artifact := range result.Artifacts {
		if artifact.Artifact == "lib" && artifact.Version != "2.0" {
			t.Errorf("lib version = %s, want 2.0", artifact.Version)
		}
	}
}

func TestResolveStrictConstraintConflict(t *testing.T) {
	workspace := t.TempDir()
	cache := newEndToEndCache(t, workspace)
	contents := map[string]string{
		"/com/example/app/1.0/app-1.0.module": `{
  "formatVersion": "1.1",
  "variants": [{
    "name": "runtime",
    "attributes": {"org.gradle.usage": "java-runtime"},
    "files": [{"name": "app-1.0.jar", "url": "app-1.0.jar"}],
    "dependencies": [{"group": "com.example", "module": "lib", "version": {"requires": "2.0"}}],
    "dependencyConstraints": [{"group": "com.example", "module": "lib", "version": {"strictly": "1.0"}}]
  }]
}`,
		"/com/example/app/1.0/app-1.0.jar": "app",
	}
	server, _ := newChecksumServer(t, contents, nil)
	_, err := Resolve(context.Background(), workspace, cache, []Dependency{{
		Coordinate: Coordinate{Group: "com.example", Artifact: "app", Version: "1.0", Extension: "jar"},
		Scope:      ScopeCompile,
	}}, Options{
		Repositories: []Repository{{ID: "test", URL: server.URL}},
		Logger:       discardLogger(),
	})
	if err == nil || !strings.Contains(err.Error(), "version conflict") {
		t.Fatalf("Resolve() error = %v, want version conflict", err)
	}
}

func TestResolveOffline(t *testing.T) {
	workspace := t.TempDir()
	cache := newEndToEndCache(t, workspace)
	server, _ := newChecksumServer(t, endToEndContents(), nil)
	repos := []Repository{{ID: "test", URL: server.URL}}
	roots := endToEndRoots()

	first, err := Resolve(context.Background(), workspace, cache, roots, Options{
		Repositories: repos,
		Logger:       discardLogger(),
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	target := first.Artifacts[0]
	blobPath, err := cache.BlobPath(target.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(blobPath); err != nil {
		t.Fatal(err)
	}

	_, err = Resolve(context.Background(), workspace, cache, roots, Options{
		Repositories: repos,
		Logger:       discardLogger(),
		Offline:      true,
	})
	if err == nil {
		t.Fatal("Resolve() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "offline") {
		t.Errorf("error %q does not mention offline", err)
	}
}

func TestResolveChecksumMismatch(t *testing.T) {
	workspace := t.TempDir()
	cache := newEndToEndCache(t, workspace)
	wrong := map[string]string{
		"/com/example/lib/1.0/lib-1.0.jar": strings.Repeat("0", 64),
	}
	server, _ := newChecksumServer(t, endToEndContents(), wrong)
	repos := []Repository{{ID: "test", URL: server.URL}}

	_, err := Resolve(context.Background(), workspace, cache, endToEndRoots(), Options{
		Repositories: repos,
		Logger:       discardLogger(),
	})
	if err == nil {
		t.Fatal("Resolve() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("error %q does not mention checksum mismatch", err)
	}
}

func TestResolveSHA1Fallback(t *testing.T) {
	workspace := t.TempDir()
	cache := newEndToEndCache(t, workspace)
	contents := endToEndContents()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasSuffix(path, ".sha256") {
			http.NotFound(w, r)
			return
		}
		if strings.HasSuffix(path, ".sha1") {
			base := strings.TrimSuffix(path, ".sha1")
			content, ok := contents[base]
			if !ok {
				http.NotFound(w, r)
				return
			}
			sum := sha1.Sum([]byte(content))
			w.Write([]byte(hex.EncodeToString(sum[:])))
			return
		}
		content, ok := contents[path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(content))
	}))
	t.Cleanup(server.Close)

	_, err := Resolve(context.Background(), workspace, cache, endToEndRoots(), Options{
		Repositories: []Repository{{ID: "test", URL: server.URL}},
		Logger:       discardLogger(),
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
}

func TestResolveFallsBackToSecondRepository(t *testing.T) {
	workspace := t.TempDir()
	cache := newEndToEndCache(t, workspace)
	firstRepo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(firstRepo.Close)
	secondRepo, _ := newChecksumServer(t, endToEndContents(), nil)
	repos := []Repository{
		{ID: "first", URL: firstRepo.URL},
		{ID: "second", URL: secondRepo.URL},
	}

	result, err := Resolve(context.Background(), workspace, cache, endToEndRoots(), Options{
		Repositories: repos,
		Logger:       discardLogger(),
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if len(result.Artifacts) != 2 {
		t.Fatalf("artifacts = %d, want 2", len(result.Artifacts))
	}
}

func TestResolveRootChange(t *testing.T) {
	workspace := t.TempDir()
	cache := newEndToEndCache(t, workspace)
	server, _ := newChecksumServer(t, endToEndContents(), nil)
	repos := []Repository{{ID: "test", URL: server.URL}}

	if _, err := Resolve(context.Background(), workspace, cache, endToEndRoots(), Options{
		Repositories: repos,
		Logger:       discardLogger(),
	}); err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	roots := []Dependency{{
		Coordinate: Coordinate{Group: "com.example", Artifact: "lib", Version: "1.0", Extension: "jar"},
		Scope:      ScopeCompile,
	}}
	result, err := Resolve(context.Background(), workspace, cache, roots, Options{
		Repositories: repos,
		Logger:       discardLogger(),
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if !result.Resolved {
		t.Error("Resolved = false after a root change, want true")
	}
	if len(result.Artifacts) != 1 || result.Artifacts[0].Artifact != "lib" {
		t.Errorf("artifacts = %+v, want lib only", result.Artifacts)
	}
}

func newEndToEndCache(t *testing.T, workspace string) *store.Store {
	t.Helper()

	cache, err := store.Open(workspace, filepath.Join(t.TempDir(), "cache"), store.Options{Logger: discardLogger()})
	if err != nil {
		t.Fatal(err)
	}
	return cache
}

func endToEndRoots() []Dependency {
	return []Dependency{{
		Coordinate: Coordinate{Group: "com.example", Artifact: "app", Version: "1.0", Extension: "jar"},
		Scope:      ScopeCompile,
	}}
}

func endToEndContents() map[string]string {
	return map[string]string{
		pomPath("com.example", "app", "1.0"): projectXML("com.example", "app", "1.0",
			`<dependencies>`+dependencyXML("com.example", "lib", "1.0")+`</dependencies>`),
		pomPath("com.example", "lib", "1.0"): projectXML("com.example", "lib", "1.0", ""),
		"/com/example/app/1.0/app-1.0.jar":   "app",
		"/com/example/lib/1.0/lib-1.0.jar":   "lib",
	}
}

func newChecksumServer(t *testing.T, contents, wrong map[string]string) (*httptest.Server, *atomic.Int32) {
	t.Helper()

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		path := r.URL.Path

		switch {
		case strings.HasSuffix(path, ".sha256"):
			base := strings.TrimSuffix(path, ".sha256")
			content, ok := contents[base]
			if !ok {
				http.NotFound(w, r)
				return
			}
			if value, ok := wrong[base]; ok {
				w.Write([]byte(value))
				return
			}
			sum := sha256.Sum256([]byte(content))
			w.Write([]byte(hex.EncodeToString(sum[:])))

		case strings.HasSuffix(path, ".sha1"):
			base := strings.TrimSuffix(path, ".sha1")
			content, ok := contents[base]
			if !ok {
				http.NotFound(w, r)
				return
			}
			if value, ok := wrong[base]; ok {
				w.Write([]byte(value))
				return
			}
			sum := sha1.Sum([]byte(content))
			w.Write([]byte(hex.EncodeToString(sum[:])))

		default:
			content, ok := contents[path]
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Write([]byte(content))
		}
	}))
	t.Cleanup(server.Close)
	return server, &requests
}
