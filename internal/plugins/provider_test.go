package plugins

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/gabriel-aplok/kine-build-java/internal/core"
)

func TestProviderTasks(t *testing.T) {
	provider, root := newTestProvider(t, &fakeCaller{})
	projectDir := filepath.Join(root, "app")
	provider.caller = &fakeCaller{handler: func(method string, params, result any) error {
		if method != "tasks" {
			t.Errorf("method = %q, want tasks", method)
			return nil
		}
		request := params.(tasksRequest)
		if request.Provider != "demo" {
			t.Errorf("provider = %q", request.Provider)
		}
		if request.Project.ID != "app" || request.Project.Dir != projectDir {
			t.Errorf("project = %+v", request.Project)
		}
		if request.Project.Options["java"] != 21 {
			t.Errorf("options = %v", request.Project.Options)
		}

		response := result.(*tasksResponse)
		response.Tasks = []wireTask{
			{
				ID:      "generate",
				Deps:    []string{"helper", "lib:jar"},
				Inputs:  []wirePath{{Path: "src/schema", Mode: "tree"}},
				Outputs: []wirePath{{Path: "build/generated", Mode: "tree"}},
				Key:     "schema",
			},
			{ID: "fail"},
		}
		return nil
	}}

	if provider.Name() != "demo" {
		t.Errorf("Name() = %q, want demo", provider.Name())
	}

	project := &core.Project{ID: "app", Dir: projectDir, Options: map[string]any{"java": 21}}
	tasks, err := provider.Tasks(context.Background(), project)
	if err != nil {
		t.Fatalf("Tasks() error = %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("tasks = %d, want 2", len(tasks))
	}

	generate := tasks[0]
	if generate.ID != "generate" {
		t.Errorf("ID = %q", generate.ID)
	}
	if !reflect.DeepEqual(generate.Deps, []string{"app:helper", "lib:jar"}) {
		t.Errorf("Deps = %v", generate.Deps)
	}
	if len(generate.Inputs) != 1 || generate.Inputs[0].Path != "app/src/schema" || generate.Inputs[0].Mode != core.ModeTree {
		t.Errorf("Inputs = %+v", generate.Inputs)
	}
	if len(generate.Outputs) != 1 || generate.Outputs[0].Path != "app/build/generated" || generate.Outputs[0].Mode != core.ModeTree {
		t.Errorf("Outputs = %+v", generate.Outputs)
	}
	if generate.Key != "plugin demo 1.0 schema" {
		t.Errorf("Key = %q", generate.Key)
	}
	if generate.Run == nil {
		t.Error("Run is nil")
	}

	if tasks[1].Key != "plugin demo 1.0 " {
		t.Errorf("empty key = %q", tasks[1].Key)
	}
}

func TestProviderRun(t *testing.T) {
	provider, root := newTestProvider(t, &fakeCaller{})
	var runParams runRequest
	provider.caller = &fakeCaller{handler: func(method string, params, result any) error {
		switch method {
		case "tasks":
			response := result.(*tasksResponse)
			response.Tasks = []wireTask{{ID: "build"}}
		case "run":
			runParams = params.(runRequest)
		}
		return nil
	}}

	projectDir := filepath.Join(root, "app")
	tasks, err := provider.Tasks(context.Background(), &core.Project{ID: "app", Dir: projectDir})
	if err != nil {
		t.Fatal(err)
	}
	if err := tasks[0].Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := runRequest{Provider: "demo", Project: "app", Task: "build"}
	if runParams != want {
		t.Errorf("run params = %+v, want %+v", runParams, want)
	}
}

func TestProviderRunError(t *testing.T) {
	provider, root := newTestProvider(t, &fakeCaller{})
	provider.caller = &fakeCaller{handler: func(method string, params, result any) error {
		if method == "tasks" {
			response := result.(*tasksResponse)
			response.Tasks = []wireTask{{ID: "build"}}
			return nil
		}
		return errors.New("boom")
	}}

	tasks, err := provider.Tasks(context.Background(), &core.Project{ID: "app", Dir: filepath.Join(root, "app")})
	if err != nil {
		t.Fatal(err)
	}
	err = tasks[0].Run(context.Background())
	if err == nil {
		t.Fatal("Run() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "plugin demo") || !strings.Contains(err.Error(), "boom") {
		t.Errorf("error %q does not name the plugin and the failure", err)
	}
}

func TestProviderTasksError(t *testing.T) {
	provider, _ := newTestProvider(t, &fakeCaller{})
	provider.caller = &fakeCaller{handler: func(method string, params, result any) error {
		return errors.New("host down")
	}}

	_, err := provider.Tasks(context.Background(), &core.Project{ID: "app"})
	if err == nil {
		t.Fatal("Tasks() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "plugin demo") || !strings.Contains(err.Error(), "host down") {
		t.Errorf("error %q does not name the plugin and the failure", err)
	}
}

func TestProviderEmptyOptions(t *testing.T) {
	provider, _ := newTestProvider(t, &fakeCaller{})
	provider.caller = &fakeCaller{handler: func(method string, params, result any) error {
		request := params.(tasksRequest)
		if request.Project.Options == nil || len(request.Project.Options) != 0 {
			t.Errorf("options = %v, want an empty map", request.Project.Options)
		}
		return nil
	}}
	if _, err := provider.Tasks(context.Background(), &core.Project{ID: "app"}); err != nil {
		t.Fatal(err)
	}
}

func TestProviderDuplicateTaskID(t *testing.T) {
	provider, _ := newTestProvider(t, &fakeCaller{})
	provider.caller = &fakeCaller{handler: func(method string, params, result any) error {
		response := result.(*tasksResponse)
		response.Tasks = []wireTask{{ID: "same"}, {ID: "same"}}
		return nil
	}}

	_, err := provider.Tasks(context.Background(), &core.Project{ID: "app"})
	if err == nil {
		t.Fatal("Tasks() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "duplicate task id") {
		t.Errorf("error %q does not mention the duplicate", err)
	}
}

func TestProviderValidatesTasks(t *testing.T) {
	tests := []struct {
		name string
		task wireTask
		want string
	}{
		{name: "empty id", task: wireTask{}, want: "task id is empty"},
		{name: "colon id", task: wireTask{ID: "a:b"}, want: "must not contain"},
		{name: "absolute input", task: wireTask{ID: "a", Inputs: []wirePath{{Path: "/etc/passwd", Mode: "file"}}}, want: "relative"},
		{name: "escaping input", task: wireTask{ID: "a", Inputs: []wirePath{{Path: "../../out", Mode: "tree"}}}, want: "escapes"},
		{name: "root input", task: wireTask{ID: "a", Inputs: []wirePath{{Path: ".", Mode: "tree"}}}, want: "project root"},
		{name: "empty path", task: wireTask{ID: "a", Outputs: []wirePath{{Mode: "file"}}}, want: "path is empty"},
		{name: "bad mode", task: wireTask{ID: "a", Outputs: []wirePath{{Path: "out", Mode: "weird"}}}, want: "must be file or tree"},
		{name: "empty dep", task: wireTask{ID: "a", Deps: []string{""}}, want: "dependency is empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, root := newTestProvider(t, &fakeCaller{})
			task := tt.task
			provider.caller = &fakeCaller{handler: func(method string, params, result any) error {
				response := result.(*tasksResponse)
				response.Tasks = []wireTask{task}
				return nil
			}}
			_, err := provider.Tasks(context.Background(), &core.Project{ID: "app", Dir: filepath.Join(root, "app")})
			if err == nil {
				t.Fatal("Tasks() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not contain %q", err, tt.want)
			}
		})
	}
}

func TestNewProviderValidation(t *testing.T) {
	caller := &fakeCaller{}
	root := t.TempDir()

	if _, err := NewProvider(testPlugin("demo"), "demo", nil, root); err == nil {
		t.Error("NewProvider() error = nil for a nil caller")
	}
	if _, err := NewProvider(testPlugin("demo"), "demo", caller, ""); err == nil {
		t.Error("NewProvider() error = nil for an empty workspace root")
	}
	if _, err := NewProvider(testPlugin("other"), "demo", caller, root); err == nil {
		t.Error("NewProvider() error = nil for an unknown provider")
	}

	invalid := testPlugin("demo")
	invalid.Manifest.API = 2
	if _, err := NewProvider(invalid, "demo", caller, root); err == nil {
		t.Error("NewProvider() error = nil for an invalid manifest")
	}
}

func newTestProvider(t *testing.T, caller Caller) (*Provider, string) {
	t.Helper()

	root := t.TempDir()
	provider, err := NewProvider(testPlugin("demo"), "demo", caller, root)
	if err != nil {
		t.Fatal(err)
	}
	return provider, root
}

func testPlugin(providers ...string) Plugin {
	return Plugin{
		Jar: "/plugins/demo.jar",
		Manifest: Manifest{
			API:       APIVersion,
			ID:        "demo",
			Version:   "1.0",
			Main:      "demo.DemoPlugin",
			Providers: providers,
		},
	}
}

type fakeCall struct {
	method string
	params any
}

type fakeCaller struct {
	mu      sync.Mutex
	calls   []fakeCall
	handler func(method string, params, result any) error
}

func (c *fakeCaller) Call(ctx context.Context, method string, params, result any) error {
	c.mu.Lock()
	c.calls = append(c.calls, fakeCall{method: method, params: params})
	handler := c.handler
	c.mu.Unlock()

	if handler != nil {
		return handler(method, params, result)
	}
	return nil
}
