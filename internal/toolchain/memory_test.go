package toolchain

import (
	"os"
	"testing"
)

func TestHeapArg(t *testing.T) {
	if got, want := HeapArg("-Xmx", 512<<20), "-Xmx536870912"; got != want {
		t.Errorf("HeapArg() = %q, want %q", got, want)
	}
	if got, want := HeapArg("-J-Xmx", 0), "-J-Xmx0"; got != want {
		t.Errorf("HeapArg() = %q, want %q", got, want)
	}
}

func TestFitJobs(t *testing.T) {
	tests := []struct {
		name      string
		requested int
		heap      int64
		available int64
		want      int
	}{
		{name: "fits", requested: 8, heap: 512 << 20, available: 1 << 30, want: 2},
		{name: "unknown memory", requested: 8, heap: 512 << 20, available: 0, want: 8},
		{name: "no request", requested: 0, heap: 512 << 20, available: 1 << 30, want: 1},
		{name: "unknown heap", requested: 8, heap: 0, available: 1 << 30, want: 8},
		{name: "below one job", requested: 8, heap: 1 << 30, available: 512 << 20, want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FitJobs(tt.requested, tt.heap, tt.available); got != tt.want {
				t.Errorf("FitJobs() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestDefaultCompilerHeap(t *testing.T) {
	if DefaultCompilerHeap != 512<<20 {
		t.Errorf("DefaultCompilerHeap = %d, want %d", DefaultCompilerHeap, int64(512<<20))
	}
}

func TestAvailableMemory(t *testing.T) {
	if _, err := os.Stat("/proc/meminfo"); err != nil {
		t.Skipf("/proc/meminfo not available: %v", err)
	}

	available, err := AvailableMemory()
	if err != nil {
		t.Fatalf("AvailableMemory() error = %v", err)
	}
	if available <= 0 {
		t.Errorf("AvailableMemory() = %d, want positive", available)
	}
}
