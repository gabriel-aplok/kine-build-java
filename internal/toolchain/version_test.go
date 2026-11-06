package toolchain

import "testing"

func TestMajorVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    int
	}{
		{name: "jdk8", version: "1.8.0_402", want: 8},
		{name: "jdk21", version: "21.0.1", want: 21},
		{name: "major only", version: "17", want: 17},
		{name: "early access", version: "21-ea", want: 21},
		{name: "empty", version: "", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := majorVersion(tt.version); got != tt.want {
				t.Errorf("majorVersion(%q) = %d, want %d", tt.version, got, tt.want)
			}
		})
	}
}

func TestCompareJDKVersions(t *testing.T) {
	tests := []struct {
		a    string
		b    string
		want int
	}{
		{a: "21.0.13", b: "21.0.9", want: 1},
		{a: "21.0.1", b: "21.0.1", want: 0},
		{a: "21", b: "21.0.0", want: 0},
		{a: "17.0.2", b: "21.0.1", want: -1},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_"+tt.b, func(t *testing.T) {
			if got := compareJDKVersions(tt.a, tt.b); got != tt.want {
				t.Errorf("compareJDKVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
			if got := compareJDKVersions(tt.b, tt.a); got != -tt.want {
				t.Errorf("reverse compare = %d, want %d", got, -tt.want)
			}
		})
	}
}

func TestSortJDKs(t *testing.T) {
	jdks := []JDK{
		{Home: "/jdk17", Version: "17.0.2", Major: 17},
		{Home: "/jdk21", Version: "21.0.1", Major: 21},
		{Home: "/jdk21b", Version: "21.0.3", Major: 21},
	}

	sortJDKs(jdks)
	want := []string{"/jdk21b", "/jdk21", "/jdk17"}
	for i, home := range want {
		if jdks[i].Home != home {
			t.Errorf("jdks[%d].Home = %q, want %q", i, jdks[i].Home, home)
		}
	}
}
