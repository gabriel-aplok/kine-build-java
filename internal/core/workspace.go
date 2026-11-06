package core

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Workspace is the loaded project tree rooted at a kine.toml file.
type Workspace struct {
	Root     string
	Name     string
	Projects map[string]*Project
	Order    []string
}

// Project is one build target declared by a kine.toml file.
type Project struct {
	ID        string
	Dir       string
	Provider  string
	DependsOn []string
	Options   map[string]any
}

var (
	projectIDPattern   = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	ErrProjectNotFound = errors.New("project not found")
)

// Load reads root/kine.toml and every member file it references.
func Load(root string) (*Workspace, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root %s: %w", root, err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root %s: %w", absRoot, err)
	}

	configPath := filepath.Join(resolvedRoot, configFileName)
	cfg, err := decodeFile(configPath)
	if err != nil {
		return nil, err
	}

	switch {
	case cfg.Project != nil:
		return loadSingleProject(resolvedRoot, configPath, cfg.Project)
	case cfg.Workspace != nil:
		return loadWorkspace(resolvedRoot, configPath, cfg.Workspace)
	default:
		return nil, fmt.Errorf("%s: missing [workspace] or [project]", configPath)
	}
}

// Project returns the project with the given id.
func (w *Workspace) Project(id string) (*Project, error) {
	project, ok := w.Projects[id]
	if !ok {
		return nil, fmt.Errorf("project %q: %w", id, ErrProjectNotFound)
	}
	return project, nil
}

func loadSingleProject(root, configPath string, file *projectFile) (*Workspace, error) {
	id := file.Name
	if id == "" {
		id = filepath.Base(root)
	}
	if err := validateProjectID(id); err != nil {
		return nil, fmt.Errorf("%s: %w", configPath, err)
	}

	project := &Project{
		ID:        id,
		Dir:       root,
		Provider:  file.Provider,
		DependsOn: file.DependsOn,
		Options:   file.Options,
	}
	return &Workspace{
		Root:     root,
		Name:     id,
		Projects: map[string]*Project{id: project},
		Order:    []string{id},
	}, nil
}

func loadWorkspace(root, configPath string, file *workspaceFile) (*Workspace, error) {
	workspace := &Workspace{
		Root:     root,
		Name:     file.Name,
		Projects: make(map[string]*Project, len(file.Members)),
		Order:    make([]string, 0, len(file.Members)),
	}
	seenDirs := make(map[string]struct{}, len(file.Members))

	for _, member := range file.Members {
		dir, err := resolveMember(root, configPath, member)
		if err != nil {
			return nil, err
		}
		if _, ok := seenDirs[dir]; ok {
			return nil, fmt.Errorf("%s: duplicate member %q", configPath, member)
		}
		seenDirs[dir] = struct{}{}

		memberConfigPath := filepath.Join(dir, configFileName)
		cfg, err := decodeFile(memberConfigPath)
		if err != nil {
			return nil, err
		}
		if cfg.Project == nil {
			return nil, fmt.Errorf("%s: member must contain [project]", memberConfigPath)
		}

		id := cfg.Project.Name
		if id == "" {
			id = filepath.Base(dir)
		}
		if err := validateProjectID(id); err != nil {
			return nil, fmt.Errorf("%s: %w", memberConfigPath, err)
		}
		if _, ok := workspace.Projects[id]; ok {
			return nil, fmt.Errorf("%s: duplicate project id %q", memberConfigPath, id)
		}

		workspace.Projects[id] = &Project{
			ID:        id,
			Dir:       dir,
			Provider:  cfg.Project.Provider,
			DependsOn: cfg.Project.DependsOn,
			Options:   cfg.Project.Options,
		}
		workspace.Order = append(workspace.Order, id)
	}

	return workspace, nil
}

func resolveMember(root, configPath, member string) (string, error) {
	if member == "" {
		return "", fmt.Errorf("%s: member must not be empty", configPath)
	}
	if filepath.IsAbs(member) {
		return "", fmt.Errorf("%s: member %q must be relative", configPath, member)
	}

	clean := filepath.Clean(member)
	if clean == "." {
		return "", fmt.Errorf("%s: member %q is the workspace root", configPath, member)
	}
	joined := filepath.Join(root, clean)
	if !isWithin(root, joined) {
		return "", fmt.Errorf("%s: member %q escapes the workspace root", configPath, member)
	}

	resolved, err := filepath.EvalSymlinks(joined)
	if err != nil {
		return "", fmt.Errorf("%s: resolve member %q: %w", configPath, member, err)
	}
	if resolved == root {
		return "", fmt.Errorf("%s: member %q resolves to the workspace root", configPath, member)
	}
	if !isWithin(root, resolved) {
		return "", fmt.Errorf("%s: member %q resolves outside the workspace root", configPath, member)
	}
	return resolved, nil
}

func isWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func validateProjectID(id string) error {
	if !projectIDPattern.MatchString(id) {
		return fmt.Errorf("invalid project id %q", id)
	}
	return nil
}
