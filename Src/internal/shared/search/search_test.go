package search

import (
	"strings"
	"testing"
	"time"
)

func TestTokens(t *testing.T) {
	got := strings.Join(Tokens("数据库 Power，３５０"), " ")
	want := "数据 据库 power 350"
	if got != want {
		t.Fatalf("got %q", got)
	}
	like, ts := Query("焊")
	if like != "焊" || ts != "" {
		t.Fatalf("单字 %q %q", like, ts)
	}
	like, ts = Query("示波器")
	if like != "" || ts != "(示波 <-> 波器)" {
		t.Fatalf("词语 %q %q", like, ts)
	}
	like, ts = Query("电源 故障")
	if like != "" || ts != "电源 & 故障" {
		t.Fatalf("多词 %q %q", like, ts)
	}
	if !strings.Contains(Highlight(`<焊>`, "焊"), "&lt;<mark>焊</mark>&gt;") {
		t.Fatal(Highlight(`<焊>`, "焊"))
	}
}

func TestSearchRecall(t *testing.T) {
	pages := []string{
		"示波器的探头要校准",
		"电源故障时先检查保险丝",
		"焊接温度不要超过 350 度",
		"电烙铁用完放回架子",
		"万用表测量电压前选对档位",
		"库存里的电阻按阻值分盒",
		"活动签到表放在文档里",
		"报销需要发票照片",
		"组长每周检查备份是否成功",
		"电容器极性不能接反",
		"网线钳和水晶头在工具箱",
		"维修记录要写清现象和处理",
		"实验室钥匙由副组长保管",
		"热风枪适合拆小型芯片",
		"松香助焊剂用完要盖紧",
		"电池回收不要丢进普通垃圾桶",
		"投影仪灯泡寿命将尽时会变暗",
		"机房空调滤网每月清洗一次",
		"学生证号可以当作登录名",
		"文档冲突时选择合并后再提交",
	}
	if len(pages) != 20 {
		t.Fatalf("样例 %d", len(pages))
	}
	tokens := make([][]string, len(pages))
	for i, page := range pages {
		tokens[i] = Tokens(page)
	}
	start := time.Now()
	if !containsToken(tokens[0], "示波") || !containsToken(tokens[0], "波器") {
		t.Fatal("示波器")
	}
	if !containsToken(tokens[1], "电源") || !containsToken(tokens[1], "故障") {
		t.Fatal("电源故障")
	}
	bigram := time.Since(start)
	start = time.Now()
	hits := 0
	for _, page := range pages {
		if strings.Contains(page, "焊") {
			hits++
		}
	}
	like := time.Since(start)
	if hits < 2 {
		t.Fatalf("单字召回 %d", hits)
	}
	if bigram > time.Second || like > time.Second {
		t.Fatalf("耗时 %s %s", bigram, like)
	}
}

func containsToken(tokens []string, want string) bool {
	for _, token := range tokens {
		if token == want {
			return true
		}
	}
	return false
}
