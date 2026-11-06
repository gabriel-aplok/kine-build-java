package core

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const configFileName = "kine.toml"

type workspaceFile struct {
	Name    string   `toml:"name"`
	Members []string `toml:"members"`
}

type projectFile struct {
	Provider  string         `toml:"provider"`
	Name      string         `toml:"name"`
	DependsOn []string       `toml:"depends_on"`
	Options   map[string]any `toml:"options"`
}

type configFile struct {
	Workspace *workspaceFile `toml:"workspace"`
	Project   *projectFile   `toml:"project"`
}

func decodeFile(path string) (*configFile, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()

	var cfg configFile
	decoder := toml.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return nil, formatDecodeError(path, err)
	}

	if (cfg.Workspace == nil) == (cfg.Project == nil) {
		return nil, fmt.Errorf("%s: exactly one of [workspace] or [project] is required", path)
	}
	if cfg.Workspace != nil && len(cfg.Workspace.Members) == 0 {
		return nil, fmt.Errorf("%s: [workspace] requires at least one member", path)
	}
	if cfg.Project != nil && cfg.Project.Provider == "" {
		return nil, fmt.Errorf("%s: [project] requires provider", path)
	}

	return &cfg, nil
}

func formatDecodeError(path string, err error) error {
	var strictErr *toml.StrictMissingError
	if errors.As(err, &strictErr) {
		var messages []string
		for _, inner := range strictErr.Unwrap() {
			var decodeErr *toml.DecodeError
			if errors.As(inner, &decodeErr) {
				row, column := decodeErr.Position()
				messages = append(messages, fmt.Sprintf("%s:%d:%d: unknown key %v", path, row, column, decodeErr.Key()))
			}
		}
		if len(messages) > 0 {
			return errors.New(strings.Join(messages, "\n"))
		}
	}

	var decodeErr *toml.DecodeError
	if errors.As(err, &decodeErr) {
		row, column := decodeErr.Position()
		return fmt.Errorf("%s:%d:%d: %v: %w", path, row, column, decodeErr.Key(), err)
	}

	return fmt.Errorf("%s: %w", path, err)
}
