package events

import (
	"testing"
	"time"
)

func TestParseRangeAllDayMonthEnd(t *testing.T) {
	start, end, err := ParseRange(true, "2026-01-31", "", "2026-02-01", "")
	if err != nil {
		t.Fatal(err)
	}
	wantStart := time.Date(2026, 1, 31, 0, 0, 0, 0, CST)
	wantEnd := time.Date(2026, 2, 2, 0, 0, 0, 0, CST)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Fatalf("%s %s", start, end)
	}
	label := WhenLabel(Event{Starts: start, Ends: end, AllDay: true})
	if label != "2026-01-31 至 2026-02-01 全天" {
		t.Fatal(label)
	}
}

func TestParseRangeCrossMidnight(t *testing.T) {
	start, end, err := ParseRange(false, "2026-01-31", "23:30", "2026-02-01", "00:30")
	if err != nil {
		t.Fatal(err)
	}
	if start.UTC().Format(time.RFC3339) != "2026-01-31T15:30:00Z" {
		t.Fatal(start.UTC())
	}
	if !end.Equal(time.Date(2026, 2, 1, 0, 30, 0, 0, CST)) {
		t.Fatal(end)
	}
	if _, _, err := ParseRange(false, "2026-01-31", "10:00", "2026-01-31", "10:00"); err == nil {
		t.Fatal("相同时刻应拒绝")
	}
}

func TestMonthGridWeekStart(t *testing.T) {
	monday := MonthDays(2026, time.February, time.Monday)
	if monday[0].Date.Format("2006-01-02") != "2026-01-26" || len(monday) != 35 {
		t.Fatalf("周一 %s %d", monday[0].Date.Format("2006-01-02"), len(monday))
	}
	sunday := MonthDays(2026, time.February, time.Sunday)
	if sunday[0].Date.Format("2006-01-02") != "2026-02-01" || len(sunday) != 28 {
		t.Fatalf("周日 %s %d", sunday[0].Date.Format("2006-01-02"), len(sunday))
	}
	if Heads(time.Monday)[0] != "一" || Heads(time.Sunday)[0] != "日" {
		t.Fatal(Heads(time.Monday), Heads(time.Sunday))
	}
}

func TestPlaceCrossDay(t *testing.T) {
	days := MonthDays(2026, time.January, time.Monday)
	feb := MonthDays(2026, time.February, time.Monday)
	start := time.Date(2026, 1, 31, 22, 0, 0, 0, CST)
	end := time.Date(2026, 2, 1, 2, 0, 0, 0, CST)
	item := Event{ID: 1, Title: "跨日", Starts: start, Ends: end}
	placeOnDays(days, []Event{item})
	placeOnDays(feb, []Event{item})
	if !dayHas(days, "2026-01-31", 1) || !dayHas(days, "2026-02-01", 1) {
		t.Fatal("跨日应同时出现在 31 日和次日")
	}
	if dayHas(days, "2026-01-30", 1) {
		t.Fatal("不应出现在 30 日")
	}
	if !dayHas(feb, "2026-02-01", 1) || !dayHas(feb, "2026-01-31", 1) || dayHas(feb, "2026-02-02", 1) {
		t.Fatal("二月格子应含 31 日和 1 日, 不含 2 日")
	}
	allStart, allEnd, err := ParseRange(true, "2026-01-31", "", "2026-01-31", "")
	if err != nil {
		t.Fatal(err)
	}
	all := Event{ID: 2, Title: "全天", Starts: allStart, Ends: allEnd, AllDay: true}
	placeOnDays(days, []Event{all})
	placeOnDays(feb, []Event{all})
	if !dayHas(days, "2026-01-31", 2) || dayHas(feb, "2026-02-01", 2) {
		t.Fatal("全天月末不应占到下月")
	}
}

func dayHas(days []Day, date string, id int64) bool {
	for _, day := range days {
		if day.Date.Format("2006-01-02") != date {
			continue
		}
		for _, item := range day.Items {
			if item.ID == id {
				return true
			}
		}
		return false
	}
	return false
}
