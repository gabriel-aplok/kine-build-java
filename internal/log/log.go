// Package log builds the text logger used by the command line.
package log

import (
	"io"
	"log/slog"
)

const taskKey = "task"

// New returns a text logger writing to w.
// Debug records are enabled when verbose is true.
func New(w io.Writer, verbose bool) *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
}

// WithTask returns a logger that tags every record with the task id.
func WithTask(logger *slog.Logger, id string) *slog.Logger {
	return logger.With(taskKey, id)
}
