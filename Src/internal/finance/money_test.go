package finance

import "testing"

func TestLedger(t *testing.T) {
	rows := handLedger()
	cash, savings := BalanceOf(rows)
	if cash != 75000 || savings != 37000 {
		t.Fatalf("余额 手头 %d 储蓄 %d", cash, savings)
	}
	months := map[string]Sum{}
	for _, sum := range ByMonth(rows) {
		months[sum.Key] = sum
	}
	expectMonth(t, months["2026-01"], 153000, 36500, 30000, 10000)
	expectMonth(t, months["2026-02"], 21000, 32500, 20000, 5000)
	expectMonth(t, months["2026-03"], 32000, 25000, 10000, 8000)
	if months["2026-01"].Cash() != 96500 || months["2026-01"].Savings() != 20000 {
		t.Fatal(months["2026-01"])
	}
	if months["2026-02"].Cash() != -26500 || months["2026-03"].Cash() != 5000 {
		t.Fatal(months["2026-02"].Cash(), months["2026-03"].Cash())
	}
	cats := map[string]Sum{}
	for _, sum := range ByCategory(rows) {
		cats[sum.Key] = sum
	}
	if cats["期初"].In != 100000 || cats["学校拨款"].In != 100000 || cats["报销"].Out != 5000 {
		t.Fatal(cats["期初"], cats["学校拨款"], cats["报销"])
	}
	if cats["活动经费"].Out != 19000 || cats["器材"].Out != 45000 || cats["耗材"].Out != 17500 {
		t.Fatal(cats["活动经费"], cats["器材"], cats["耗材"])
	}
	other := cats["其他"]
	if other.In != 6000 || other.Out != 7500 || other.Save != 60000 || other.Draw != 23000 {
		t.Fatal(other)
	}
}

func TestParseYuan(t *testing.T) {
	cases := []struct {
		text string
		want int64
		ok   bool
	}{
		{"12.34", 1234, true},
		{"12.3", 1230, true},
		{"12", 1200, true},
		{"0.01", 1, true},
		{" 8.00 ", 800, true},
		{"0", 0, false},
		{"0.00", 0, false},
		{"-1", 0, false},
		{"1.234", 0, false},
		{"12.", 0, false},
		{".5", 0, false},
		{"1,234.00", 0, false},
	}
	for _, item := range cases {
		got, err := ParseYuan(item.text)
		if item.ok {
			if err != nil || got != item.want {
				t.Fatalf("%q -> %d %v", item.text, got, err)
			}
			continue
		}
		if err == nil {
			t.Fatalf("%q 应拒绝", item.text)
		}
	}
}

func expectMonth(t *testing.T, sum Sum, in, out, save, draw int64) {
	t.Helper()
	if sum.In != in || sum.Out != out || sum.Save != save || sum.Draw != draw {
		t.Fatalf("%s %+v", sum.Key, sum)
	}
}

func handLedger() []Row {
	type item struct {
		month    string
		kind     string
		category string
		note     string
		cents    int64
	}
	raw := []item{
		{"2026-01", KindIn, "期初", "", 100000},
		{"2026-01", KindOut, "耗材", "", 5000},
		{"2026-01", KindOut, "器材", "", 20000},
		{"2026-01", KindSave, "其他", "", 30000},
		{"2026-01", KindIn, "学校拨款", "", 50000},
		{"2026-01", KindOut, "活动经费", "", 8000},
		{"2026-01", KindOut, "报销", "", 1500},
		{"2026-01", KindDraw, "其他", "", 10000},
		{"2026-01", KindOut, "其他", "=1+1", 2000},
		{"2026-01", KindIn, "其他", "", 3000},
		{"2026-02", KindIn, "学校拨款", "", 20000},
		{"2026-02", KindOut, "耗材", "", 4000},
		{"2026-02", KindOut, "器材", "", 15000},
		{"2026-02", KindSave, "其他", "", 20000},
		{"2026-02", KindOut, "活动经费", "", 6000},
		{"2026-02", KindOut, "报销", "", 2500},
		{"2026-02", KindDraw, "其他", "", 5000},
		{"2026-02", KindIn, "其他", "", 1000},
		{"2026-02", KindOut, "其他", "", 3500},
		{"2026-02", KindOut, "耗材", "", 1500},
		{"2026-03", KindIn, "学校拨款", "", 30000},
		{"2026-03", KindOut, "器材", "", 10000},
		{"2026-03", KindOut, "耗材", "", 2000},
		{"2026-03", KindSave, "其他", "", 10000},
		{"2026-03", KindOut, "活动经费", "", 5000},
		{"2026-03", KindOut, "报销", "", 1000},
		{"2026-03", KindDraw, "其他", "", 8000},
		{"2026-03", KindIn, "其他", "", 2000},
		{"2026-03", KindOut, "其他", "", 2000},
		{"2026-03", KindOut, "耗材", "", 5000},
		{"2026-03", KindOut, "器材", "", 99999},
	}
	rows := make([]Row, len(raw))
	for i, item := range raw {
		rows[i] = Row{Kind: item.kind, Cents: item.cents, Month: item.month, Category: item.category, Voided: i == len(raw)-1}
	}
	return rows
}
