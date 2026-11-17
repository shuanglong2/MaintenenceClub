package web

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/people"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/buildinfo"
)

const backupStatusPath = "/srv/maintclub/backups/LAST_STATUS"

type systemView struct {
	Version    string
	Commit     string
	Built      string
	DiskFree   string
	DiskTotal  string
	DiskLow    bool
	UploadUsed string
	UploadMax  string
	UploadHigh bool
	DBSize     string
	DBConn     string
	BackupAt   string
	BackupOK   bool
	BackupText string
	Memory     string
	CPU        string
	Mem        string
	DiskUsed   string
	NetRate    string
	NetTotal   string
	Warn       string
	CPUPct     int
	MemPct     int
	DiskPct    int
}

type hostView struct {
	CPU      string
	Mem      string
	NetRate  string
	NetTotal string
	CPUPct   int
	MemPct   int
}

type backupStatus struct {
	At   string
	OK   bool
	Text string
	Note string
}

func (s *server) systemLive(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if !people.Can(user.Session.User, people.ActSystemManage, people.Resource{}) {
		s.notFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	s.renderPart(w, r, "system-live", page{Sys: s.collectStatus(r.Context())})
}

func (s *server) systemStatus(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if !people.Can(user.Session.User, people.ActSystemManage, people.Resource{}) {
		s.notFound(w, r)
		return
	}
	s.render(w, r, http.StatusOK, page{
		Title: "系统状态",
		Body:  "system-body",
		Sys:   s.collectStatus(r.Context()),
	})
}

func (s *server) collectStatus(ctx context.Context) systemView {
	host := sampleHost()
	view := systemView{
		Version: buildinfo.Version, Commit: buildinfo.Commit, Built: buildinfo.Built,
		Memory: processMemory(), CPU: host.CPU, Mem: host.Mem, NetRate: host.NetRate, NetTotal: host.NetTotal,
		CPUPct: host.CPUPct, MemPct: host.MemPct,
	}
	root := "/srv/maintclub"
	if store := s.uploads(); store != nil && store.Root() != "" {
		root = store.Root()
		used := dirSize(store.Root())
		view.UploadUsed = formatBytes(used)
		if quota := store.Quota(); quota > 0 {
			view.UploadMax = formatBytes(uint64(quota))
			view.UploadHigh = uploadHigh(used, uint64(quota))
		} else {
			view.UploadMax = "不限制"
		}
	} else {
		view.UploadUsed = "未知"
		view.UploadMax = "未知"
	}
	if free, total, err := diskUsage(root); err != nil {
		view.DiskFree = "未知"
		view.DiskTotal = "未知"
	} else {
		view.DiskFree = formatBytes(free)
		view.DiskTotal = formatBytes(total)
		view.DiskLow = diskLow(free, total)
		if total > 0 && total >= free {
			view.DiskPct = int((total - free) * 100 / total)
		}
		if total >= free {
			view.DiskUsed = formatBytes(total - free)
		} else {
			view.DiskUsed = "未知"
		}
	}
	if view.DiskUsed == "" {
		view.DiskUsed = "未知"
	}
	view.DBSize, view.DBConn = s.databaseFacts(ctx)
	backup := readBackupStatus(backupStatusPath)
	view.BackupOK = backup.OK
	view.BackupText = backup.Text
	view.BackupAt = backup.At
	if view.BackupAt == "" {
		view.BackupAt = "无"
	}
	var notes []string
	if view.DiskLow {
		notes = append(notes, "磁盘剩余低于 15%")
	}
	if view.UploadHigh {
		notes = append(notes, "上传占用已达到配额的 80%")
	}
	if backup.At != "" && !backup.OK {
		notes = append(notes, "最近一次备份失败")
	}
	view.Warn = strings.Join(notes, ". ")
	return view
}

func (s *server) databaseFacts(ctx context.Context) (string, string) {
	q, ok := s.db.(interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	})
	if !ok {
		return "未知", ""
	}
	var size, conns int64
	err := q.QueryRow(ctx, `
SELECT pg_database_size(current_database()), numbackends
FROM pg_stat_database
WHERE datname = current_database()`).Scan(&size, &conns)
	if err != nil || size < 0 {
		return "未知", ""
	}
	return formatBytes(uint64(size)), fmt.Sprintf("%d 个连接", conns)
}

func cpuPercent(prev, next []uint64) (int, bool) {
	if len(prev) < 4 || len(prev) != len(next) {
		return 0, false
	}
	var prevTotal, nextTotal, prevIdle, nextIdle uint64
	for i := range prev {
		prevTotal += prev[i]
		nextTotal += next[i]
	}
	prevIdle = prev[3]
	nextIdle = next[3]
	if len(prev) > 4 {
		prevIdle += prev[4]
		nextIdle += next[4]
	}
	if nextTotal <= prevTotal || nextIdle < prevIdle {
		return 0, false
	}
	total := nextTotal - prevTotal
	idle := nextIdle - prevIdle
	if idle > total {
		return 0, true
	}
	return int((total - idle) * 100 / total), true
}

func dirSize(root string) uint64 {
	var total uint64
	_ = filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil || entry == nil || !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil || info.Size() < 0 {
			return nil
		}
		total += uint64(info.Size())
		return nil
	})
	return total
}

func readBackupStatus(path string) backupStatus {
	data, err := os.ReadFile(path)
	if err != nil {
		return backupStatus{Text: "还没有备份记录"}
	}
	return parseBackupStatus(string(data))
}

func parseBackupStatus(text string) backupStatus {
	status := backupStatus{Text: "还没有备份记录"}
	seen := false
	for _, line := range strings.Split(text, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		switch key {
		case "result":
			seen = true
			status.OK = value == "ok"
		case "at":
			status.At = value
		case "note":
			status.Note = value
		}
	}
	if !seen {
		return status
	}
	if status.OK {
		status.Text = "成功"
		return status
	}
	status.Text = "失败"
	if status.Note != "" {
		status.Text = "失败: " + status.Note
	}
	return status
}

func diskLow(free, total uint64) bool {
	return total > 0 && free*100/total < 15
}

func uploadHigh(used, quota uint64) bool {
	return quota > 0 && used*10 >= quota*8
}

func formatBytes(n uint64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
