package deps

import "testing"

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a    string
		b    string
		want int
	}{
		{a: "1.0", b: "1.0", want: 0},
		{a: "1.0", b: "1.0.0", want: 0},
		{a: "1.0.1", b: "1.0", want: 1},
		{a: "1.0", b: "1.0.1", want: -1},
		{a: "2.0", b: "10.0", want: -1},
		{a: "1.0-alpha", b: "1.0-beta", want: -1},
		{a: "1.0-beta", b: "1.0-rc1", want: -1},
		{a: "1.0-rc1", b: "1.0-rc2", want: -1},
		{a: "1.0-rc2", b: "1.0", want: -1},
		{a: "1.0", b: "1.0-sp", want: -1},
		{a: "1.0-Final", b: "1.0", want: 0},
		{a: "1.0.RELEASE", b: "1.0", want: 0},
		{a: "1.0-unknown", b: "1.0", want: -1},
		{a: "1.0-zzz", b: "1.0-aaa", want: 1},
		{a: "1.0+1", b: "1.0", want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_"+tt.b, func(t *testing.T) {
			if got := CompareVersions(tt.a, tt.b); got != tt.want {
				t.Errorf("CompareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
			if got := CompareVersions(tt.b, tt.a); got != -tt.want {
				t.Errorf("CompareVersions(%q, %q) = %d, want %d", tt.b, tt.a, got, -tt.want)
			}
		})
	}
}
