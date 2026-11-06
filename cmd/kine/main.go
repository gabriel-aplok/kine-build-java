package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/log"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

const (
	version            = "0.1.0"
	defaultMemoryLimit = "256MiB"
	defaultCacheSize   = "10GiB"
	memoryReserve      = 512 << 20
	exitBuildFailure   = 1
	exitUsage          = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	return runWithProviders(args, stdout, stderr, nil)
}

func runWithProviders(args []string, stdout, stderr io.Writer, providers []core.Provider) int {
	command, rest, err := parseCommand(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitUsage
	}
	return execute(command, rest, stdout, stderr, providers)
}

func execute(command commandKind, args []string, stdout, stderr io.Writer, providers []core.Provider) int {
	flagArgs, programArgs := splitProgramArgs(args)

	cacheCommand := ""
	if command == commandCache {
		if len(flagArgs) == 0 || strings.HasPrefix(flagArgs[0], "-") {
			fmt.Fprintln(stderr, "cache requires info or prune")
			return exitUsage
		}
		cacheCommand = flagArgs[0]
		if cacheCommand != cacheInfoCommand && cacheCommand != cachePruneCommand {
			fmt.Fprintf(stderr, "unknown cache command %q\n", cacheCommand)
			return exitUsage
		}
		flagArgs = flagArgs[1:]
	}

	flags := flag.NewFlagSet("kine", flag.ContinueOnError)
	flags.SetOutput(stderr)

	jobs := flags.Int("jobs", runtime.NumCPU(), "maximum parallel tasks")
	verbose := flags.Bool("verbose", false, "enable debug logs")
	memoryLimit := flags.String("memory-limit", defaultMemoryLimit, "Go soft memory limit, such as 256MiB")
	cacheDir := flags.String("cache-dir", "", "cache directory, defaults to the user cache")
	cacheSize := flags.String("cache-size", defaultCacheSize, "cache size limit, such as 10GiB")
	update := flags.Bool("update", false, "update dependency locks")
	offline := flags.Bool("offline", false, "disable network access")
	interval := flags.Duration("interval", defaultWatchInterval, "watch poll interval")
	importFrom := flags.String("from", "auto", "import source: auto, maven, or gradle")
	importForce := flags.Bool("force", false, "overwrite an existing kine.toml")
	importDryRun := flags.Bool("dry-run", false, "print the config without writing")
	projectFilter := flags.String("project", "", "limit tasks to one project")
	device := flags.String("device", "", "adb device serial for Android runs")
	variant := flags.String("variant", "", "Android build variant, such as freeDebug")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "usage: kine [command] [flags]")
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(stderr, "commands:")
		for _, name := range commandNames() {
			fmt.Fprintf(stderr, "  %s\n", name)
		}
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(stderr, "flags:")
		flags.PrintDefaults()
	}

	if err := flags.Parse(flagArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return exitUsage
	}
	if command == commandHelp {
		flags.Usage()
		return 0
	}
	if command == commandImport {
		if len(programArgs) > 0 {
			fmt.Fprintf(stderr, "unexpected arguments: %s\n", strings.Join(programArgs, " "))
			return exitUsage
		}
		dirs := flags.Args()
		if len(dirs) > 1 {
			fmt.Fprintln(stderr, "import takes at most one directory")
			return exitUsage
		}
		dir := "."
		if len(dirs) == 1 {
			dir = dirs[0]
		}
		return runImport(dir, stdout, stderr, *importFrom, *importForce, *importDryRun)
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "unexpected arguments: %s\n", strings.Join(flags.Args(), " "))
		return exitUsage
	}
	if len(programArgs) > 0 && command != commandRun {
		fmt.Fprintf(stderr, "unexpected arguments: %s\n", strings.Join(programArgs, " "))
		return exitUsage
	}
	if *jobs < 1 {
		fmt.Fprintln(stderr, "jobs must be at least 1")
		return exitUsage
	}
	if command == commandWatch && *interval <= 0 {
		fmt.Fprintln(stderr, "interval must be positive")
		return exitUsage
	}

	limit, err := parseByteSize(*memoryLimit)
	if err != nil {
		fmt.Fprintf(stderr, "invalid memory limit: %v\n", err)
		return exitUsage
	}
	debug.SetMemoryLimit(limit)

	cacheLimit, err := parseByteSize(*cacheSize)
	if err != nil {
		fmt.Fprintf(stderr, "invalid cache size: %v\n", err)
		return exitUsage
	}

	logger := log.New(stderr, *verbose)
	request := taskRequest{
		command:    command,
		jobs:       *jobs,
		cacheDir:   *cacheDir,
		cacheLimit: cacheLimit,
		update:     *update,
		offline:    *offline,
		runArgs:    programArgs,
		project:    *projectFilter,
		device:     *device,
		variant:    *variant,
	}

	switch command {
	case commandVersion:
		fmt.Fprintf(stdout, "kine %s\n", version)
		return 0
	case commandClean:
		return cleanWorkspace(stdout, logger)
	case commandTasks:
		return listTasks(context.Background(), stdout, stderr, logger, providers, request)
	case commandCache:
		if cacheCommand == cacheInfoCommand {
			return runCacheInfo(stdout, logger, request)
		}
		return runCachePrune(stdout, logger, request)
	case commandDoctor:
		return runDoctor(stdout, logger, request)
	case commandWatch:
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		return runWatch(ctx, request, stdout, stderr, logger, providers, *interval)
	}

	keep, ok := taskSelection(command)
	if !ok {
		fmt.Fprintf(stderr, "%s is not implemented yet\n", commandName(command))
		return exitUsage
	}
	request.keep = keep
	return runTasks(context.Background(), request, stdout, stderr, logger, providers)
}

func splitProgramArgs(args []string) ([]string, []string) {
	for index, arg := range args {
		if arg == "--" {
			return args[:index], args[index+1:]
		}
	}
	return args, nil
}

func capJobs(jobs int, available int64, known bool) int {
	if !known {
		return jobs
	}
	usable := available - memoryReserve
	if usable < 1 {
		usable = 1
	}
	return toolchain.FitJobs(jobs, toolchain.DefaultCompilerHeap, usable)
}

func parseByteSize(value string) (int64, error) {
	trimmed := strings.TrimSpace(value)
	multiplier := int64(1)

	switch {
	case strings.HasSuffix(trimmed, "GiB"):
		multiplier = 1 << 30
		trimmed = strings.TrimSuffix(trimmed, "GiB")
	case strings.HasSuffix(trimmed, "MiB"):
		multiplier = 1 << 20
		trimmed = strings.TrimSuffix(trimmed, "MiB")
	case strings.HasSuffix(trimmed, "KiB"):
		multiplier = 1 << 10
		trimmed = strings.TrimSuffix(trimmed, "KiB")
	}

	number := strings.TrimSpace(trimmed)
	n, err := strconv.ParseInt(number, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %q: %w", value, err)
	}
	if n <= 0 {
		return 0, fmt.Errorf("parse %q: must be positive", value)
	}
	if n > (1<<62)/multiplier {
		return 0, fmt.Errorf("parse %q: value too large", value)
	}
	return n * multiplier, nil
}
