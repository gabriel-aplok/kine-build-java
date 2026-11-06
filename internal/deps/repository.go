package deps

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

const requestTimeout = 60 * time.Second

// ErrNotFound is returned when a repository does not have a file.
var ErrNotFound = errors.New("not found")

// Repository is one Maven repository.
type Repository struct {
	ID  string `toml:"id"`
	URL string `toml:"url"`
}

type repositoriesFile struct {
	Repositories []Repository `toml:"repository"`
}

// DefaultRepositories returns Maven Local, Central, and Google Maven.
func DefaultRepositories() []Repository {
	var repos []Repository
	if home, err := os.UserHomeDir(); err == nil {
		repos = append(repos, Repository{
			ID:  "local",
			URL: fileURL(filepath.Join(home, ".m2", "repository")),
		})
	}
	return append(repos,
		Repository{ID: "central", URL: "https://repo.maven.apache.org/maven2"},
		Repository{ID: "google", URL: "https://dl.google.com/dl/android/maven2"},
	)
}

// ToolRepositories loads the workspace repositories and appends the default
// Maven repositories. Project dependencies use LoadRepositories; build tools
// such as bundletool and manifest-merger must still be resolvable when a
// project uses a private repository list.
func ToolRepositories(workspaceRoot string) ([]Repository, error) {
	repositories, err := LoadRepositories(workspaceRoot)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(repositories))
	for _, repository := range repositories {
		seen[repository.ID] = struct{}{}
	}
	for _, repository := range DefaultRepositories() {
		if _, ok := seen[repository.ID]; ok {
			continue
		}
		seen[repository.ID] = struct{}{}
		repositories = append(repositories, repository)
	}
	return repositories, nil
}

// LoadRepositories reads repositories.toml or returns the defaults.
func LoadRepositories(workspaceRoot string) ([]Repository, error) {
	path := filepath.Join(workspaceRoot, "repositories.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return DefaultRepositories(), nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var file repositoriesFile
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := validateRepositories(file.Repositories); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return file.Repositories, nil
}

func validateRepositories(repos []Repository) error {
	if len(repos) == 0 {
		return fmt.Errorf("at least one repository is required")
	}

	seen := make(map[string]struct{}, len(repos))
	for _, repo := range repos {
		if repo.ID == "" {
			return fmt.Errorf("repository id is empty")
		}
		if _, ok := seen[repo.ID]; ok {
			return fmt.Errorf("duplicate repository id %q", repo.ID)
		}
		seen[repo.ID] = struct{}{}

		parsed, err := url.Parse(repo.URL)
		if err != nil {
			return fmt.Errorf("repository %q: %w", repo.ID, err)
		}
		switch parsed.Scheme {
		case "http", "https", "file":
		default:
			return fmt.Errorf("repository %q: unsupported scheme %q", repo.ID, parsed.Scheme)
		}
	}
	return nil
}

type client struct {
	http *http.Client
}

func newClient() *client {
	return &client{http: &http.Client{Timeout: requestTimeout}}
}

func (c *client) fetchBytes(ctx context.Context, repo Repository, path string) ([]byte, error) {
	reader, err := c.open(ctx, repo, path)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read %s/%s: %w", repo.ID, path, err)
	}
	return data, nil
}

func (c *client) open(ctx context.Context, repo Repository, path string) (io.ReadCloser, error) {
	parsed, err := url.Parse(repo.URL)
	if err != nil {
		return nil, fmt.Errorf("repository %q: %w", repo.ID, err)
	}

	switch parsed.Scheme {
	case "http", "https":
		target := strings.TrimSuffix(repo.URL, "/") + "/" + path
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return nil, fmt.Errorf("request %s: %w", target, err)
		}
		request.Header.Set("User-Agent", "kine")

		response, err := c.http.Do(request)
		if err != nil {
			return nil, fmt.Errorf("fetch %s: %w", target, err)
		}
		if response.StatusCode == http.StatusNotFound {
			response.Body.Close()
			return nil, ErrNotFound
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			return nil, fmt.Errorf("fetch %s: status %d", target, response.StatusCode)
		}
		return response.Body, nil

	case "file":
		root, err := fileURLPath(parsed)
		if err != nil {
			return nil, err
		}
		full := filepath.Join(root, filepath.FromSlash(path))
		if !isWithinPath(root, full) {
			return nil, fmt.Errorf("repository %q: path %q escapes the root", repo.ID, path)
		}
		file, err := os.Open(full)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil, ErrNotFound
			}
			return nil, fmt.Errorf("open %s: %w", full, err)
		}
		return file, nil

	default:
		return nil, fmt.Errorf("repository %q: unsupported scheme %q", repo.ID, parsed.Scheme)
	}
}

func fileURL(path string) string {
	slash := filepath.ToSlash(path)
	if !strings.HasPrefix(slash, "/") {
		slash = "/" + slash
	}
	return (&url.URL{Scheme: "file", Path: slash}).String()
}

func fileURLPath(parsed *url.URL) (string, error) {
	if parsed.Host != "" && parsed.Host != "localhost" {
		return "", fmt.Errorf("file repository host %q is not supported", parsed.Host)
	}
	path, err := url.PathUnescape(parsed.Path)
	if err != nil {
		return "", fmt.Errorf("decode file url: %w", err)
	}
	path = filepath.FromSlash(path)
	if runtime.GOOS == "windows" {
		trimmed := strings.TrimPrefix(path, string(filepath.Separator))
		if filepath.VolumeName(trimmed) != "" {
			path = trimmed
		}
	}
	return path, nil
}

func isWithinPath(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
