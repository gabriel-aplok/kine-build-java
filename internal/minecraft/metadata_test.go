package minecraft

import (
	"strings"
	"testing"
)

func TestParseFabricMetadata(t *testing.T) {
	data := `{
  "schemaVersion": 1,
  "id": "example-mod",
  "version": "1.2.3",
  "name": "Example Mod",
  "authors": ["Kine"],
  "entrypoints": {"main": ["com.example.ExampleMod"]},
  "depends": {"minecraft": "1.20.1", "fabricloader": ">=0.15.0"}
}`

	metadata, err := ParseFabricMetadata([]byte(data))
	if err != nil {
		t.Fatalf("ParseFabricMetadata() error = %v", err)
	}
	if metadata.ID != "example-mod" || metadata.Version != "1.2.3" {
		t.Errorf("metadata = %+v", metadata)
	}
	if len(metadata.Entrypoints["main"]) != 1 || metadata.Entrypoints["main"][0] != "com.example.ExampleMod" {
		t.Errorf("Entrypoints = %v", metadata.Entrypoints)
	}
	if metadata.Depends["minecraft"] != "1.20.1" {
		t.Errorf("Depends = %v", metadata.Depends)
	}
}

func TestParseFabricMetadataErrors(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{name: "bad schema", data: `{"schemaVersion": 2, "id": "example", "version": "1"}`, want: "schemaVersion"},
		{name: "bad id", data: `{"id": "Example", "version": "1"}`, want: "id"},
		{name: "empty id", data: `{"version": "1"}`, want: "id"},
		{name: "empty version", data: `{"id": "example", "version": ""}`, want: "version"},
		{name: "space version", data: `{"id": "example", "version": "1 0"}`, want: "version"},
		{name: "empty entrypoint name", data: `{"id": "example", "version": "1", "entrypoints": {"": ["a"]}}`, want: "entrypoint"},
		{name: "empty entrypoint list", data: `{"id": "example", "version": "1", "entrypoints": {"main": []}}`, want: "entrypoint"},
		{name: "empty entrypoint", data: `{"id": "example", "version": "1", "entrypoints": {"main": [""]}}`, want: "entrypoint"},
		{name: "empty depends key", data: `{"id": "example", "version": "1", "depends": {"": "1"}}`, want: "dependency"},
		{name: "empty depends value", data: `{"id": "example", "version": "1", "depends": {"minecraft": " "}}`, want: "dependency"},
		{name: "invalid json", data: `{`, want: "decode"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseFabricMetadata([]byte(tt.data))
			if err == nil {
				t.Fatal("ParseFabricMetadata() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not contain %q", err, tt.want)
			}
		})
	}
}
