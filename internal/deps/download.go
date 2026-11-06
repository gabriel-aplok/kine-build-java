package deps

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/gabriel-aplok/kine-build-java/internal/store"
)

type downloadedArtifact struct {
	resolvedArtifact
	Blob       store.Blob
	Repository string
	Path       string
}

func (r *resolver) downloadArtifacts(ctx context.Context, artifacts []resolvedArtifact, workers int) ([]downloadedArtifact, error) {
	if workers < 1 {
		workers = 4
	}
	if workers > 8 {
		workers = 8
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make([]downloadedArtifact, len(artifacts))
	jobs := make(chan int)
	var wait sync.WaitGroup
	var firstErr error
	var once sync.Once

	for i := 0; i < workers; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for index := range jobs {
				if ctx.Err() != nil {
					return
				}
				downloaded, err := r.downloadArtifact(ctx, artifacts[index])
				if err != nil {
					once.Do(func() {
						firstErr = err
						cancel()
					})
					return
				}
				results[index] = downloaded
			}
		}()
	}

	go func() {
		defer close(jobs)
		for index := range artifacts {
			select {
			case jobs <- index:
			case <-ctx.Done():
				return
			}
		}
	}()

	wait.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func (r *resolver) downloadArtifact(ctx context.Context, artifact resolvedArtifact) (downloadedArtifact, error) {
	path := artifact.Coordinate.Path()

	for _, repo := range r.repos {
		reader, err := r.client.open(ctx, repo, path)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return downloadedArtifact{}, fmt.Errorf("download %s from %s: %w", artifact.Coordinate, repo.ID, err)
		}

		tempPath, sha256Value, sha1Value, err := r.streamToTemp(reader)
		reader.Close()
		if err != nil {
			return downloadedArtifact{}, fmt.Errorf("download %s: %w", artifact.Coordinate, err)
		}

		if err := r.verifyChecksum(ctx, repo, path, sha256Value, sha1Value); err != nil {
			os.Remove(tempPath)
			return downloadedArtifact{}, err
		}
		if err := os.Chmod(tempPath, 0o644); err != nil {
			os.Remove(tempPath)
			return downloadedArtifact{}, fmt.Errorf("chmod %s: %w", tempPath, err)
		}

		blob, err := r.cache.PutBlob(tempPath)
		os.Remove(tempPath)
		if err != nil {
			return downloadedArtifact{}, fmt.Errorf("store %s: %w", artifact.Coordinate, err)
		}
		return downloadedArtifact{
			resolvedArtifact: artifact,
			Blob:             blob,
			Repository:       repo.ID,
		}, nil
	}

	return downloadedArtifact{}, fmt.Errorf("artifact %s: %w", artifact.Coordinate, ErrNotFound)
}

func (r *resolver) streamToTemp(reader io.Reader) (string, string, string, error) {
	if err := os.MkdirAll(r.tempDir, 0o755); err != nil {
		return "", "", "", fmt.Errorf("create temp dir: %w", err)
	}
	file, err := os.CreateTemp(r.tempDir, "artifact-*")
	if err != nil {
		return "", "", "", fmt.Errorf("create temp file: %w", err)
	}
	name := file.Name()

	sha256Hasher := sha256.New()
	sha1Hasher := sha1.New()
	if _, err := io.Copy(io.MultiWriter(file, sha256Hasher, sha1Hasher), reader); err != nil {
		file.Close()
		os.Remove(name)
		return "", "", "", fmt.Errorf("write temp file: %w", err)
	}
	if err := file.Close(); err != nil {
		os.Remove(name)
		return "", "", "", fmt.Errorf("close temp file: %w", err)
	}

	return name,
		hex.EncodeToString(sha256Hasher.Sum(nil)),
		hex.EncodeToString(sha1Hasher.Sum(nil)),
		nil
}

func (r *resolver) verifyChecksum(ctx context.Context, repo Repository, path, sha256Value, sha1Value string) error {
	expected, err := r.client.fetchBytes(ctx, repo, path+".sha256")
	if err == nil {
		if !strings.EqualFold(checksumValue(expected), sha256Value) {
			return fmt.Errorf("checksum mismatch for %s", path)
		}
		return nil
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}

	expected, err = r.client.fetchBytes(ctx, repo, path+".sha1")
	if err == nil {
		if !strings.EqualFold(checksumValue(expected), sha1Value) {
			return fmt.Errorf("checksum mismatch for %s", path)
		}
		return nil
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	return nil
}

func (r *resolver) verifyBytesChecksum(ctx context.Context, repo Repository, path string, data []byte) error {
	sha256Sum := sha256.Sum256(data)
	sha1Sum := sha1.Sum(data)
	return r.verifyChecksum(
		ctx,
		repo,
		path,
		hex.EncodeToString(sha256Sum[:]),
		hex.EncodeToString(sha1Sum[:]),
	)
}

func checksumValue(data []byte) string {
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
