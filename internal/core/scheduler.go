package core

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/gabriel-aplok/kine-build-java/internal/log"
)

// Scheduler runs a task graph with a fixed worker pool.
type Scheduler struct {
	Jobs   int
	Cache  Cache
	Logger *slog.Logger
}

type job struct {
	task        *Task
	fingerprint string
}

type result struct {
	id  string
	err error
}

type taskGraph struct {
	tasks      map[string]*Task
	deps       map[string][]string
	dependents map[string][]string
	indegree   map[string]int
	ready      []string
}

// Run executes tasks after their dependencies finish.
func (s *Scheduler) Run(ctx context.Context, root string, tasks []*Task) error {
	if s.Jobs < 1 {
		return fmt.Errorf("jobs must be at least 1")
	}
	if s.Cache == nil {
		return fmt.Errorf("cache is nil")
	}
	logger := s.Logger
	if logger == nil {
		logger = slog.Default()
	}

	graph, err := newTaskGraph(tasks)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	workerCount := s.Jobs
	if workerCount > len(tasks) {
		workerCount = len(tasks)
	}

	ready := make(chan job, len(tasks))
	done := make(chan result, len(tasks))

	var workers sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for item := range ready {
				done <- s.runJob(ctx, root, item, logger)
			}
		}()
	}

	fingerprints := make(map[string]string, len(tasks))
	for _, id := range graph.ready {
		fingerprint, err := Fingerprint(root, graph.tasks[id], fingerprints)
		if err != nil {
			cancel()
			close(ready)
			workers.Wait()
			return fmt.Errorf("task %q: %w", id, err)
		}
		fingerprints[id] = fingerprint
	}
	for _, id := range graph.ready {
		ready <- job{task: graph.tasks[id], fingerprint: fingerprints[id]}
	}

	remaining := len(tasks)
	for remaining > 0 {
		select {
		case <-ctx.Done():
			cancel()
			close(ready)
			workers.Wait()
			return ctx.Err()
		case res := <-done:
			remaining--
			if res.err != nil {
				cancel()
				close(ready)
				workers.Wait()
				return fmt.Errorf("task %q: %w", res.id, res.err)
			}
			for _, dependent := range graph.dependents[res.id] {
				graph.indegree[dependent]--
				if graph.indegree[dependent] != 0 {
					continue
				}
				fingerprint, err := Fingerprint(root, graph.tasks[dependent], fingerprints)
				if err != nil {
					cancel()
					close(ready)
					workers.Wait()
					return fmt.Errorf("task %q: %w", dependent, err)
				}
				fingerprints[dependent] = fingerprint
				ready <- job{task: graph.tasks[dependent], fingerprint: fingerprint}
			}
		}
	}

	close(ready)
	workers.Wait()
	return nil
}

func (s *Scheduler) runJob(ctx context.Context, root string, item job, logger *slog.Logger) result {
	task := item.task
	taskLogger := log.WithTask(logger, task.ID)
	start := time.Now()

	if err := ctx.Err(); err != nil {
		return result{id: task.ID, err: err}
	}

	if !task.AlwaysRun() {
		hit, err := s.Cache.Lookup(ctx, item.fingerprint, task.Outputs)
		if err != nil {
			err = fmt.Errorf("cache lookup: %w", err)
			taskLogger.Error("failed", "duration", time.Since(start), "error", err)
			return result{id: task.ID, err: err}
		}
		if hit {
			exists, err := outputsExist(root, task.Outputs)
			if err != nil {
				taskLogger.Error("failed", "duration", time.Since(start), "error", err)
				return result{id: task.ID, err: err}
			}
			if exists {
				taskLogger.Info("done", "cached", true, "duration", time.Since(start))
				return result{id: task.ID}
			}
		}
	}

	taskLogger.Info("start")
	if task.Run == nil {
		err := fmt.Errorf("task has no action")
		taskLogger.Error("failed", "duration", time.Since(start), "error", err)
		return result{id: task.ID, err: err}
	}
	if err := runTask(ctx, task); err != nil {
		taskLogger.Error("failed", "duration", time.Since(start), "error", err)
		return result{id: task.ID, err: err}
	}
	if err := s.Cache.Store(ctx, item.fingerprint, task.Outputs); err != nil {
		err = fmt.Errorf("cache store: %w", err)
		taskLogger.Error("failed", "duration", time.Since(start), "error", err)
		return result{id: task.ID, err: err}
	}

	taskLogger.Info("done", "cached", false, "duration", time.Since(start))
	return result{id: task.ID}
}

func runTask(ctx context.Context, task *Task) (err error) {
	defer func() {
		if value := recover(); value != nil {
			err = fmt.Errorf("panic: %v\n%s", value, debug.Stack())
		}
	}()
	return task.Run(ctx)
}

func outputsExist(root string, outputs []Output) (bool, error) {
	for _, output := range outputs {
		path := filepath.Join(root, filepath.FromSlash(output.Path))
		if _, err := os.Stat(path); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return false, nil
			}
			return false, fmt.Errorf("output %q: %w", output.Path, err)
		}
	}
	return true, nil
}

func newTaskGraph(tasks []*Task) (*taskGraph, error) {
	graph := &taskGraph{
		tasks:      make(map[string]*Task, len(tasks)),
		deps:       make(map[string][]string, len(tasks)),
		dependents: make(map[string][]string, len(tasks)),
		indegree:   make(map[string]int, len(tasks)),
	}

	order := make([]string, 0, len(tasks))
	for _, task := range tasks {
		if task == nil {
			return nil, fmt.Errorf("task is nil")
		}
		if task.ID == "" {
			return nil, fmt.Errorf("task id is empty")
		}
		if _, ok := graph.tasks[task.ID]; ok {
			return nil, fmt.Errorf("duplicate task id %q", task.ID)
		}
		graph.tasks[task.ID] = task
		order = append(order, task.ID)
	}

	for _, id := range order {
		task := graph.tasks[id]
		seen := make(map[string]struct{}, len(task.Deps))
		unique := make([]string, 0, len(task.Deps))
		for _, dep := range task.Deps {
			if dep == id {
				return nil, fmt.Errorf("task %q depends on itself", id)
			}
			if _, ok := graph.tasks[dep]; !ok {
				return nil, fmt.Errorf("task %q depends on unknown task %q", id, dep)
			}
			if _, ok := seen[dep]; ok {
				continue
			}
			seen[dep] = struct{}{}
			unique = append(unique, dep)
			graph.dependents[dep] = append(graph.dependents[dep], id)
		}
		graph.deps[id] = unique
		graph.indegree[id] = len(unique)
	}

	for _, id := range order {
		if graph.indegree[id] == 0 {
			graph.ready = append(graph.ready, id)
		}
	}

	indegree := make(map[string]int, len(graph.indegree))
	for id, count := range graph.indegree {
		indegree[id] = count
	}
	ready := append([]string(nil), graph.ready...)
	processed := make([]string, 0, len(order))
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		processed = append(processed, id)
		for _, dependent := range graph.dependents[id] {
			indegree[dependent]--
			if indegree[dependent] == 0 {
				ready = append(ready, dependent)
			}
		}
	}
	if len(processed) != len(order) {
		remaining := make(map[string]struct{}, len(order)-len(processed))
		for _, id := range order {
			if indegree[id] > 0 {
				remaining[id] = struct{}{}
			}
		}
		path := findCycle(order, graph.deps, remaining)
		if len(path) == 0 {
			return nil, fmt.Errorf("dependency cycle")
		}
		return nil, fmt.Errorf("dependency cycle: %s", strings.Join(path, " -> "))
	}

	return graph, nil
}
