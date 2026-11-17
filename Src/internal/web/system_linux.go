//go:build linux

package web

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

func diskUsage(path string) (free, total uint64, err error) {
	var st syscall.Statfs_t
	if err = syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	bsize := uint64(st.Bsize)
	return st.Bavail * bsize, st.Blocks * bsize, nil
}

func processMemory() string {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return "未知"
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			break
		}
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			break
		}
		return formatBytes(kb * 1024)
	}
	return "未知"
}
