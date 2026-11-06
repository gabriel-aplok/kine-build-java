package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/store"
)

var projectCacheDirs = []string{"deps", "tools", "tmp"}
var workspaceCacheDirs = []string{"tools", "tmp"}

func cleanWorkspace(stdout io.Writer, logger *slog.Logger) int {
	workspace, err := core.Load(".")
	if err != nil {
		logger.Error("load workspace", "error", err)
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

	ids := make([]string, 0, len(workspace.Projects))
	for id := range workspace.Projects {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		project := workspace.Projects[id]
		build := filepath.Join(project.Dir, "build")
		if err := os.RemoveAll(build); err != nil {
			logger.Error("remove build", "project", id, "error", err)
			return exitBuildFailure
		}
		logger.Info("removed build", "project", id, "path", build)

		for _, name := range projectCacheDirs {
			if err := os.RemoveAll(filepath.Join(project.Dir, ".kine", name)); err != nil {
				logger.Error("remove project cache", "project", id, "error", err)
				return exitBuildFailure
			}
		}
	}

	for _, name := range workspaceCacheDirs {
		if err := os.RemoveAll(filepath.Join(workspace.Root, ".kine", name)); err != nil {
			logger.Error("remove workspace cache", "error", err)
			return exitBuildFailure
		}
	}

	fmt.Fprintln(stdout, "clean complete")
	return 0
}
