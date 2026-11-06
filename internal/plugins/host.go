package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
	"github.com/gabriel-aplok/kine-build-java/internal/toolchain"
)

const (
	hostHeap        = 256 << 20
	shutdownTimeout = 5 * time.Second
	startTimeout    = 30 * time.Second
)

// HostConfig configures the JVM plugin host.
type HostConfig struct {
	WorkspaceRoot string
	JDK           *toolchain.JDK
	Plugins       []Plugin
	Logger        *slog.Logger
	Stderr        io.Writer
}

// Host starts the JVM host lazily and serves plugin calls.
type Host struct {
	config HostConfig
	logger *slog.Logger

	mu      sync.Mutex
	started bool
	closed  bool
	bridge  *Bridge
	closer  io.Closer
	err     error

	startFn func(context.Context) (*Bridge, io.Closer, error)
}

// NewHost returns a plugin host. The JVM starts on the first call.
func NewHost(config HostConfig) (*Host, error) {
	if config.WorkspaceRoot == "" {
		return nil, fmt.Errorf("plugin host requires a workspace root")
	}
	if config.JDK == nil {
		return nil, fmt.Errorf("plugin host requires a JDK")
	}
	if config.JDK.Major < MinHostJDK {
		return nil, fmt.Errorf("plugin host requires JDK %d or later, found %d", MinHostJDK, config.JDK.Major)
	}
	if config.JDK.Java == "" {
		return nil, fmt.Errorf("plugin host requires a java executable")
	}

	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}
	stderr := config.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}
	config.Logger = logger
	config.Stderr = stderr

	host := &Host{config: config, logger: logger}
	host.startFn = host.start
	return host, nil
}

// Call sends one RPC to the host, starting it on first use.
func (h *Host) Call(ctx context.Context, method string, params, result any) error {
	bridge, err := h.ensureStarted(ctx)
	if err != nil {
		return err
	}
	return bridge.Call(ctx, method, params, result)
}

// Providers returns one core provider per plugin provider id.
func (h *Host) Providers() ([]core.Provider, error) {
	var providers []core.Provider
	for _, plugin := range h.config.Plugins {
		for _, id := range plugin.Manifest.Providers {
			provider, err := NewProvider(plugin, id, h, h.config.WorkspaceRoot)
			if err != nil {
				return nil, err
			}
			providers = append(providers, provider)
		}
	}
	return providers, nil
}

// Close shuts down the host and kills the process if it does not exit.
func (h *Host) Close() error {
	h.mu.Lock()
	started := h.started
	closed := h.closed
	bridge := h.bridge
	closer := h.closer
	startErr := h.err
	h.closed = true
	h.mu.Unlock()

	if closed || !started || startErr != nil || bridge == nil {
		return nil
	}

	var closeErr error
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := bridge.Call(ctx, "shutdown", struct{}{}, nil); err != nil {
		closeErr = fmt.Errorf("shutdown plugin host: %w", err)
	}
	if err := bridge.Close(); err != nil && closeErr == nil {
		closeErr = err
	}
	if closer != nil {
		if err := closer.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	return closeErr
}

func (h *Host) ensureStarted(ctx context.Context) (*Bridge, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return nil, errors.New("plugin host is closed")
	}
	if h.started {
		return h.bridge, h.err
	}
	h.started = true

	bridge, closer, err := h.startFn(ctx)
	if err != nil {
		h.err = fmt.Errorf("start plugin host: %w", err)
		return nil, h.err
	}
	h.bridge = bridge
	h.closer = closer
	return bridge, nil
}

func (h *Host) start(ctx context.Context) (*Bridge, io.Closer, error) {
	build, err := compileHost(ctx, h.config.WorkspaceRoot, h.config.JDK, toolchain.Run)
	if err != nil {
		return nil, nil, err
	}

	tmpDir := filepath.Join(h.config.WorkspaceRoot, ".kine", "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("create plugin host temp dir: %w", err)
	}

	args := []string{
		toolchain.HeapArg("-Xmx", hostHeap),
		"-cp", build.Classes,
		"dev.kine.plugin.host.Main",
	}
	for _, plugin := range h.config.Plugins {
		args = append(args, plugin.Jar)
	}

	process, err := toolchain.Start(toolchain.Command{
		Path: h.config.JDK.Java,
		Args: args,
		Dir:  h.config.WorkspaceRoot,
		Env:  toolchain.BuildEnv(h.config.JDK, tmpDir, nil),
	})
	if err != nil {
		return nil, nil, err
	}

	go h.forwardStderr(process)

	bridge := NewBridge(process.Stdout(), process.Stdin(), h.notify)
	handshakeCtx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()
	if err := bridge.Call(handshakeCtx, "handshake", struct{}{}, nil); err != nil {
		bridge.Close()
		process.Kill()
		return nil, nil, fmt.Errorf("handshake: %w", err)
	}

	h.logger.Debug("plugin host started", "plugins", len(h.config.Plugins))
	return bridge, processCloser{process: process}, nil
}

type processCloser struct {
	process *toolchain.Process
}

func (c processCloser) Close() error {
	return c.process.Kill()
}

func (h *Host) forwardStderr(process *toolchain.Process) {
	_, err := io.Copy(h.config.Stderr, process.Stderr())
	if err != nil && !errors.Is(err, os.ErrClosed) {
		h.logger.Debug("read plugin host stderr", "error", err)
	}
}

func (h *Host) notify(method string, params json.RawMessage) {
	if method != "log" {
		h.logger.Debug("plugin host notification", "method", method)
		return
	}

	var entry struct {
		Level   string `json:"level"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(params, &entry); err != nil {
		h.logger.Warn("decode plugin log", "error", err)
		return
	}
	switch entry.Level {
	case "debug":
		h.logger.Debug(entry.Message)
	case "warn":
		h.logger.Warn(entry.Message)
	case "error":
		h.logger.Error(entry.Message)
	default:
		h.logger.Info(entry.Message)
	}
}
