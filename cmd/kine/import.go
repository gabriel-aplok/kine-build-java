package main

import (
	"errors"
	"fmt"
	"io"

	"github.com/gabriel-aplok/kine-build-java/internal/compat"
)

func runImport(dir string, stdout, stderr io.Writer, from string, force, dryRun bool) int {
	source := compat.Source(from)
	switch source {
	case compat.SourceAuto, compat.SourceMaven, compat.SourceGradle:
	default:
		fmt.Fprintf(stderr, "unknown import source %q, use auto, maven, or gradle\n", from)
		return exitUsage
	}

	result, err := compat.Import(compat.Options{
		Dir:    dir,
		From:   source,
		Force:  force,
		DryRun: dryRun,
	})
	if err != nil {
		if errors.Is(err, compat.ErrConfigExists) {
			fmt.Fprintln(stderr, "kine.toml already exists, use --force to overwrite it")
			return exitUsage
		}
		fmt.Fprintf(stderr, "import failed: %v\n", err)
		return exitUsage
	}

	for _, warning := range result.Warnings {
		fmt.Fprintf(stderr, "warning: %s\n", warning)
	}
	if dryRun {
		if _, err := stdout.Write(result.Config); err != nil {
			fmt.Fprintf(stderr, "write config: %v\n", err)
			return exitUsage
		}
	}
	fmt.Fprintln(stderr, result.Summary())
	if result.Written {
		fmt.Fprintln(stdout, "wrote kine.toml")
	}
	return 0
}
