package toolchain

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var testEnv = []string{"PATH=/usr/bin:/bin"}

func TestRunSuccess(t *testing.T) {
	result, err := Run(context.Background(), Command{
		Path: "/bin/sh",
		Args: []string{"-c", "printf out; printf err >&2"},
		Env:  testEnv,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", result.ExitCode)
	}
	if string(result.Stdout) != "out" {
		t.Errorf("Stdout = %q, want out", result.Stdout)
	}
	if string(result.Stderr) != "err" {
		t.Errorf("Stderr = %q, want err", result.Stderr)
	}
	if result.Truncated {
		t.Error("Truncated = true, want false")
	}
}

func TestRunStreamsOutput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	result, err := Run(context.Background(), Command{
		Path:   "/bin/sh",
		Args:   []string{"-c", "printf out; printf err >&2"},
		Env:    testEnv,
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if stdout.String() != "out" {
		t.Errorf("streamed stdout = %q, want out", stdout.String())
	}
	if stderr.String() != "err" {
		t.Errorf("streamed stderr = %q, want err", stderr.String())
	}
	if len(result.Stdout) != 0 || len(result.Stderr) != 0 {
		t.Errorf("captured output = %q / %q, want empty", result.Stdout, result.Stderr)
	}
	if result.Truncated {
		t.Error("Truncated = true, want false")
	}
}

func TestStartStreamsOutput(t *testing.T) {
	process, err := Start(Command{
		Path: "/bin/sh",
		Args: []string{"-c", "printf out; printf err >&2"},
		Env:  testEnv,
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	var stdout, stderr strings.Builder
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		data, err := io.ReadAll(process.Stdout())
		if err != nil {
			t.Errorf("read stdout: %v", err)
		}
		stdout.Write(data)
	}()
	go func() {
		defer workers.Done()
		data, err := io.ReadAll(process.Stderr())
		if err != nil {
			t.Errorf("read stderr: %v", err)
		}
		stderr.Write(data)
	}()
	workers.Wait()

	if err := process.Wait(); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if stdout.String() != "out" {
		t.Errorf("stdout = %q, want out", stdout.String())
	}
	if stderr.String() != "err" {
		t.Errorf("stderr = %q, want err", stderr.String())
	}
}

func TestStartStdin(t *testing.T) {
	process, err := Start(Command{
		Path: "/bin/sh",
		Args: []string{"-c", "cat"},
		Env:  testEnv,
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, err := process.Stdin().Write([]byte("hello")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := process.Stdin().Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	data, err := io.ReadAll(process.Stdout())
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("stdout = %q, want hello", data)
	}
	if err := process.Wait(); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
}

func TestStartKill(t *testing.T) {
	process, err := Start(Command{
		Path: "/bin/sh",
		Args: []string{"-c", "sleep 10"},
		Env:  testEnv,
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	started := time.Now()
	if err := process.Kill(); err != nil {
		t.Fatalf("Kill() error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Errorf("Kill() took %s", elapsed)
	}
	if err := process.Wait(); err == nil {
		t.Error("Wait() error = nil after kill, want an exit error")
	}
}

func TestStartRejectsBadPath(t *testing.T) {
	if _, err := Start(Command{Path: "sh"}); err == nil {
		t.Error("Start() error = nil for a relative path")
	}
	if _, err := Start(Command{Path: "/nonexistent/sh"}); err == nil {
		t.Error("Start() error = nil for a missing command")
	}
}

func TestRunExitError(t *testing.T) {
	result, err := Run(context.Background(), Command{
		Path: "/bin/sh",
		Args: []string{"-c", "exit 3"},
		Env:  testEnv,
	})

	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("Run() error = %v, want *ExitError", err)
	}
	if exitErr.Code != 3 {
		t.Errorf("ExitError.Code = %d, want 3", exitErr.Code)
	}
	if result.ExitCode != 3 {
		t.Errorf("Result.ExitCode = %d, want 3", result.ExitCode)
	}
}

func TestRunRejectsRelativePath(t *testing.T) {
	if _, err := Run(context.Background(), Command{Path: "sh"}); err == nil {
		t.Fatal("Run() error = nil, want error")
	}
}

func TestRunDir(t *testing.T) {
	dir := t.TempDir()
	result, err := Run(context.Background(), Command{
		Path: "/bin/sh",
		Args: []string{"-c", "pwd -P"},
		Dir:  dir,
		Env:  testEnv,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	want, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(result.Stdout)); got != want {
		t.Errorf("pwd = %q, want %q", got, want)
	}
}

func TestRunStdin(t *testing.T) {
	result, err := Run(context.Background(), Command{
		Path:  "/bin/sh",
		Args:  []string{"-c", "cat"},
		Env:   testEnv,
		Stdin: strings.NewReader("input"),
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if string(result.Stdout) != "input" {
		t.Errorf("Stdout = %q, want input", result.Stdout)
	}
}

func TestRunTruncatesOutput(t *testing.T) {
	result, err := Run(context.Background(), Command{
		Path:      "/bin/sh",
		Args:      []string{"-c", "yes x | head -c 10000"},
		Env:       testEnv,
		MaxOutput: 100,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !result.Truncated {
		t.Error("Truncated = false, want true")
	}
	if len(result.Stdout) > 100 {
		t.Errorf("len(Stdout) = %d, want <= 100", len(result.Stdout))
	}
}

func TestRunCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := Run(ctx, Command{
		Path: "/bin/sh",
		Args: []string{"-c", "sleep 5"},
		Env:  testEnv,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Run() error = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("Run() took %v, want a fast kill", elapsed)
	}
}

func TestRunTimeout(t *testing.T) {
	_, err := Run(context.Background(), Command{
		Path:    "/bin/sh",
		Args:    []string{"-c", "sleep 5"},
		Env:     testEnv,
		Timeout: 100 * time.Millisecond,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Run() error = %v, want context.DeadlineExceeded", err)
	}
}

func TestRunMissingBinary(t *testing.T) {
	_, err := Run(context.Background(), Command{
		Path: filepath.Join(os.TempDir(), "missing-kine-binary"),
		Env:  testEnv,
	})
	if err == nil {
		t.Fatal("Run() error = nil, want error")
	}
}
