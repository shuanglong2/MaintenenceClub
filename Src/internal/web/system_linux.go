//go:build linux

package web

import (
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func diskUsage(path string) (free, total uint64, err error) {
	var st syscall.Statfs_t
	if err = syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	bsize := uint64(st.Bsize)
	return st.Bavail * bsize, st.Blocks * bsize, nil
}

func sampleHost() hostView {
	out := hostView{CPU: "未知", Mem: "未知", NetRate: "未知", NetTotal: "未知"}
	if total, avail, err := readMem(); err == nil && total > 0 && total >= avail {
		out.Mem = formatBytes(total-avail) + " / " + formatBytes(total)
	}
	cpu1, errCPU1 := readCPU()
	rx1, tx1, errNet1 := readNet()
	time.Sleep(200 * time.Millisecond)
	cpu2, errCPU2 := readCPU()
	rx2, tx2, errNet2 := readNet()
	if errCPU1 == nil && errCPU2 == nil {
		if pct, ok := cpuPercent(cpu1, cpu2); ok {
			out.CPU = strconv.Itoa(pct) + "%"
		}
	}
	if errNet1 == nil && errNet2 == nil && rx2 >= rx1 && tx2 >= tx1 {
		out.NetTotal = "接收 " + formatBytes(rx2) + ", 发送 " + formatBytes(tx2)
		out.NetRate = "接收 " + formatBytes(uint64(float64(rx2-rx1)/0.2)) + "/s, 发送 " + formatBytes(uint64(float64(tx2-tx1)/0.2)) + "/s"
	}
	return out
}

func readCPU() ([]uint64, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return nil, err
	}
	line, _, _ := strings.Cut(string(data), "\n")
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return nil, os.ErrInvalid
	}
	out := make([]uint64, 0, len(fields)-1)
	for _, field := range fields[1:] {
		n, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

func readMem() (total, avail uint64, err error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, err
	}
	var totalKB, availKB, freeKB uint64
	var hasAvail bool
	for _, line := range strings.Split(string(data), "\n") {
		key, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		n, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		switch key {
		case "MemTotal":
			totalKB = n
		case "MemAvailable":
			availKB = n
			hasAvail = true
		case "MemFree":
			freeKB = n
		}
	}
	if totalKB == 0 {
		return 0, 0, os.ErrInvalid
	}
	if !hasAvail {
		availKB = freeKB
	}
	return totalKB * 1024, availKB * 1024, nil
}

func readNet() (rx, tx uint64, err error) {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return 0, 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" || name == "lo" {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) < 9 {
			continue
		}
		in, errIn := strconv.ParseUint(fields[0], 10, 64)
		out, errOut := strconv.ParseUint(fields[8], 10, 64)
		if errIn != nil || errOut != nil {
			continue
		}
		rx += in
		tx += out
	}
	return rx, tx, nil
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
