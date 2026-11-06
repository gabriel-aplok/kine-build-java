package main

import (
	"reflect"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
)

func TestLocalTaskID(t *testing.T) {
	tests := []struct {
		id   string
		want string
	}{
		{id: "app:compile-java", want: "compile-java"},
		{id: "compile-java", want: "compile-java"},
		{id: "app:test", want: "test"},
	}
	for _, tt := range tests {
		if got := localTaskID(tt.id); got != tt.want {
			t.Errorf("localTaskID(%q) = %q, want %q", tt.id, got, tt.want)
		}
	}
}

func TestSelectTasksBuild(t *testing.T) {
	tasks := selectTestTasks()

	selected, err := selectTasks(tasks, "", func(local string) bool {
		return local != "run"
	})
	if err != nil {
		t.Fatalf("selectTasks() error = %v", err)
	}
	assertTaskIDs(t, selected, "app:compile", "app:jar", "app:test")
}

func TestSelectTasksRun(t *testing.T) {
	tasks := selectTestTasks()

	selected, err := selectTasks(tasks, "", func(local string) bool {
		return local == "run"
	})
	if err != nil {
		t.Fatalf("selectTasks() error = %v", err)
	}
	assertTaskIDs(t, selected, "app:compile", "app:run")
}

func TestSelectTasksTest(t *testing.T) {
	tasks := selectTestTasks()

	selected, err := selectTasks(tasks, "", func(local string) bool {
		return local == "test"
	})
	if err != nil {
		t.Fatalf("selectTasks() error = %v", err)
	}
	assertTaskIDs(t, selected, "app:compile", "app:test")
}

func TestSelectTasksMissingDependency(t *testing.T) {
	tasks := []*core.Task{{ID: "app:jar", Deps: []string{"app:missing"}}}

	if _, err := selectTasks(tasks, "", func(string) bool { return true }); err == nil {
		t.Fatal("selectTasks() error = nil, want error")
	}
}

func TestSelectTasksDuplicateID(t *testing.T) {
	tasks := []*core.Task{{ID: "app:a"}, {ID: "app:a"}}

	if _, err := selectTasks(tasks, "", func(string) bool { return true }); err == nil {
		t.Fatal("selectTasks() error = nil, want error")
	}
}

func TestParseCommand(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		command commandKind
		rest    []string
		wantErr bool
	}{
		{name: "default", command: commandBuild},
		{name: "build", args: []string{"build", "--verbose"}, command: commandBuild, rest: []string{"--verbose"}},
		{name: "flags only", args: []string{"--verbose"}, command: commandBuild, rest: []string{"--verbose"}},
		{name: "run args", args: []string{"run", "--", "x"}, command: commandRun, rest: []string{"--", "x"}},
		{name: "cache", args: []string{"cache", "info"}, command: commandCache, rest: []string{"info"}},
		{name: "help", args: []string{"help"}, command: commandHelp},
		{name: "unknown", args: []string{"unknown"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command, rest, err := parseCommand(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatal("parseCommand() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseCommand() error = %v", err)
			}
			if command != tt.command {
				t.Errorf("command = %v, want %v", command, tt.command)
			}
			if !reflect.DeepEqual(rest, tt.rest) {
				t.Errorf("rest = %v, want %v", rest, tt.rest)
			}
		})
	}
}

func selectTestTasks() []*core.Task {
	return []*core.Task{
		{ID: "app:compile"},
		{ID: "app:jar", Deps: []string{"app:compile"}},
		{ID: "app:run", Deps: []string{"app:compile"}},
		{ID: "app:test", Deps: []string{"app:compile"}},
	}
}

func assertTaskIDs(t *testing.T, tasks []*core.Task, want ...string) {
	t.Helper()

	if len(tasks) != len(want) {
		t.Fatalf("tasks = %d, want %d", len(tasks), len(want))
	}
	for i, id := range want {
		if tasks[i].ID != id {
			t.Errorf("tasks[%d].ID = %q, want %q", i, tasks[i].ID, id)
		}
	}
}

func TestSelectTasksProject(t *testing.T) {
	tasks := []*core.Task{
		{ID: "app:compile", Project: "app"},
		{ID: "app:run", Project: "app", Deps: []string{"app:compile"}},
		{ID: "other:compile", Project: "other"},
		{ID: "other:run", Project: "other", Deps: []string{"other:compile"}},
	}

	selected, err := selectTasks(tasks, "app", func(local string) bool {
		return local == "run"
	})
	if err != nil {
		t.Fatalf("selectTasks() error = %v", err)
	}
	assertTaskIDs(t, selected, "app:compile", "app:run")
}
