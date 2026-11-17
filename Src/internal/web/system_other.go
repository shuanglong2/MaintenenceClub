//go:build !linux

package web

import (
	"errors"
	"runtime"
)

func diskUsage(string) (uint64, uint64, error) {
	return 0, 0, errors.New("当前系统没有磁盘统计")
}

func processMemory() string {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return formatBytes(stats.Sys)
}
