package toolchain

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

const defaultMaxOutput int64 = 1 << 20

// Command describes one child process.
type Command struct {
	Path      string
	Args      []string
	Dir       string
	Env       []string
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
	Timeout   time.Duration
	MaxOutput int64
	Logger    *slog.Logger
}

type execTarget struct {
	path string
	args []string
	dir  string
}

// Result holds captured process output.
type Result struct {
	ExitCode  int
	Stdout    []byte
	Stderr    []byte
	Truncated bool
}

// ExitError is returned when a process exits with a non-zero code.
type ExitError struct {
	Code int
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("exit status %d", e.Code)
}

// Run executes a child process and returns its output and exit code.
func Run(ctx context.Context, command Command) (Result, error) {
	if command.Path == "" {
		return Result{}, fmt.Errorf("command path is empty")
	}
	if !filepath.IsAbs(command.Path) {
		return Result{}, fmt.Errorf("command path %q must be absolute", command.Path)
	}

	if command.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, command.Timeout)
		defer cancel()
	}

	maxOutput := command.MaxOutput
	if maxOutput <= 0 {
		maxOutput = defaultMaxOutput
	}

	var stdout *limitedBuffer
	var stderr *limitedBuffer

	target := prepareExecutable(command.Path, command.Args, command.Dir)
	cmd := exec.Command(target.path, target.args...)
	cmd.Dir = target.dir
	if command.Env == nil {
		cmd.Env = []string{}
	} else {
		cmd.Env = command.Env
	}
	cmd.Stdin = command.Stdin
	if command.Stdout != nil {
		cmd.Stdout = command.Stdout
	} else {
		stdout = &limitedBuffer{max: maxOutput}
		cmd.Stdout = stdout
	}
	if command.Stderr != nil {
		cmd.Stderr = command.Stderr
	} else {
		stderr = &limitedBuffer{max: maxOutput}
		cmd.Stderr = stderr
	}
	configureProcess(cmd)

	if command.Logger != nil {
		command.Logger.Debug("exec", "path", command.Path, "args", command.Args, "dir", command.Dir)
	}

	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("start %s: %w", command.Path, err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-ctx.Done():
		if cmd.Process != nil {
			_ = stopProcess(cmd.Process)
		}
		<-done
		return buildResult(stdout, stderr), ctx.Err()

	case err := <-done:
		result := buildResult(stdout, stderr)
		if err == nil {
			return result, nil
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			return result, &ExitError{Code: result.ExitCode}
		}
		return result, fmt.Errorf("wait %s: %w", command.Path, err)
	}
}

// Process is a running child process with pipe access.
type Process struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stdout   io.ReadCloser
	stderr   io.ReadCloser
	waitOnce sync.Once
	waitErr  error
}

// Start launches a child process and returns its pipes.
func Start(command Command) (*Process, error) {
	if command.Path == "" {
		return nil, fmt.Errorf("command path is empty")
	}
	if !filepath.IsAbs(command.Path) {
		return nil, fmt.Errorf("command path %q must be absolute", command.Path)
	}

	target := prepareExecutable(command.Path, command.Args, command.Dir)
	cmd := exec.Command(target.path, target.args...)
	cmd.Dir = target.dir
	if command.Env == nil {
		cmd.Env = []string{}
	} else {
		cmd.Env = command.Env
	}
	configureProcess(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe %s: %w", command.Path, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe %s: %w", command.Path, err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe %s: %w", command.Path, err)
	}

	if command.Logger != nil {
		command.Logger.Debug("start", "path", command.Path, "args", command.Args, "dir", command.Dir)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", command.Path, err)
	}
	return &Process{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
	}, nil
}

// Stdin returns the writable stdin pipe.
func (p *Process) Stdin() io.WriteCloser {
	return p.stdin
}

// Stdout returns the readable stdout pipe.
func (p *Process) Stdout() io.ReadCloser {
	return p.stdout
}

// Stderr returns the readable stderr pipe.
func (p *Process) Stderr() io.ReadCloser {
	return p.stderr
}

// Wait waits for the process to exit and returns its error.
func (p *Process) Wait() error {
	p.waitOnce.Do(func() {
		p.waitErr = p.cmd.Wait()
	})
	return p.waitErr
}

// Kill kills the process group and waits for the process.
func (p *Process) Kill() error {
	if p.cmd.Process == nil {
		return nil
	}
	if err := stopProcess(p.cmd.Process); err != nil {
		return err
	}
	p.Wait()
	return nil
}

func buildResult(stdout, stderr *limitedBuffer) Result {
	result := Result{}
	if stdout != nil {
		result.Stdout = stdout.Bytes()
		result.Truncated = stdout.Truncated()
	}
	if stderr != nil {
		result.Stderr = stderr.Bytes()
		result.Truncated = result.Truncated || stderr.Truncated()
	}
	return result
}

type limitedBuffer struct {
	max       int64
	data      []byte
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	remaining := b.max - int64(len(b.data))
	if remaining > 0 {
		if int64(len(p)) > remaining {
			b.data = append(b.data, p[:remaining]...)
			b.truncated = true
		} else {
			b.data = append(b.data, p...)
		}
	} else if len(p) > 0 {
		b.truncated = true
	}
	return len(p), nil
}

func (b *limitedBuffer) Bytes() []byte {
	return b.data
}

func (b *limitedBuffer) Truncated() bool {
	return b.truncated
}
