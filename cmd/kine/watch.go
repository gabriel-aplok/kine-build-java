package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
)

const defaultWatchInterval = 500 * time.Millisecond

var watchSkipDirs = map[string]struct{}{
	"build": {},
	".kine": {},
	".git":  {},
}

func runWatch(ctx context.Context, request taskRequest, stdout, stderr io.Writer, logger *slog.Logger, providers []core.Provider, interval time.Duration) int {
	root, err := os.Getwd()
	if err != nil {
		logger.Error("resolve working directory", "error", err)
		return exitUsage
	}

	request.command = commandBuild
	request.keep, _ = taskSelection(commandBuild)
	request.runArgs = nil

	runTasks(ctx, request, stdout, stderr, logger, providers)
	if ctx.Err() != nil {
		return 0
	}
	signature, err := workspaceSignature(root)
	if err != nil {
		logger.Error("workspace signature", "error", err)
		return exitBuildFailure
	}
	logger.Info("watching", "interval", interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return 0
		case <-ticker.C:
		}

		next, err := workspaceSignature(root)
		if err != nil {
			logger.Warn("workspace signature", "error", err)
			continue
		}
		if next == signature {
			continue
		}
		signature = next

		runTasks(ctx, request, stdout, stderr, logger, providers)
		if ctx.Err() != nil {
			return 0
		}
	}
}

func workspaceSignature(root string) (string, error) {
	hasher := sha256.New()

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path == root {
				return nil
			}
			if _, skip := watchSkipDirs[entry.Name()]; skip {
				return filepath.SkipDir
			}
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		fmt.Fprintf(hasher, "%s\x00%d\x00%d\n", filepath.ToSlash(relative), info.Size(), info.ModTime().UnixNano())
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}
