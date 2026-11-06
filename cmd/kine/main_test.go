package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/store"
)

func TestParseByteSize(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int64
		ok   bool
	}{
		{name: "mebibytes", in: "256MiB", want: 256 << 20, ok: true},
		{name: "gibibytes", in: "1GiB", want: 1 << 30, ok: true},
		{name: "kibibytes", in: "512KiB", want: 512 << 10, ok: true},
		{name: "bytes", in: "1024", want: 1024, ok: true},
		{name: "spaces", in: " 64 MiB ", want: 64 << 20, ok: true},
		{name: "invalid", in: "nope", ok: false},
		{name: "zero", in: "0", ok: false},
		{name: "negative", in: "-1MiB", ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseByteSize(tt.in)
			if tt.ok {
				if err != nil {
					t.Fatalf("parseByteSize() error = %v", err)
				}
				if got != tt.want {
					t.Errorf("parseByteSize() = %d, want %d", got, tt.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("parseByteSize() error = nil, want error")
			}
		})
	}
}

func TestRunHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() = %d, want 0", code)
	}
	if !strings.Contains(stderr.String(), "usage: kine") {
		t.Errorf("stderr %q does not contain usage", stderr.String())
	}
}

func TestVersionCommand(t *testing.T) {
	chdir(t, t.TempDir())
	var stdout, stderr bytes.Buffer

	code := run([]string{"version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() = %d, want 0", code)
	}
	want := "kine " + version + "\n"
	if stdout.String() != want {
		t.Errorf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestHelpListsCommands(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run() = %d, want 0", code)
	}
	for _, name := range commandNames() {
		if !strings.Contains(stderr.String(), name) {
			t.Errorf("usage does not list %q", name)
		}
	}
}

func TestRunRejectsBadJobs(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"--jobs", "0"}, &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("run() = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "jobs") {
		t.Errorf("stderr %q does not mention jobs", stderr.String())
	}
}

func TestRunRejectsBadMemoryLimit(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"--memory-limit", "nope"}, &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("run() = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "memory") {
		t.Errorf("stderr %q does not mention memory", stderr.String())
	}
}

