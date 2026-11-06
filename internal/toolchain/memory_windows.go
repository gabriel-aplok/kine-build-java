//go:build windows

package toolchain

import (
	"fmt"
	"math"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	globalMemoryStatusEx = kernel32.NewProc("GlobalMemoryStatusEx")
)

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

// AvailableMemory returns the physical memory currently available.
func AvailableMemory() (int64, error) {
	var status memoryStatusEx
	status.Length = uint32(unsafe.Sizeof(status))
	result, _, err := globalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&status)))
	runtime.KeepAlive(&status)
	if result == 0 {
		return 0, fmt.Errorf("GlobalMemoryStatusEx: %w", err)
	}
	if status.AvailPhys > math.MaxInt64 {
		return math.MaxInt64, nil
	}
	return int64(status.AvailPhys), nil
}
