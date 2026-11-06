//go:build !windows

package toolchain

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// AvailableMemory returns MemAvailable from /proc/meminfo.
func AvailableMemory() (int64, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, fmt.Errorf("read /proc/meminfo: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "MemAvailable:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			break
		}
		kibibytes, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse MemAvailable: %w", err)
		}
		return kibibytes * 1024, nil
	}
	return 0, fmt.Errorf("MemAvailable not found")
}
