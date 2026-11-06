package main

import (
	"fmt"
	"io"
	"log/slog"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/plugins"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

func openPlugins(workspace *core.Workspace, logger *slog.Logger, stderr io.Writer) ([]core.Provider, func() error, error) {
	loaded, err := plugins.Load(workspace.Root)
	if err != nil {
		return nil, nil, err
	}
	if len(loaded) == 0 {
		return nil, func() error { return nil }, nil
	}

	jdk, err := toolchain.Find(toolchain.Requirement{})
	if err != nil {
		return nil, nil, fmt.Errorf("plugin host: %w", err)
	}
	if jdk.Major < plugins.MinHostJDK {
		return nil, nil, fmt.Errorf("plugin host requires JDK %d or later, found %d", plugins.MinHostJDK, jdk.Major)
	}

	host, err := plugins.NewHost(plugins.HostConfig{
		WorkspaceRoot: workspace.Root,
		JDK:           jdk,
		Plugins:       loaded,
		Logger:        logger,
		Stderr:        stderr,
	})
	if err != nil {
		return nil, nil, err
	}
	providers, err := host.Providers()
	if err != nil {
		if closeErr := host.Close(); closeErr != nil {
			logger.Warn("close plugin host", "error", closeErr)
		}
		return nil, nil, err
	}
	return providers, host.Close, nil
}
