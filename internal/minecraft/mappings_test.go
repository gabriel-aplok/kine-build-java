package minecraft

import (
	"strings"
	"testing"
)

func TestParseMappings(t *testing.T) {
	data := "tiny\t2\t0\tofficial\tintermediary\tnamed\n" +
		"c\tnet/minecraft/class_1\tnet/minecraft/class_1\tnet/minecraft/Example\n" +
		"\tm\tmethod_1\tmethod_1\tdoThing\t()V\n" +
		"\tf\tfield_1\tfield_1\tvalue\tI\n" +
		"\tc\ta comment\n" +
		"\tp\t1\tparam\n"

	mappings, err := ParseMappings([]byte(data))
	if err != nil {
		t.Fatalf("ParseMappings() error = %v", err)
	}
	want := []string{"official", "intermediary", "named"}
	if len(mappings.Namespaces) != len(want) {
		t.Fatalf("Namespaces = %v", mappings.Namespaces)
	}
	for i, namespace := range want {
		if mappings.Namespaces[i] != namespace {
			t.Errorf("Namespaces[%d] = %q, want %q", i, mappings.Namespaces[i], namespace)
		}
	}
}

func TestParseMappingsErrors(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{name: "empty", data: "", want: "empty"},
		{name: "bad magic", data: "not-tiny\t2\t0\ta\tb\tc", want: "line 1"},
		{name: "bad version", data: "tiny\t1\t0\ta\tb\tc", want: "line 1"},
		{name: "too few namespaces", data: "tiny\t2\t0", want: "line 1"},
		{name: "duplicate namespace", data: "tiny\t2\t0\ta\ta\tc", want: "duplicate"},
		{name: "empty namespace", data: "tiny\t2\t0\ta\t\tc", want: "empty"},
		{name: "bad class line", data: "tiny\t2\t0\ta\tb\tc\nc\ta\tb", want: "class line"},
		{name: "bad class kind", data: "tiny\t2\t0\ta\tb\tc\nx\ta\tb\tc", want: "class line"},
		{name: "unknown member", data: "tiny\t2\t0\ta\tb\tc\n\tx\tone", want: "member type"},
		{name: "member without names", data: "tiny\t2\t0\ta\tb\tc\n\tm", want: "no names"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseMappings([]byte(tt.data))
			if err == nil {
				t.Fatal("ParseMappings() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not contain %q", err, tt.want)
			}
		})
	}
}
