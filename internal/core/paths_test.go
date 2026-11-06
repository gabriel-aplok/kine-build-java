package core

import (
	"path/filepath"
	"testing"
)

func TestWithin(t *testing.T) {
	root := t.TempDir()

	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "root", path: root, want: true},
		{name: "child", path: filepath.Join(root, "app", "file"), want: true},
		{name: "sibling", path: filepath.Join(root, "..", "other"), want: false},
		{name: "parent", path: filepath.Dir(root), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Within(root, tt.path); got != tt.want {
				t.Errorf("Within(%q, %q) = %t, want %t", root, tt.path, got, tt.want)
			}
		})
	}
}
