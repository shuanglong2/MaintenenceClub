package finance

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/people"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/audit"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/files"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/migrate"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/testkit"
)

func TestFinanceFlow(t *testing.T) {
	pool := testkit.Open(t)
	ctx := context.Background()
	if err := migrate.Up(ctx, pool, []migrate.Module{
		{Name: "audit", FS: audit.SQL},
		{Name: "people", FS: people.SQL},
		{Name: "files", FS: files.SQL},
		{Name: "finance", FS: SQL},
	}); err != nil {
		t.Fatal(err)
	}
	peopleSvc, err := people.New(pool, people.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := peopleSvc.CreateFirst(ctx, "leader", "leader-pass"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	leader, err := peopleSvc.Login(ctx, "leader", "leader-pass", "127.0.0.1", now)
	if err != nil {
		t.Fatal(err)
	}
	leaderActor := Actor{ID: leader.User.ID, Name: leader.Name, Role: leader.User.Role, Active: true}
	peopleActor := people.Actor{ID: leader.User.ID, Name: leader.Name, Role: leader.User.Role, Active: true}
	if _, err := peopleSvc.Create(ctx, peopleActor, people.Input{Name: "财务甲", Login: "teller", Role: "组员", Finance: true, Password: "teller-pass"}); err != nil {
		t.Fatal(err)
	}
	if _, err := peopleSvc.Create(ctx, peopleActor, people.Input{Name: "组员甲", Login: "member1", Role: "组员", Password: "member-pass"}); err != nil {
		t.Fatal(err)
	}
	teller, err := peopleSvc.Login(ctx, "teller", "teller-pass", "127.0.0.1", now)
	if err != nil {
		t.Fatal(err)
	}
	tellerActor := Actor{ID: teller.User.ID, Name: teller.Name, Role: teller.User.Role, Finance: true, Active: true}
	member, err := peopleSvc.Login(ctx, "member1", "member-pass", "127.0.0.1", now)
	if err != nil {
		t.Fatal(err)
	}
	memberActor := Actor{ID: member.User.ID, Name: member.Name, Role: member.User.Role, Active: true}
	store := files.New(pool, t.TempDir(), 1<<20)
	svc := New(pool, store)
	cats, err := svc.Categories(ctx, leaderActor)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]int64{}
	for _, cat := range cats {
		byName[cat.Name] = cat.ID
	}
	var last Entry
	for _, row := range handLedger() {
		day, err := time.Parse("2006-01-02", row.Month+"-15")
		if err != nil {
			t.Fatal(err)
		}
		last, err = svc.Create(ctx, tellerActor, Draft{
			Kind: row.Kind, Cents: row.Cents, Date: &day, CategoryID: byName[row.Category], Note: noteOf(row),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.Create(ctx, leaderActor, Draft{Kind: KindIn, Cents: 100, CategoryID: byName["其他"]}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("组长不能改财务: %v", err)
	}
	if _, _, err := svc.List(ctx, memberActor, Filter{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("组员不能看财务: %v", err)
	}
	if err := svc.Void(ctx, tellerActor, last.ID, "这笔记错了"); err != nil {
		t.Fatal(err)
	}
	rows, err := svc.Rows(ctx, leaderActor)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 30 {
		t.Fatalf("有效流水 %d", len(rows))
	}
	cash, savings := BalanceOf(rows)
	if cash != 75000 || savings != 37000 {
		t.Fatalf("库里的余额 %d %d", cash, savings)
	}
	gotCash, gotSavings, n, err := svc.Balance(ctx, leaderActor)
	if err != nil || gotCash != cash || gotSavings != savings || n != 30 {
		t.Fatalf("汇总 %d %d %d %v", gotCash, gotSavings, n, err)
	}
	months := map[string]Sum{}
	for _, sum := range ByMonth(rows) {
		months[sum.Key] = sum
	}
	expectMonth(t, months["2026-01"], 153000, 36500, 30000, 10000)
	expectMonth(t, months["2026-02"], 21000, 32500, 20000, 5000)
	expectMonth(t, months["2026-03"], 32000, 25000, 10000, 8000)
	listed, total, err := svc.List(ctx, leaderActor, Filter{Month: "2026-01"})
	if err != nil || total != 10 {
		t.Fatalf("一月 %d %v", total, err)
	}
	if listed[0].ActorName != "财务甲" {
		t.Fatal(listed[0].ActorName)
	}
	raw, err := svc.Export(ctx, leaderActor, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 3 || raw[0] != 0xEF || raw[1] != 0xBB || raw[2] != 0xBF || !strings.Contains(string(raw), "'=1+1") {
		t.Fatalf("导出 %q", raw[:min(80, len(raw))])
	}
	var exports int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit.events WHERE action = 'finance.export'`).Scan(&exports); err != nil || exports != 1 {
		t.Fatal(err, exports)
	}
	_, err = pool.Exec(ctx, `DELETE FROM finance.entries WHERE id = $1`, last.ID)
	if err == nil || !strings.Contains(err.Error(), "不能删除") {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `UPDATE finance.entries SET amount_cents = 1 WHERE id = $1`, listed[0].ID)
	if err == nil || !strings.Contains(err.Error(), "不能修改") {
		t.Fatal(err)
	}
	var amount int64
	if err := pool.QueryRow(ctx, `SELECT amount_cents FROM finance.entries WHERE id = $1`, listed[0].ID).Scan(&amount); err != nil || amount == 1 {
		t.Fatal(err, amount)
	}
	if err := svc.Attach(ctx, tellerActor, listed[0].ID, "invoice.pdf", strings.NewReader("%PDF-1.4\n1 0 obj\n<<>>\nendobj\ntrailer\n<<>>\n%%EOF\n")); err != nil {
		t.Fatal(err)
	}
	withFile, err := svc.Get(ctx, leaderActor, listed[0].ID)
	if err != nil || len(withFile.Files) != 1 {
		t.Fatal(err, withFile.Files)
	}
	ok, err := svc.CanSeeFile(ctx, memberActor, withFile.Files[0].ID)
	if err != nil || ok {
		t.Fatal(err, ok)
	}
	ok, err = svc.CanSeeFile(ctx, leaderActor, withFile.Files[0].ID)
	if err != nil || !ok {
		t.Fatal(err, ok)
	}
	html, err := svc.HomeHTML(ctx)
	if err != nil || !strings.Contains(string(html), "750.00") || !strings.Contains(string(html), "370.00") {
		t.Fatal(err, html)
	}
	if err := svc.AddCategory(ctx, tellerActor, "交通"); err != nil {
		t.Fatal(err)
	}
	if err := svc.AddCategory(ctx, tellerActor, "交通"); !errors.Is(err, ErrBadInput) {
		t.Fatal(err)
	}
}

func noteOf(row Row) string {
	if row.Month == "2026-01" && row.Kind == KindOut && row.Category == "其他" && row.Cents == 2000 {
		return "=1+1"
	}
	return ""
}
