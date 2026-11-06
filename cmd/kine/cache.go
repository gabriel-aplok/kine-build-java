package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/gabriel-aplok/kine-build-java/internal/store"
)

const (
	cacheInfoCommand  = "info"
	cachePruneCommand = "prune"
)

func runCacheInfo(stdout io.Writer, logger *slog.Logger, request taskRequest) int {
	ctx := context.Background()
	cacheStore, ok := openCache(logger, request)
	if !ok {
		return exitBuildFailure
	}

	stats, err := cacheStore.Stats(ctx)
	if err != nil {
		logger.Error("cache stats", "error", err)
		return exitBuildFailure
	}
	printStats(stdout, "cache", stats)
	return 0
}

func runCachePrune(stdout io.Writer, logger *slog.Logger, request taskRequest) int {
	ctx := context.Background()
	cacheStore, ok := openCache(logger, request)
	if !ok {
		return exitBuildFailure
	}

	before, err := cacheStore.Stats(ctx)
	if err != nil {
		logger.Error("cache stats", "error", err)
		return exitBuildFailure
	}
	after, err := cacheStore.Prune(ctx)
	if err != nil {
		logger.Error("cache prune", "error", err)
		return exitBuildFailure
	}
	printStats(stdout, "before", before)
	printStats(stdout, "after", after)
	return 0
}

func openCache(logger *slog.Logger, request taskRequest) (*store.Store, bool) {
	cacheStore, err := store.Open(".", request.cacheDir, store.Options{
		MaxSize: request.cacheLimit,
		Logger:  logger,
	})
	if err != nil {
		logger.Error("open cache", "error", err)
		return nil, false
	}
	return cacheStore, true
}

func printStats(w io.Writer, label string, stats store.Stats) {
	fmt.Fprintf(w, "%s: entries=%d blobs=%d size=%d cap=%d\n", label, stats.Entries, stats.Blobs, stats.Size, stats.MaxSize)
}
