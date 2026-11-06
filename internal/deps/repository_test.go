package deps

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultRepositories(t *testing.T) {
	repos := DefaultRepositories()
	if len(repos) != 3 {
		t.Fatalf("len(repos) = %d, want 3", len(repos))
	}
	want := []string{"local", "central", "google"}
	for i, repo := range repos {
		if repo.ID != want[i] {
			t.Errorf("repos[%d].ID = %q, want %q", i, repo.ID, want[i])
		}
		if repo.URL == "" {
			t.Errorf("repos[%d].URL is empty", i)
		}
	}
}

func TestLoadRepositoriesDefaults(t *testing.T) {
	repos, err := LoadRepositories(t.TempDir())
	if err != nil {
		t.Fatalf("LoadRepositories() error = %v", err)
	}
	if len(repos) != len(DefaultRepositories()) {
		t.Fatalf("len(repos) = %d, want %d", len(repos), len(DefaultRepositories()))
	}
}

func TestLoadRepositories(t *testing.T) {
	root := t.TempDir()
	writeRepositories(t, root, `[[repository]]
id = "one"
url = "https://example.com/one"

[[repository]]
id = "two"
url = "file:///tmp/two"
`)

	repos, err := LoadRepositories(root)
	if err != nil {
		t.Fatalf("LoadRepositories() error = %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("len(repos) = %d, want 2", len(repos))
	}
	if repos[0].ID != "one" || repos[1].ID != "two" {
		t.Errorf("repos = %+v, want one then two", repos)
	}
}

func TestToolRepositoriesAppendsDefaults(t *testing.T) {
	root := t.TempDir()
	writeRepositories(t, root, `[[repository]]
id = "one"
url = "https://example.com/one"
`)
	repos, err := ToolRepositories(root)
	if err != nil {
		t.Fatalf("ToolRepositories() error = %v", err)
	}
	if repos[0].ID != "one" {
		t.Errorf("first repository = %q", repos[0].ID)
	}
	seen := map[string]bool{}
	for _, repo := range repos {
		seen[repo.ID] = true
	}
	for _, want := range []string{"central", "google"} {
		if !seen[want] {
			t.Errorf("repos %+v do not contain %q", repos, want)
		}
	}
}

func TestLoadRepositoriesRejectsDuplicateID(t *testing.T) {
	root := t.TempDir()
	writeRepositories(t, root, `[[repository]]
id = "one"
url = "https://example.com/one"

[[repository]]
id = "one"
url = "https://example.com/two"
`)

	if _, err := LoadRepositories(root); err == nil {
		t.Fatal("LoadRepositories() error = nil, want error")
	}
}

func TestLoadRepositoriesRejectsBadScheme(t *testing.T) {
	root := t.TempDir()
	writeRepositories(t, root, `[[repository]]
id = "one"
url = "ftp://example.com/one"
`)

	if _, err := LoadRepositories(root); err == nil {
		t.Fatal("LoadRepositories() error = nil, want error")
	}
}

func TestLoadRepositoriesRejectsUnknownKey(t *testing.T) {
	root := t.TempDir()
	writeRepositories(t, root, `[[repository]]
id = "one"
url = "https://example.com/one"
extra = true
`)

	if _, err := LoadRepositories(root); err == nil {
		t.Fatal("LoadRepositories() error = nil, want error")
	}
}

func TestFetchBytesHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/com/example/lib/1.0/lib-1.0.pom" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte("pom"))
	}))
	defer server.Close()

	client := newClient()
	data, err := client.fetchBytes(
		context.Background(),
		Repository{ID: "test", URL: server.URL},
		"com/example/lib/1.0/lib-1.0.pom",
	)
	if err != nil {
		t.Fatalf("fetchBytes() error = %v", err)
	}
	if string(data) != "pom" {
		t.Errorf("fetchBytes() = %q, want pom", data)
	}
}

func TestFetchBytesHTTPNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := newClient()
	_, err := client.fetchBytes(context.Background(), Repository{ID: "test", URL: server.URL}, "missing.pom")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("fetchBytes() error = %v, want ErrNotFound", err)
	}
}

func TestFetchBytesHTTPStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := newClient()
	_, err := client.fetchBytes(context.Background(), Repository{ID: "test", URL: server.URL}, "broken.pom")
	if err == nil {
		t.Fatal("fetchBytes() error = nil, want error")
	}
}

func TestFetchBytesFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "com", "example", "lib", "1.0", "lib-1.0.pom")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("pom"), 0o600); err != nil {
		t.Fatal(err)
	}

	client := newClient()
	data, err := client.fetchBytes(
		context.Background(),
		Repository{ID: "local", URL: "file://" + root},
		"com/example/lib/1.0/lib-1.0.pom",
	)
	if err != nil {
		t.Fatalf("fetchBytes() error = %v", err)
	}
	if string(data) != "pom" {
		t.Errorf("fetchBytes() = %q, want pom", data)
	}
}

func TestFetchBytesFileNotFound(t *testing.T) {
	client := newClient()
	_, err := client.fetchBytes(
		context.Background(),
		Repository{ID: "local", URL: "file://" + t.TempDir()},
		"missing.pom",
	)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("fetchBytes() error = %v, want ErrNotFound", err)
	}
}

func TestFetchBytesFileEscape(t *testing.T) {
	client := newClient()
	_, err := client.fetchBytes(
		context.Background(),
		Repository{ID: "local", URL: "file://" + t.TempDir()},
		"../outside.pom",
	)
	if err == nil {
		t.Fatal("fetchBytes() error = nil, want error")
	}
}

func writeRepositories(t *testing.T, root, content string) {
	t.Helper()

	path := filepath.Join(root, "repositories.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFileURLRoundTrip(t *testing.T) {
	root := t.TempDir()
	parsed, err := url.Parse(fileURL(root))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	path, err := fileURLPath(parsed)
	if err != nil {
		t.Fatalf("fileURLPath() error = %v", err)
	}
	if path != root {
		t.Errorf("path = %q, want %q", path, root)
	}
}
