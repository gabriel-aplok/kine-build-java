package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	kinelog "github.com/gabriel-aplok/kine-build-java/internal/log"
)

type stubCache struct {
	hit    bool
	err    error
	stored atomic.Int32
}

func (c *stubCache) Lookup(ctx context.Context, fingerprint string, outputs []Output) (bool, error) {
	if c.err != nil {
		return false, c.err
	}
	return c.hit, nil
}

func (c *stubCache) Store(ctx context.Context, fingerprint string, outputs []Output) error {
	if c.err != nil {
		return c.err
	}
	c.stored.Add(1)
	return nil
}

func TestSchedulerRespectsDependencies(t *testing.T) {
	var mu sync.Mutex
	var events []string
	record := func(event string) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	}

	tasks := []*Task{
		{
			ID:   "b",
			Deps: []string{"a"},
			Run: func(ctx context.Context) error {
				record("b:start")
				return nil
			},
		},
		{
			ID: "a",
			Run: func(ctx context.Context) error {
				record("a:start")
				record("a:end")
				return nil
			},
		},
	}

	if err := runScheduler(t, t.TempDir(), tasks, 2, &stubCache{}, testLogger()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 3 {
		t.Fatalf("events = %v, want 3 entries", events)
	}
	if events[0] != "a:start" || events[1] != "a:end" || events[2] != "b:start" {
		t.Errorf("events = %v, want a before b", events)
	}
}

