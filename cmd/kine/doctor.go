package main

import (
	"fmt"
	"io"
	"log/slog"

	"github.com/gabriel-aplok/kine-build-java/internal/android"
	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/deps"
	"github.com/gabriel-aplok/kine-build-java/internal/minecraft"
	"github.com/gabriel-aplok/kine-build-java/internal/plugins"
	"github.com/gabriel-aplok/kine-build-java/internal/store"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

const doctorNameWidth = 13

func runDoctor(stdout io.Writer, logger *slog.Logger, request taskRequest) int {
	failed := false
	report := func(name string, err error) {
		if err != nil {
			failed = true
			fmt.Fprintf(stdout, "%-*s fail: %v\n", doctorNameWidth, name, err)
			return
		}
		fmt.Fprintf(stdout, "%-*s ok\n", doctorNameWidth, name)
	}

	workspace, workspaceErr := core.Load(".")
	report("workspace", workspaceErr)

	root := "."
	if workspaceErr != nil {
		report("graph", fmt.Errorf("workspace did not load"))
	} else {
		root = workspace.Root
		_, graphErr := core.BuildProjectGraph(workspace)
		report("graph", graphErr)
	}

	_, jdkErr := toolchain.Find(toolchain.Requirement{})
	report("jdk", jdkErr)

	if _, err := store.Open(root, request.cacheDir, store.Options{
		MaxSize: request.cacheLimit,
		Logger:  logger,
	}); err != nil {
		report("cache", err)
	} else {
		report("cache", nil)
	}

	_, repoErr := deps.LoadRepositories(root)
	report("repositories", repoErr)

	if _, err := plugins.Load(root); err != nil {
		report("plugins", err)
	} else {
		report("plugins", nil)
	}

	if workspaceErr == nil && usesAndroid(workspace) {
		report("android", checkAndroid())
	}
	if workspaceErr == nil {
		for _, project := range workspace.Projects {
			if project.Provider == "minecraft" {
				report("minecraft", minecraft.CheckProject(project))
				break
			}
		}
	}

	if failed {
		return exitBuildFailure
	}
	return 0
}

func usesAndroid(workspace *core.Workspace) bool {
	for _, project := range workspace.Projects {
		if project.Provider == "android" {
			return true
		}
	}
	return false
}

func checkAndroid() error {
	if _, err := android.FindSDK(android.Requirement{}); err != nil {
		return err
	}
	jdk, err := toolchain.Find(toolchain.Requirement{})
	if err != nil {
		return err
	}
	if jdk.Major < android.MinBuildJDK {
		return fmt.Errorf("JDK %d is older than %d", jdk.Major, android.MinBuildJDK)
	}
	if _, err := toolchain.ToolPath(jdk, "keytool"); err != nil {
		return err
	}
	return nil
}
