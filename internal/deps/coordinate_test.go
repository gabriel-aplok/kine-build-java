package deps

import "testing"

func TestParseCoordinate(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  Coordinate
	}{
		{
			name:  "simple",
			input: "org.jetbrains.kotlin:kotlin-stdlib:2.0.0",
			want:  Coordinate{Group: "org.jetbrains.kotlin", Artifact: "kotlin-stdlib", Version: "2.0.0", Extension: "jar"},
		},
		{
			name:  "classifier",
			input: "com.example:lib:1.0:linux@jar",
			want:  Coordinate{Group: "com.example", Artifact: "lib", Version: "1.0", Classifier: "linux", Extension: "jar"},
		},
		{
			name:  "extension",
			input: "com.example:lib:1.0@aar",
			want:  Coordinate{Group: "com.example", Artifact: "lib", Version: "1.0", Extension: "aar"},
		},
		{
			name:  "spaces",
			input: " com.example:lib:1.0 ",
			want:  Coordinate{Group: "com.example", Artifact: "lib", Version: "1.0", Extension: "jar"},
		},
		{
			name:  "exact range",
			input: "androidx.concurrent:concurrent-futures:[1.1.0]",
			want:  Coordinate{Group: "androidx.concurrent", Artifact: "concurrent-futures", Version: "1.1.0", Extension: "jar"},
		},
		{
			name:  "bounded range",
			input: "com.example:lib:[1.0,2.0)",
			want:  Coordinate{Group: "com.example", Artifact: "lib", Version: "1.0", Extension: "jar"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseCoordinate(tt.input)
			if err != nil {
				t.Fatalf("ParseCoordinate() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("ParseCoordinate() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestCoordinatePaths(t *testing.T) {
	coord := Coordinate{
		Group:      "com.example",
		Artifact:   "lib",
		Version:    "1.0",
		Classifier: "linux",
		Extension:  "jar",
	}

	if got, want := coord.FileName(), "lib-1.0-linux.jar"; got != want {
		t.Errorf("FileName() = %q, want %q", got, want)
	}
	if got, want := coord.Path(), "com/example/lib/1.0/lib-1.0-linux.jar"; got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
	if got, want := coord.POMPath(), "com/example/lib/1.0/lib-1.0.pom"; got != want {
		t.Errorf("POMPath() = %q, want %q", got, want)
	}
	if got, want := coord.String(), "com.example:lib:1.0:linux"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	if got, want := coord.Key(), "com.example:lib:linux:jar"; got != want {
		t.Errorf("Key() = %q, want %q", got, want)
	}
}

func TestParseCoordinateRejects(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "empty", input: ""},
		{name: "too few", input: "a:b"},
		{name: "too many", input: "a:b:c:d:e"},
		{name: "empty artifact", input: "a::1.0"},
		{name: "empty version", input: "a:b:"},
		{name: "empty classifier", input: "a:b:1.0:"},
		{name: "empty extension", input: "a:b:1.0@"},
		{name: "slash", input: "../x:b:1.0"},
		{name: "snapshot", input: "a:b:1.0-SNAPSHOT"},
		{name: "open range", input: "a:b:(,2.0]"},
		{name: "dot dot", input: "a:b:.."},
		{name: "double at", input: "a:b:1.0@jar@zip"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseCoordinate(tt.input); err == nil {
				t.Errorf("ParseCoordinate(%q) error = nil, want error", tt.input)
			}
		})
	}
}