func TestSchedulerParallelismBound(t *testing.T) {
	const jobs = 2
	const taskCount = 3

	var mu sync.Mutex
	active := 0
	maxActive := 0
	started := make(chan struct{}, taskCount)
	release := make(chan struct{})

	run := func(ctx context.Context) error {
		mu.Lock()
		active++
		if active > maxActive {
			maxActive = active
		}
		mu.Unlock()
		defer func() {
			mu.Lock()
			active--
			mu.Unlock()
		}()

		started <- struct{}{}

		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	tasks := make([]*Task, taskCount)
	for i := range tasks {
		tasks[i] = &Task{ID: "task-" + string(rune('a'+i)), Run: run}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	root := t.TempDir()
	done := make(chan error, 1)
	go func() {
		scheduler := &Scheduler{Jobs: jobs, Cache: &stubCache{}, Logger: testLogger()}
		done <- scheduler.Run(ctx, root, tasks)
	}()

	for i := 0; i < jobs; i++ {
		select {
		case <-started:
		case <-ctx.Done():
			<-done
			t.Fatal("scheduler did not start enough tasks in parallel")
		}
	}
	close(release)

	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if maxActive != jobs {
		t.Errorf("max active tasks = %d, want %d", maxActive, jobs)
	}
}

func TestSchedulerCacheHitSkipsAction(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "out.txt"), "cached")

	called := false
	task := &Task{
		ID:      "app:compile",
		Outputs: []Output{{Path: "out.txt", Mode: ModeFile}},
		Run: func(ctx context.Context) error {
			called = true
			return nil
		},
	}
	cache := &stubCache{hit: true}

	if err := runScheduler(t, root, []*Task{task}, 1, cache, testLogger()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if called {
		t.Error("task action ran on a cache hit")
	}
	if got := cache.stored.Load(); got != 0 {
		t.Errorf("Store() calls = %d, want 0", got)
	}
}

func TestSchedulerMissingOutputRunsTask(t *testing.T) {
	root := t.TempDir()

	called := false
	task := &Task{
		ID:      "app:compile",
		Outputs: []Output{{Path: "missing.txt", Mode: ModeFile}},
		Run: func(ctx context.Context) error {
			called = true
			return nil
		},
	}
	cache := &stubCache{hit: true}

	if err := runScheduler(t, root, []*Task{task}, 1, cache, testLogger()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !called {
		t.Error("task action did not run when the output was missing")
	}
	if got := cache.stored.Load(); got != 1 {
		t.Errorf("Store() calls = %d, want 1", got)
	}
}

func TestSchedulerFailureCancelsAndNamesTask(t *testing.T) {
	bStarted := make(chan struct{})
	var bCanceled atomic.Bool

	tasks := []*Task{
		{
			ID: "a",
			Run: func(ctx context.Context) error {
				select {
				case <-bStarted:
					return errors.New("boom")
				case <-time.After(2 * time.Second):
					return errors.New("task b did not start")
				case <-ctx.Done():
					return ctx.Err()
				}
			},
		},
		{
			ID: "b",
			Run: func(ctx context.Context) error {
				close(bStarted)
				<-ctx.Done()
				bCanceled.Store(true)
				return ctx.Err()
			},
		},
	}

	err := runScheduler(t, t.TempDir(), tasks, 2, &stubCache{}, testLogger())
	if err == nil {
		t.Fatal("Run() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "a") || !strings.Contains(err.Error(), "boom") {
		t.Errorf("error %q does not name task a and the cause", err)
	}
	if !bCanceled.Load() {
		t.Error("task b was not canceled")
	}
}

func TestSchedulerLogsTaskIDAndDuration(t *testing.T) {
	var buf bytes.Buffer
	logger := kinelog.New(&buf, false)
	task := &Task{
		ID:  "app:compile",
		Run: func(ctx context.Context) error { return nil },
	}

	if err := runScheduler(t, t.TempDir(), []*Task{task}, 1, &stubCache{}, logger); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "app:compile") {
		t.Errorf("log output %q does not contain the task id", out)
	}
	if !strings.Contains(out, "duration=") {
		t.Errorf("log output %q does not contain the duration", out)
	}
}

func TestSchedulerRejectsUnknownDependency(t *testing.T) {
	tasks := []*Task{{ID: "a", Deps: []string{"missing"}}}

	err := runScheduler(t, t.TempDir(), tasks, 1, &stubCache{}, testLogger())
	if err == nil {
		t.Fatal("Run() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("error %q does not name the missing dependency", err)
	}
}

func TestSchedulerRejectsCycle(t *testing.T) {
	tasks := []*Task{
		{ID: "a", Deps: []string{"b"}},
		{ID: "b", Deps: []string{"a"}},
	}

	err := runScheduler(t, t.TempDir(), tasks, 1, &stubCache{}, testLogger())
	if err == nil {
		t.Fatal("Run() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error %q does not mention cycle", err)
	}
}

func TestSchedulerRejectsDuplicateIDs(t *testing.T) {
	tasks := []*Task{{ID: "a"}, {ID: "a"}}

	err := runScheduler(t, t.TempDir(), tasks, 1, &stubCache{}, testLogger())
	if err == nil {
		t.Fatal("Run() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error %q does not mention duplicate", err)
	}
}

func TestSchedulerRejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name      string
		scheduler *Scheduler
	}{
		{name: "no jobs", scheduler: &Scheduler{Jobs: 0, Cache: &stubCache{}}},
		{name: "nil cache", scheduler: &Scheduler{Jobs: 1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.scheduler.Run(context.Background(), t.TempDir(), nil)
			if err == nil {
				t.Fatal("Run() error = nil, want error")
			}
		})
	}
}

func TestSchedulerRejectsNilAction(t *testing.T) {
	tasks := []*Task{{ID: "a"}}

	err := runScheduler(t, t.TempDir(), tasks, 1, &stubCache{}, testLogger())
	if err == nil {
		t.Fatal("Run() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "action") {
		t.Errorf("error %q does not mention action", err)
	}
}

func BenchmarkScheduler(b *testing.B) {
	root := b.TempDir()
	tasks := make([]*Task, 100)
	for i := range tasks {
		tasks[i] = &Task{
			ID:  fmt.Sprintf("task-%03d", i),
			Run: func(ctx context.Context) error { return nil },
		}
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scheduler := &Scheduler{Jobs: 4, Cache: &stubCache{}, Logger: testLogger()}
		if err := scheduler.Run(context.Background(), root, tasks); err != nil {
			b.Fatal(err)
		}
	}
}

func runScheduler(t *testing.T, root string, tasks []*Task, jobs int, cache Cache, logger *slog.Logger) error {
	t.Helper()

	scheduler := &Scheduler{Jobs: jobs, Cache: cache, Logger: logger}
	return scheduler.Run(context.Background(), root, tasks)
}

func testLogger() *slog.Logger {
	return kinelog.New(io.Discard, false)
}
