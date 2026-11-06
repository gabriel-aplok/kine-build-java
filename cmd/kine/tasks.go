package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
)

func listTasks(ctx context.Context, stdout, stderr io.Writer, logger *slog.Logger, providers []core.Provider, request taskRequest) int {
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

	cacheStore := openStore(workspace, request, logger)
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

	sorted := append([]*core.Task(nil), tasks...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].ID < sorted[j].ID
	})
	for _, task := range sorted {
		if request.project != "" && task.Project != request.project {
			continue
		}
		if len(task.Deps) == 0 {
			fmt.Fprintln(stdout, task.ID)
			continue
		}
		fmt.Fprintf(stdout, "%s <- %s\n", task.ID, strings.Join(task.Deps, ", "))
	}
	return 0
}
