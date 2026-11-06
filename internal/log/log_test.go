package log

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewInfoLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf, false)

	logger.Debug("hidden")
	logger.Info("visible")

	out := buf.String()
	if strings.Contains(out, "hidden") {
		t.Error("debug record was written at info level")
	}
	if !strings.Contains(out, "visible") {
		t.Error("info record was not written")
	}
	if strings.Contains(out, "\x1b[") {
		t.Error("output contains ANSI color")
	}
}

func TestNewVerbose(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf, true)

	logger.Debug("visible debug")

	if !strings.Contains(buf.String(), "visible debug") {
		t.Error("debug record was not written with verbose enabled")
	}
}

func TestWithTask(t *testing.T) {
	var buf bytes.Buffer
	logger := WithTask(New(&buf, false), "compile")

	logger.Info("started")

	out := buf.String()
	if !strings.Contains(out, "task=compile") {
		t.Errorf("output %q does not contain task=compile", out)
	}
}
