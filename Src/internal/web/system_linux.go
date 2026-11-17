//go:build linux

package web

import (
	"fmt"
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
		out.MemPct = int((total - avail) * 100 / total)
	}
	cpu1, cores1, errCPU1 := readCPUs()
	rx1, tx1, errNet1 := readNet()
	io1 := readIO()
	time.Sleep(200 * time.Millisecond)
	cpu2, cores2, errCPU2 := readCPUs()
	rx2, tx2, errNet2 := readNet()
	io2 := readIO()
	if errCPU1 == nil && errCPU2 == nil {
		if pct, ok := cpuPercent(cpu1, cpu2); ok {
			out.CPU = strconv.Itoa(pct) + "%"
			out.CPUPct = pct
		}
		out.Cores = coreLoads(cores1, cores2)
	}
	if swap, zram := readPressure(); swap != "" {
		out.Swap = swap
		out.Zram = zram
	}
	if errNet1 == nil && errNet2 == nil && rx2 >= rx1 && tx2 >= tx1 {
		out.NetTotal = "接收 " + formatBytes(rx2) + ", 发送 " + formatBytes(tx2)
		downBps := float64(rx2-rx1) / 0.2
		upBps := float64(tx2-tx1) / 0.2
		out.NetRate = "接收 " + formatBytes(uint64(downBps)) + "/s, 发送 " + formatBytes(uint64(upBps)) + "/s"
		downMbps := downBps * 8 / 1e6
		upMbps := upBps * 8 / 1e6
		out.Down = fmt.Sprintf("%.1f Mbps / 100 Mbps", downMbps)
		out.Up = fmt.Sprintf("%.1f Mbps / 50 Mbps", upMbps)
		out.RxPct = percentOf(downMbps, 100)
		out.TxPct = percentOf(upMbps, 50)
	}
	if io1 != nil && io2 != nil && io2[0] >= io1[0] && io2[1] >= io1[1] {
		out.ProcIO = "读 " + formatBytes(uint64(float64(io2[0]-io1[0])/0.2)) + "/s, 写 " + formatBytes(uint64(float64(io2[1]-io1[1])/0.2)) + "/s"
	}
	return out
}

func percentOf(value, cap float64) int {
	if cap <= 0 {
		return 0
	}
	pct := int(value / cap * 100)
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}

func readCPUs() (total []uint64, cores [][]uint64, err error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return nil, nil, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || !strings.HasPrefix(fields[0], "cpu") {
			continue
		}
		nums := make([]uint64, 0, len(fields)-1)
		bad := false
		for _, field := range fields[1:] {
			n, err := strconv.ParseUint(field, 10, 64)
			if err != nil {
				bad = true
				break
			}
			nums = append(nums, n)
		}
		if bad {
			continue
		}
		if fields[0] == "cpu" {
			total = nums
			continue
		}
		cores = append(cores, nums)
	}
	if total == nil {
		return nil, nil, os.ErrInvalid
	}
	return total, cores, nil
}

func coreLoads(prev, next [][]uint64) string {
	n := len(prev)
	if len(next) < n {
		n = len(next)
	}
	parts := make([]string, 0, n)
	for i := 0; i < n; i++ {
		pct, ok := cpuPercent(prev[i], next[i])
		if !ok {
			continue
		}
		parts = append(parts, fmt.Sprintf("核心 %d %d%%", i, pct))
	}
	return strings.Join(parts, " · ")
}

func readPressure() (swap, zram string) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return "", ""
	}
	var total, free uint64
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
		case "SwapTotal":
			total = n * 1024
		case "SwapFree":
			free = n * 1024
		}
	}
	if total > 0 && total >= free {
		swap = formatBytes(total-free) + " / " + formatBytes(total)
	} else {
		swap = "没有 Swap"
	}
	raw, err := os.ReadFile("/proc/swaps")
	if err != nil {
		return swap, "没有 Zram"
	}
	var used, size uint64
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || !strings.Contains(fields[0], "zram") {
			continue
		}
		sz, errSz := strconv.ParseUint(fields[2], 10, 64)
		use, errUse := strconv.ParseUint(fields[3], 10, 64)
		if errSz != nil || errUse != nil {
			continue
		}
		size += sz * 1024
		used += use * 1024
	}
	if size == 0 {
		return swap, "没有 Zram"
	}
	return swap, formatBytes(used) + " / " + formatBytes(size)
}

func readIO() []uint64 {
	data, err := os.ReadFile("/proc/self/io")
	if err != nil {
		return nil
	}
	var readBytes, writeBytes uint64
	for _, line := range strings.Split(string(data), "\n") {
		key, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		n, err := strconv.ParseUint(strings.TrimSpace(rest), 10, 64)
		if err != nil {
			continue
		}
		switch key {
		case "read_bytes":
			readBytes = n
		case "write_bytes":
			writeBytes = n
		}
	}
	return []uint64{readBytes, writeBytes}
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
