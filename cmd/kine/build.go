package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/gabriel-aplok/kine-build-java/internal/android"
	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/desktop"
	"github.com/gabriel-aplok/kine-build-java/internal/jvm"
	"github.com/gabriel-aplok/kine-build-java/internal/minecraft"
	"github.com/gabriel-aplok/kine-build-java/internal/store"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

const noTestsMessage = "no tests"

type taskRequest struct {
	command    commandKind
	keep       func(local string) bool
	jobs       int
	cacheDir   string
	cacheLimit int64
	update     bool
	offline    bool
	runArgs    []string
	project    string
	device     string
	variant    string
}

func runTasks(ctx context.Context, request taskRequest, stdout, stderr io.Writer, logger *slog.Logger, providers []core.Provider) int {
	available, memoryErr := toolchain.AvailableMemory()
	jobLimit := capJobs(request.jobs, available, memoryErr == nil)

	workspace, err := core.Load(".")
	if err != nil {
		logger.Error("load workspace", "error", err)
		return exitUsage
	}
	if request.project != "" {
		if _, ok := workspace.Projects[request.project]; !ok {
			logger.Error("project not found", "project", request.project)
			return exitUsage
		}
	}
	graph, err := core.BuildProjectGraph(workspace)
	if err != nil {
		logger.Error("validate project graph", "error", err)
		return exitUsage
	}

	lock, err := store.LockWorkspace(workspace.Root)
	if err != nil {
		logger.Error("lock workspace", "error", err)
		return exitUsage
	}
	defer func() {
		if err := lock.Close(); err != nil {
			logger.Warn("release workspace lock", "error", err)
		}
	}()

	var cache core.Cache = core.NoopCache{}
	cacheStore := openStore(workspace, request, logger)
	if cacheStore != nil {
		cache = cacheStore
	}

	pluginProviders, closePlugins, err := openPlugins(workspace, logger, stderr)
	if err != nil {
		logger.Error("load plugins", "error", err)
		return exitUsage
	}
	defer func() {
		if err := closePlugins(); err != nil {
			logger.Warn("close plugin host", "error", err)
		}
	}()

	allProviders := append([]core.Provider(nil), providers...)
	allProviders = append(allProviders, pluginProviders...)
	registry, err := buildRegistry(workspace, cacheStore, logger, request, allProviders)
	if err != nil {
		logger.Error("register providers", "error", err)
		return exitUsage
	}

	tasks, err := core.CollectTasks(ctx, workspace, graph, registry)
	if err != nil {
		logger.Error("collect tasks", "error", err)
		return exitUsage
	}

	selected, err := selectTasks(tasks, request.project, request.keep)
	if err != nil {
		logger.Error("select tasks", "error", err)
		return exitUsage
	}
	if len(selected) == 0 {
		switch request.command {
		case commandRun:
			logger.Error("no run task", "error", fmt.Errorf("no project defines a run task"))
			return exitUsage
		case commandTest:
			fmt.Fprintln(stdout, noTestsMessage)
			return 0
		}
	}

	scheduler := &core.Scheduler{
		Jobs:   jobLimit,
		Cache:  cache,
		Logger: logger,
	}
	if err := scheduler.Run(ctx, workspace.Root, selected); err != nil {
		logger.Error("command failed", "command", commandName(request.command), "error", err)
		return exitBuildFailure
	}

	if cacheStore != nil {
		stats, err := cacheStore.Prune(ctx)
		if err != nil {
			logger.Warn("cache prune", "error", err)
		} else if stats.Size > stats.MaxSize {
			logger.Warn("cache over cap", "size", stats.Size, "max", stats.MaxSize)
		}
	}

	fmt.Fprintf(stdout, "%s complete\n", commandName(request.command))
	return 0
}

func openStore(workspace *core.Workspace, request taskRequest, logger *slog.Logger) *store.Store {
	cacheStore, err := store.Open(workspace.Root, request.cacheDir, store.Options{
		MaxSize: request.cacheLimit,
		Logger:  logger,
	})
	if err != nil {
		logger.Warn("cache disabled", "error", err)
		return nil
	}
	return cacheStore
}

func buildRegistry(workspace *core.Workspace, cacheStore *store.Store, logger *slog.Logger, request taskRequest, providers []core.Provider) (*core.Registry, error) {
	registry := core.NewRegistry()
	jvmProvider := jvm.NewProvider(jvm.Config{
		Workspace: workspace,
		Cache:     cacheStore,
		Logger:    logger,
		Update:    request.update,
		Offline:   request.offline,
		RunArgs:   request.runArgs,
	})
	if err := registry.Register(jvmProvider); err != nil {
		return nil, fmt.Errorf("register jvm provider: %w", err)
	}
	desktopProvider := desktop.NewProvider(desktop.Config{
		Workspace: workspace,
		Cache:     cacheStore,
		Logger:    logger,
		Update:    request.update,
		Offline:   request.offline,
		RunArgs:   request.runArgs,
	})
	if err := registry.Register(desktopProvider); err != nil {
		return nil, fmt.Errorf("register desktop provider: %w", err)
	}
	androidProvider := android.NewProvider(android.Config{
		Workspace: workspace,
		Cache:     cacheStore,
		Logger:    logger,
		Update:    request.update,
		Offline:   request.offline,
		Device:    request.device,
		Variant:   request.variant,
	})
	if err := registry.Register(androidProvider); err != nil {
		return nil, fmt.Errorf("register android provider: %w", err)
	}
	minecraftProvider := minecraft.NewProvider(minecraft.Config{
		Workspace: workspace,
		Logger:    logger,
	})
	if err := registry.Register(minecraftProvider); err != nil {
		return nil, fmt.Errorf("register minecraft provider: %w", err)
	}
	for _, provider := range providers {
		if err := registry.Register(provider); err != nil {
			return nil, fmt.Errorf("register provider %s: %w", provider.Name(), err)
		}
	}
	return registry, nil
}