func TestRunMissingConfig(t *testing.T) {
	chdir(t, t.TempDir())
	var stdout, stderr bytes.Buffer

	code := run(nil, &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("run() = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "kine.toml") {
		t.Errorf("stderr %q does not name kine.toml", stderr.String())
	}
}

func TestRunInvalidConfig(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, `[workspace]
name = "demo"
members = ["app"]
extra = true
`)
	chdir(t, dir)
	var stdout, stderr bytes.Buffer

	code := run(nil, &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("run() = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "extra") {
		t.Errorf("stderr %q does not name the unknown key", stderr.String())
	}
}

func TestRunUnknownProvider(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, `[project]
name = "app"
provider = "missing"
`)
	chdir(t, dir)
	var stdout, stderr bytes.Buffer

	code := run([]string{"--cache-dir", t.TempDir()}, &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("run() = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "missing") {
		t.Errorf("stderr %q does not name the provider", stderr.String())
	}
}

func TestCapJobs(t *testing.T) {
	tests := []struct {
		name      string
		jobs      int
		available int64
		known     bool
		want      int
	}{
		{name: "fits two", jobs: 8, available: 3 << 29, known: true, want: 2},
		{name: "requested is lower", jobs: 2, available: 3 << 29, known: true, want: 2},
		{name: "unknown memory", jobs: 8, available: 0, known: false, want: 8},
		{name: "below reserve", jobs: 8, available: 400 << 20, known: true, want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := capJobs(tt.jobs, tt.available, tt.known); got != tt.want {
				t.Errorf("capJobs() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestRunAppliesMemoryLimit(t *testing.T) {
	previous := debug.SetMemoryLimit(-1)
	defer debug.SetMemoryLimit(previous)

	chdir(t, t.TempDir())
	var stdout, stderr bytes.Buffer

	code := run([]string{"--memory-limit", "64MiB"}, &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("run() = %d, want %d", code, exitUsage)
	}
	if got := debug.SetMemoryLimit(-1); got != 64<<20 {
		t.Errorf("memory limit = %d, want %d", got, 64<<20)
	}
}

func writeConfig(t *testing.T, dir, content string) {
	t.Helper()

	path := filepath.Join(dir, "kine.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func chdir(t *testing.T, dir string) {
	t.Helper()

	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
}

func TestRunRejectsBadCacheSize(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"--cache-size", "nope"}, &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("run() = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "cache") {
		t.Errorf("stderr %q does not mention cache", stderr.String())
	}
}

func TestRunLockConflict(t *testing.T) {
	dir := t.TempDir()
	writeProjectConfig(t, dir)
	chdir(t, dir)

	lock, err := store.LockWorkspace(dir)
	if err != nil {
		t.Fatalf("LockWorkspace() error = %v", err)
	}
	defer lock.Close()

	var stdout, stderr bytes.Buffer
	code := run([]string{"--cache-dir", t.TempDir()}, &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("run() = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "locked") {
		t.Errorf("stderr %q does not mention the lock", stderr.String())
	}
}

func TestRunCacheFallback(t *testing.T) {
	dir := t.TempDir()
	writeProjectConfig(t, dir)
	chdir(t, dir)

	cachePath := filepath.Join(t.TempDir(), "cache")
	if err := os.WriteFile(cachePath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	provider := &fakeProvider{}

	var stdout, stderr bytes.Buffer
	code := runWithProviders(
		[]string{"--cache-dir", cachePath},
		&stdout,
		&stderr,
		[]core.Provider{provider},
	)
	if code != 0 {
		t.Fatalf("run() = %d, stderr = %s", code, stderr.String())
	}
	if got := provider.runs.Load(); got != 1 {
		t.Errorf("task ran %d times, want 1", got)
	}
	if !strings.Contains(stderr.String(), "cache disabled") {
		t.Errorf("stderr %q does not mention the cache fallback", stderr.String())
	}
}

func TestRunCacheHit(t *testing.T) {
	dir := t.TempDir()
	writeProjectConfig(t, dir)
	chdir(t, dir)

	cacheDir := t.TempDir()
	provider := &fakeProvider{}
	args := []string{"--cache-dir", cacheDir, "--cache-size", "10MiB"}

	var stdout, stderr bytes.Buffer
	code := runWithProviders(args, &stdout, &stderr, []core.Provider{provider})
	if code != 0 {
		t.Fatalf("first run = %d, stderr = %s", code, stderr.String())
	}
	if got := provider.runs.Load(); got != 1 {
		t.Fatalf("task ran %d times, want 1", got)
	}

	output := filepath.Join(dir, "out.txt")
	if err := os.Remove(output); err != nil {
		t.Fatal(err)
	}

	stdout.Reset()
	stderr.Reset()
	code = runWithProviders(args, &stdout, &stderr, []core.Provider{provider})
	if code != 0 {
		t.Fatalf("second run = %d, stderr = %s", code, stderr.String())
	}
	if got := provider.runs.Load(); got != 1 {
		t.Errorf("task ran %d times, want 1", got)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("output was not restored: %v", err)
	}
	if string(data) != "content" {
		t.Errorf("output = %q, want content", data)
	}
}

func TestRunWarnsWhenCacheOverCap(t *testing.T) {
	dir := t.TempDir()
	writeProjectConfig(t, dir)
	chdir(t, dir)

	cacheDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cache, err := store.Open(dir, cacheDir, store.Options{Logger: logger})
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}

	source := filepath.Join(dir, "pinned.txt")
	if err := os.WriteFile(source, []byte("pinned"), 0o600); err != nil {
		t.Fatal(err)
	}
	blob, err := cache.PutBlob(source)
	if err != nil {
		t.Fatalf("PutBlob() error = %v", err)
	}
	if err := cache.MaterializeBlob(blob.Hash, filepath.Join(dir, "pinned-link.txt"), blob.Perm); err != nil {
		t.Fatalf("MaterializeBlob() error = %v", err)
	}

	provider := &fakeProvider{}
	var stdout, stderr bytes.Buffer
	code := runWithProviders(
		[]string{"--cache-dir", cacheDir, "--cache-size", "1"},
		&stdout,
		&stderr,
		[]core.Provider{provider},
	)
	if code != 0 {
		t.Fatalf("run() = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "cache over cap") {
		t.Errorf("stderr %q does not mention the cap", stderr.String())
	}
}

func TestSplitProgramArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		flags   []string
		program []string
	}{
		{name: "none", args: []string{"--verbose"}, flags: []string{"--verbose"}},
		{name: "empty", args: nil},
		{name: "separator", args: []string{"--verbose", "--", "a", "b"}, flags: []string{"--verbose"}, program: []string{"a", "b"}},
		{name: "trailing", args: []string{"a", "--"}, flags: []string{"a"}, program: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flags, program := splitProgramArgs(tt.args)
			if !reflect.DeepEqual(flags, tt.flags) {
				t.Errorf("flags = %v, want %v", flags, tt.flags)
			}
			if !reflect.DeepEqual(program, tt.program) {
				t.Errorf("program = %v, want %v", program, tt.program)
			}
		})
	}
}

func TestBuildCommandExcludesRun(t *testing.T) {
	dir := t.TempDir()
	writeRecordingConfig(t, dir)
	chdir(t, dir)
	provider := newRecordingProvider()

	var stdout, stderr bytes.Buffer
	code := runWithProviders([]string{"--cache-dir", t.TempDir()}, &stdout, &stderr, []core.Provider{provider})
	if code != 0 {
		t.Fatalf("run() = %d, stderr = %s", code, stderr.String())
	}
	assertRuns(t, provider.ranIDs(), "compile", "jar", "test")
	if !strings.Contains(stdout.String(), "build complete") {
		t.Errorf("stdout %q does not contain the completion message", stdout.String())
	}
}

func TestRunCommandSelectsRun(t *testing.T) {
	dir := t.TempDir()
	writeRecordingConfig(t, dir)
	chdir(t, dir)
	provider := newRecordingProvider()

	var stdout, stderr bytes.Buffer
	code := runWithProviders([]string{"run", "--cache-dir", t.TempDir()}, &stdout, &stderr, []core.Provider{provider})
	if code != 0 {
		t.Fatalf("run() = %d, stderr = %s", code, stderr.String())
	}
	assertRuns(t, provider.ranIDs(), "compile", "run")
	if !strings.Contains(stdout.String(), "run complete") {
		t.Errorf("stdout %q does not contain the completion message", stdout.String())
	}
}

func TestRunCommandAcceptsProgramArgs(t *testing.T) {
	dir := t.TempDir()
	writeRecordingConfig(t, dir)
	chdir(t, dir)
	provider := newRecordingProvider()

	var stdout, stderr bytes.Buffer
	code := runWithProviders(
		[]string{"run", "--cache-dir", t.TempDir(), "--", "one", "two"},
		&stdout,
		&stderr,
		[]core.Provider{provider},
	)
	if code != 0 {
		t.Fatalf("run() = %d, stderr = %s", code, stderr.String())
	}
	assertRuns(t, provider.ranIDs(), "compile", "run")
}

func TestBuildCommandRejectsProgramArgs(t *testing.T) {
	dir := t.TempDir()
	writeRecordingConfig(t, dir)
	chdir(t, dir)

	var stdout, stderr bytes.Buffer
	code := runWithProviders(
		[]string{"build", "--", "one"},
		&stdout,
		&stderr,
		[]core.Provider{newRecordingProvider()},
	)
	if code != exitUsage {
		t.Fatalf("run() = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "unexpected arguments") {
		t.Errorf("stderr %q does not mention unexpected arguments", stderr.String())
	}
}

func TestTestCommandSelectsTest(t *testing.T) {
	dir := t.TempDir()
	writeRecordingConfig(t, dir)
	chdir(t, dir)
	provider := newRecordingProvider()

	var stdout, stderr bytes.Buffer
	code := runWithProviders([]string{"test", "--cache-dir", t.TempDir()}, &stdout, &stderr, []core.Provider{provider})
	if code != 0 {
		t.Fatalf("run() = %d, stderr = %s", code, stderr.String())
	}
	assertRuns(t, provider.ranIDs(), "compile", "test")
	if !strings.Contains(stdout.String(), "test complete") {
		t.Errorf("stdout %q does not contain the completion message", stdout.String())
	}
}

func TestTestCommandNoTests(t *testing.T) {
	dir := t.TempDir()
	writeRecordingConfig(t, dir)
	chdir(t, dir)
	provider := newRecordingProvider().without("test")

	var stdout, stderr bytes.Buffer
	code := runWithProviders([]string{"test", "--cache-dir", t.TempDir()}, &stdout, &stderr, []core.Provider{provider})
	if code != 0 {
		t.Fatalf("run() = %d, want 0, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), noTestsMessage) {
		t.Errorf("stdout %q does not contain %q", stdout.String(), noTestsMessage)
	}
	if runs := provider.ranIDs(); len(runs) != 0 {
		t.Errorf("ran %v, want nothing", runs)
	}
}

func TestRunCommandMissingRun(t *testing.T) {
	dir := t.TempDir()
	writeRecordingConfig(t, dir)
	chdir(t, dir)
	provider := newRecordingProvider().without("run")

	var stdout, stderr bytes.Buffer
	code := runWithProviders([]string{"run", "--cache-dir", t.TempDir()}, &stdout, &stderr, []core.Provider{provider})
	if code != exitUsage {
		t.Fatalf("run() = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "no run task") {
		t.Errorf("stderr %q does not mention the missing run task", stderr.String())
	}
}

func TestUnknownCommandExitsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := run([]string{"nope"}, &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("run() = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "unknown command") {
		t.Errorf("stderr %q does not mention the unknown command", stderr.String())
	}
}

func writeRecordingConfig(t *testing.T, dir string) {
	t.Helper()

	writeConfig(t, dir, `[project]
name = "app"
provider = "recording"
`)
}

type recordingProvider struct {
	ids  []string
	deps map[string][]string
	mu   sync.Mutex
	ran  []string
}

func newRecordingProvider() *recordingProvider {
	return &recordingProvider{
		ids: []string{"compile", "jar", "run", "test"},
		deps: map[string][]string{
			"jar":  {"app:compile"},
			"run":  {"app:compile"},
			"test": {"app:compile"},
		},
	}
}

func (p *recordingProvider) without(id string) *recordingProvider {
	filtered := make([]string, 0, len(p.ids))
	for _, existing := range p.ids {
		if existing != id {
			filtered = append(filtered, existing)
		}
	}
	p.ids = filtered
	return p
}

func (p *recordingProvider) Name() string {
	return "recording"
}

func (p *recordingProvider) Tasks(ctx context.Context, project *core.Project) ([]*core.Task, error) {
	tasks := make([]*core.Task, 0, len(p.ids))
	for _, id := range p.ids {
		id := id
		tasks = append(tasks, &core.Task{
			ID:   id,
			Deps: p.deps[id],
			Run: func(ctx context.Context) error {
				p.mu.Lock()
				defer p.mu.Unlock()
				p.ran = append(p.ran, id)
				return nil
			},
		})
	}
	return tasks, nil
}

func (p *recordingProvider) ranIDs() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.ran...)
}

func assertRuns(t *testing.T, got []string, want ...string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("ran %v, want %v", got, want)
	}
	counts := make(map[string]int, len(want))
	for _, id := range want {
		counts[id]++
	}
	for _, id := range got {
		counts[id]--
	}
	for _, count := range counts {
		if count != 0 {
			t.Errorf("ran %v, want %v", got, want)
			return
		}
	}
}

type fakeProvider struct {
	runs atomic.Int32
}

func (p *fakeProvider) Name() string {
	return "fake"
}

func (p *fakeProvider) Tasks(ctx context.Context, project *core.Project) ([]*core.Task, error) {
	return []*core.Task{{
		ID:      "build",
		Outputs: []core.Output{{Path: "out.txt", Mode: core.ModeFile}},
		Run: func(ctx context.Context) error {
			p.runs.Add(1)
			return os.WriteFile(filepath.Join(project.Dir, "out.txt"), []byte("content"), 0o600)
		},
	}}, nil
}

func writeProjectConfig(t *testing.T, dir string) {
	t.Helper()

	writeConfig(t, dir, `[project]
name = "app"
provider = "fake"
`)
}
