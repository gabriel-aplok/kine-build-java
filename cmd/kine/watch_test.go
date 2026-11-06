package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
)

func TestWorkspaceSignatureChangesOnEdit(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "src", "a.txt"))

	first, err := workspaceSignature(root)
	if err != nil {
		t.Fatalf("workspaceSignature() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "a.txt"), []byte("longer"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := workspaceSignature(root)
	if err != nil {
		t.Fatalf("workspaceSignature() error = %v", err)
	}
	if first == second {
		t.Error("signature did not change after an edit")
	}
}

func TestWorkspaceSignatureSkipsDirs(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "src", "a.txt"))
	for _, dir := range []string{"build", ".kine", ".git"} {
		writeTestFile(t, filepath.Join(root, dir, "x.txt"))
	}

	first, err := workspaceSignature(root)
	if err != nil {
		t.Fatalf("workspaceSignature() error = %v", err)
	}
	for _, dir := range []string{"build", ".kine", ".git"} {
		if err := os.WriteFile(filepath.Join(root, dir, "x.txt"), []byte("longer"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	second, err := workspaceSignature(root)
	if err != nil {
		t.Fatalf("workspaceSignature() error = %v", err)
	}
	if first != second {
		t.Error("signature changed from skipped directories")
	}
}

func TestWatchRebuildsOnChange(t *testing.T) {
	dir := t.TempDir()
	writeRecordingConfig(t, dir)
	writeTestFile(t, filepath.Join(dir, "notes.txt"))
	chdir(t, dir)

	provider := newRecordingProvider()
	logs := &lockedBuffer{}
	logger := slog.New(slog.NewTextHandler(logs, nil))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() {
		done <- runWatch(ctx, watchRequest(t), io.Discard, io.Discard, logger, []core.Provider{provider}, time.Millisecond)
	}()

	waitFor(t, func() bool { return len(provider.ranIDs()) >= 3 }, 15*time.Second)
	waitFor(t, func() bool { return strings.Contains(logs.String(), "watching") }, 15*time.Second)
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("changed content"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return len(provider.ranIDs()) >= 6 }, 15*time.Second)
	cancel()

	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("runWatch() = %d, want 0", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runWatch did not stop")
	}
}

func TestWatchContinuesAfterFailure(t *testing.T) {
	dir := t.TempDir()
	writeRecordingConfig(t, dir)
	writeTestFile(t, filepath.Join(dir, "notes.txt"))
	chdir(t, dir)

	provider := &flakyProvider{}
	logs := &lockedBuffer{}
	logger := slog.New(slog.NewTextHandler(logs, nil))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() {
		done <- runWatch(ctx, watchRequest(t), io.Discard, io.Discard, logger, []core.Provider{provider}, time.Millisecond)
	}()

	waitFor(t, func() bool { return provider.calls.Load() >= 1 }, 5*time.Second)
	waitFor(t, func() bool { return strings.Contains(logs.String(), "watching") }, 5*time.Second)
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("changed content"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return provider.calls.Load() >= 2 }, 5*time.Second)
	cancel()

	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("runWatch() = %d, want 0", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runWatch did not stop")
	}
}

func TestWatchRejectsBadInterval(t *testing.T) {
	chdir(t, t.TempDir())
	var stdout, stderr bytes.Buffer

	code := run([]string{"watch", "--interval", "0s"}, &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("run() = %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "interval") {
		t.Errorf("stderr %q does not mention the interval", stderr.String())
	}
}

func watchRequest(t *testing.T) taskRequest {
	t.Helper()

	return taskRequest{
		command:    commandWatch,
		jobs:       1,
		cacheDir:   t.TempDir(),
		cacheLimit: 1 << 20,
	}
}

func waitFor(t *testing.T, condition func() bool, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("condition not met before timeout")
}

type flakyProvider struct {
	calls atomic.Int32
}

func (p *flakyProvider) Name() string {
	return "recording"
}

func (p *flakyProvider) Tasks(ctx context.Context, project *core.Project) ([]*core.Task, error) {
	return []*core.Task{{
		ID: "build",
		Run: func(ctx context.Context) error {
			if p.calls.Add(1) == 1 {
				return errors.New("first build fails")
			}
			return nil
		},
	}}, nil
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(data)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
