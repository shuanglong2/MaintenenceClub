package web

import "testing"

func TestParseBackupStatus(t *testing.T) {
	ok := parseBackupStatus("result=ok\nat=2026-11-17T03:00:00Z\n")
	if !ok.OK || ok.At != "2026-11-17T03:00:00Z" || ok.Text != "成功" {
		t.Fatalf("%+v", ok)
	}
	bad := parseBackupStatus("result=fail\nat=2026-11-17T03:00:00Z\nnote=数据库备份失败\n")
	if bad.OK || bad.Text != "失败: 数据库备份失败" {
		t.Fatalf("%+v", bad)
	}
	empty := parseBackupStatus("")
	if empty.OK || empty.Text != "还没有备份记录" {
		t.Fatalf("%+v", empty)
	}
}

func TestCapacityThresholds(t *testing.T) {
	if !diskLow(14, 100) || diskLow(15, 100) || diskLow(0, 0) {
		t.Fatal("磁盘阈值")
	}
	if !uploadHigh(80, 100) || uploadHigh(79, 100) || uploadHigh(1, 0) {
		t.Fatal("上传阈值")
	}
}

func TestFormatBytes(t *testing.T) {
	if formatBytes(0) != "0 B" || formatBytes(1536) != "1.5 KB" || formatBytes(6<<30) != "6.0 GB" {
		t.Fatalf("%s %s %s", formatBytes(0), formatBytes(1536), formatBytes(6<<30))
	}
}
