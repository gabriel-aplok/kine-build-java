package toolchain

import "strconv"

// DefaultCompilerHeap is the default heap limit per compiler process.
const DefaultCompilerHeap int64 = 512 << 20

// HeapArg formats a JVM heap flag with a byte count.
func HeapArg(prefix string, bytes int64) string {
	if bytes < 0 {
		bytes = 0
	}
	return prefix + strconv.FormatInt(bytes, 10)
}

// FitJobs returns how many jobs fit in the available memory.
func FitJobs(requested int, heapPerJob, available int64) int {
	if requested < 1 {
		requested = 1
	}
	if heapPerJob <= 0 || available <= 0 {
		return requested
	}
	fit := int(available / heapPerJob)
	if fit < 1 {
		return 1
	}
	if fit < requested {
		return fit
	}
	return requested
}
