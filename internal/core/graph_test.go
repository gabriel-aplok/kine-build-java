package core

import (
	"strings"
	"testing"
)

func TestBuildProjectGraphOrder(t *testing.T) {
	ws := graphWorkspace("a", "b", "c")
	ws.Projects["b"].DependsOn = []string{"a"}
	ws.Projects["c"].DependsOn = []string{"b"}

	graph, err := BuildProjectGraph(ws)
	if err != nil {
		t.Fatalf("BuildProjectGraph() error = %v", err)
	}
	assertOrder(t, graph.Order, "a", "b", "c")
	if len(graph.Deps["c"]) != 1 || graph.Deps["c"][0] != "b" {
		t.Errorf("Deps[c] = %v, want [b]", graph.Deps["c"])
	}
}

func TestBuildProjectGraphIndependentOrder(t *testing.T) {
	ws := graphWorkspace("c", "a", "b")

	graph, err := BuildProjectGraph(ws)
	if err != nil {
		t.Fatalf("BuildProjectGraph() error = %v", err)
	}
	assertOrder(t, graph.Order, "c", "a", "b")
}

func TestBuildProjectGraphDiamond(t *testing.T) {
	ws := graphWorkspace("a", "b", "c", "d")
	ws.Projects["b"].DependsOn = []string{"a"}
	ws.Projects["c"].DependsOn = []string{"a"}
	ws.Projects["d"].DependsOn = []string{"b", "c"}

	graph, err := BuildProjectGraph(ws)
	if err != nil {
		t.Fatalf("BuildProjectGraph() error = %v", err)
	}
	assertOrder(t, graph.Order, "a", "b", "c", "d")
}

func TestBuildProjectGraphDeduplicatesDeps(t *testing.T) {
	ws := graphWorkspace("a", "b")
	ws.Projects["a"].DependsOn = []string{"b", "b"}

	graph, err := BuildProjectGraph(ws)
	if err != nil {
		t.Fatalf("BuildProjectGraph() error = %v", err)
	}
	if len(graph.Deps["a"]) != 1 {
		t.Errorf("Deps[a] = %v, want one entry", graph.Deps["a"])
	}
}

func TestBuildProjectGraphMissingDep(t *testing.T) {
	ws := graphWorkspace("a")
	ws.Projects["a"].DependsOn = []string{"missing"}

	_, err := BuildProjectGraph(ws)
	if err == nil {
		t.Fatal("BuildProjectGraph() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("error %q does not name the missing project", err)
	}
}

func TestBuildProjectGraphSelfDep(t *testing.T) {
	ws := graphWorkspace("a")
	ws.Projects["a"].DependsOn = []string{"a"}

	_, err := BuildProjectGraph(ws)
	if err == nil {
		t.Fatal("BuildProjectGraph() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "itself") {
		t.Errorf("error %q does not mention itself", err)
	}
}

func TestBuildProjectGraphCycle(t *testing.T) {
	ws := graphWorkspace("a", "b", "c")
	ws.Projects["a"].DependsOn = []string{"b"}
	ws.Projects["b"].DependsOn = []string{"c"}
	ws.Projects["c"].DependsOn = []string{"a"}

	_, err := BuildProjectGraph(ws)
	if err == nil {
		t.Fatal("BuildProjectGraph() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error %q does not mention cycle", err)
	}
	if !strings.Contains(err.Error(), "a -> b -> c -> a") {
		t.Errorf("error %q does not name the cycle path", err)
	}
}

func graphWorkspace(ids ...string) *Workspace {
	ws := &Workspace{
		Projects: make(map[string]*Project, len(ids)),
		Order:    ids,
	}
	for _, id := range ids {
		ws.Projects[id] = &Project{ID: id}
	}
	return ws
}

func assertOrder(t *testing.T, got []string, want ...string) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}
