package core

import (
	"fmt"
	"strings"
)

// ProjectGraph is the validated dependency order of a workspace.
type ProjectGraph struct {
	Order []string
	Deps  map[string][]string
}

// BuildProjectGraph validates project dependencies and returns a deterministic
// topological order.
func BuildProjectGraph(workspace *Workspace) (*ProjectGraph, error) {
	deps := make(map[string][]string, len(workspace.Order))
	indegree := make(map[string]int, len(workspace.Order))
	reverse := make(map[string][]string, len(workspace.Order))

	for _, id := range workspace.Order {
		project, ok := workspace.Projects[id]
		if !ok {
			return nil, fmt.Errorf("project %q: missing from workspace", id)
		}

		seen := make(map[string]struct{}, len(project.DependsOn))
		unique := make([]string, 0, len(project.DependsOn))
		for _, dep := range project.DependsOn {
			if dep == id {
				return nil, fmt.Errorf("project %q depends on itself", id)
			}
			if _, ok := workspace.Projects[dep]; !ok {
				return nil, fmt.Errorf("project %q depends on unknown project %q", id, dep)
			}
			if _, ok := seen[dep]; ok {
				continue
			}
			seen[dep] = struct{}{}
			unique = append(unique, dep)
			reverse[dep] = append(reverse[dep], id)
		}
		deps[id] = unique
		indegree[id] = len(unique)
	}

	ready := make([]string, 0, len(workspace.Order))
	for _, id := range workspace.Order {
		if indegree[id] == 0 {
			ready = append(ready, id)
		}
	}

	order := make([]string, 0, len(workspace.Order))
	for len(ready) > 0 {
		id := ready[0]
		ready = ready[1:]
		order = append(order, id)

		for _, dependent := range reverse[id] {
			indegree[dependent]--
			if indegree[dependent] == 0 {
				ready = append(ready, dependent)
			}
		}
	}

	if len(order) != len(workspace.Order) {
		remaining := make(map[string]struct{}, len(workspace.Order)-len(order))
		for _, id := range workspace.Order {
			if indegree[id] > 0 {
				remaining[id] = struct{}{}
			}
		}

		path := findCycle(workspace.Order, deps, remaining)
		if len(path) == 0 {
			return nil, fmt.Errorf("dependency cycle")
		}
		return nil, fmt.Errorf("dependency cycle: %s", strings.Join(path, " -> "))
	}

	return &ProjectGraph{Order: order, Deps: deps}, nil
}

func findCycle(order []string, deps map[string][]string, remaining map[string]struct{}) []string {
	const (
		unvisited = iota
		visiting
		visited
	)

	state := make(map[string]int, len(remaining))
	stack := make([]string, 0, len(remaining))
	var cycle []string

	var visit func(string) bool
	visit = func(id string) bool {
		state[id] = visiting
		stack = append(stack, id)

		for _, dep := range deps[id] {
			if _, ok := remaining[dep]; !ok {
				continue
			}
			switch state[dep] {
			case visiting:
				start := 0
				for i, item := range stack {
					if item == dep {
						start = i
						break
					}
				}
				cycle = append(cycle, stack[start:]...)
				cycle = append(cycle, dep)
				return true
			case unvisited:
				if visit(dep) {
					return true
				}
			}
		}

		stack = stack[:len(stack)-1]
		state[id] = visited
		return false
	}

	for _, id := range order {
		if _, ok := remaining[id]; !ok {
			continue
		}
		if state[id] == unvisited && visit(id) {
			return cycle
		}
	}
	return nil
}
