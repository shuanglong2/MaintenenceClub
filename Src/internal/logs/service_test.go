package logs

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/inventory"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/people"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/audit"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/files"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/migrate"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/testkit"
)

func TestLogFlow(t *testing.T) {
	pool := testkit.Open(t)
	ctx := context.Background()
	if err := migrate.Up(ctx, pool, []migrate.Module{
		{Name: "audit", FS: audit.SQL},
		{Name: "people", FS: people.SQL},
		{Name: "files", FS: files.SQL},
		{Name: "inventory", FS: inventory.SQL},
		{Name: "logs", FS: SQL},
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
	if _, err := peopleSvc.Create(ctx, peopleActor, people.Input{Name: "组员甲", Login: "member1", Role: "组员", Password: "member-pass"}); err != nil {
		t.Fatal(err)
	}
	if _, err := peopleSvc.Create(ctx, peopleActor, people.Input{Name: "组员乙", Login: "member2", Role: "组员", Password: "member-pass"}); err != nil {
		t.Fatal(err)
	}
	member, err := peopleSvc.Login(ctx, "member1", "member-pass", "127.0.0.1", now)
	if err != nil {
		t.Fatal(err)
	}
	memberActor := Actor{ID: member.User.ID, Name: member.Name, Role: member.User.Role, Active: true}
	other, err := peopleSvc.Login(ctx, "member2", "member-pass", "127.0.0.1", now)
	if err != nil {
		t.Fatal(err)
	}
	otherActor := Actor{ID: other.User.ID, Name: other.Name, Role: other.User.Role, Active: true}
	inv := inventory.New(pool, nil)
	item, err := inv.Create(ctx, inventory.Actor{ID: leader.User.ID, Name: leader.Name, Role: leader.User.Role, Active: true}, inventory.Draft{
		Code: "S1", Name: "焊锡丝", Category: "消耗品", Qty: 5,
	})
	if err != nil {
		t.Fatal(err)
	}
	store := files.New(pool, t.TempDir(), 1<<20)
	svc := New(pool, store, inv)
	day := dateOnly(now)
	repair, err := svc.Create(ctx, memberActor, Draft{
		Kind: kindRepair, Date: &day, Symptom: "换焊锡", Action: "重焊", Target: "computer",
		Source: "高一3班", Contact: "13800001111", Parts: []Part{{ItemID: item.ID, Qty: 2}},
	})
	if err != nil || repair.Contact != "13800001111" || len(repair.Parts) != 1 {
		t.Fatalf("%v %+v", err, repair)
	}
	got, err := inv.Get(ctx, inventory.Actor{ID: leader.User.ID, Name: leader.Name, Role: leader.User.Role, Active: true}, item.ID)
	if err != nil || got.Qty != 3 {
		t.Fatalf("领用后 %v %+v", err, got.Qty)
	}
	hidden, err := svc.Get(ctx, otherActor, repair.ID)
	if err != nil || hidden.Contact != "" {
		t.Fatalf("别人看见了联系方式: %v %q", err, hidden.Contact)
	}
	if seen, err := svc.Get(ctx, leaderActor, repair.ID); err != nil || seen.Contact != "13800001111" {
		t.Fatalf("组长应能看联系方式 %v %q", err, seen.Contact)
	}
	var stored string
	if err := pool.QueryRow(ctx, `SELECT contact FROM logs.repairs WHERE entry_id = $1`, repair.ID).Scan(&stored); err != nil || stored != "13800001111" {
		t.Fatal(err, stored)
	}
	work, err := svc.Create(ctx, leaderActor, Draft{Kind: kindWork, Date: &day, Body: "组长值班", Minutes: intPtr(30)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Update(ctx, memberActor, work.ID, Draft{Kind: kindWork, Date: &day, Body: "改掉"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("组员不能改别人的日志: %v", err)
	}
	if err := svc.SetStatus(ctx, memberActor, repair.ID, statusDoing); err != nil {
		t.Fatal(err)
	}
	var summary string
	if err := pool.QueryRow(ctx, `SELECT summary FROM audit.events WHERE action = 'logs.status' AND entity_id = $1`, repair.ID).Scan(&summary); err != nil {
		t.Fatal(err)
	}
	if summary != "把状态从 受理 改成 处理中" {
		t.Fatal(summary)
	}
	if _, err := svc.Update(ctx, memberActor, repair.ID, Draft{
		Kind: kindRepair, Date: &day, Symptom: "换焊锡", Status: statusDoing, Target: "computer", Parts: []Part{{ItemID: item.ID, Qty: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	got, err = inv.Get(ctx, inventory.Actor{ID: leader.User.ID, Name: leader.Name, Role: leader.User.Role, Active: true}, item.ID)
	if err != nil || got.Qty != 4 {
		t.Fatalf("改数量后 %v %+v", err, got.Qty)
	}
	found, _, err := svc.List(ctx, otherActor, Filter{Query: "换焊锡"})
	if err != nil || len(found) != 1 {
		t.Fatalf("搜索 %v %+v", err, found)
	}
	if leaked, _, err := svc.List(ctx, otherActor, Filter{Query: "13800001111"}); err != nil || len(leaked) != 0 {
		t.Fatalf("搜索到了联系方式 %v %+v", err, leaked)
	}
	history, err := svc.ItemHistoryHTML(ctx, item.ID)
	if err != nil || !strings.Contains(string(history), "换焊锡") {
		t.Fatal(err, history)
	}
	if err := svc.Attach(ctx, memberActor, repair.ID, "note.txt", strings.NewReader("现场照片说明")); err != nil {
		t.Fatal(err)
	}
	withFile, err := svc.Get(ctx, memberActor, repair.ID)
	if err != nil || len(withFile.Files) != 1 {
		t.Fatal(err, withFile.Files)
	}
	ok, err := svc.CanSeeFile(ctx, otherActor, withFile.Files[0].ID)
	if err != nil || !ok {
		t.Fatal(err, ok)
	}
	if err := svc.Void(ctx, memberActor, repair.ID, "填错了"); err != nil {
		t.Fatal(err)
	}
	got, err = inv.Get(ctx, inventory.Actor{ID: leader.User.ID, Name: leader.Name, Role: leader.User.Role, Active: true}, item.ID)
	if err != nil || got.Qty != 5 {
		t.Fatalf("作废后 %v %+v", err, got.Qty)
	}
	if _, err := svc.Create(ctx, memberActor, Draft{
		Kind: kindRepair, Date: &day, Symptom: "一次用太多", Target: "other", Parts: []Part{{ItemID: item.ID, Qty: 9}},
	}); err == nil || !strings.Contains(err.Error(), "库存不足") {
		t.Fatal(err)
	}
	got, err = inv.Get(ctx, inventory.Actor{ID: leader.User.ID, Name: leader.Name, Role: leader.User.Role, Active: true}, item.ID)
	if err != nil || got.Qty != 5 {
		t.Fatalf("失败应回滚 %v %+v", err, got.Qty)
	}
	var extra int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM logs.entries WHERE body = '' AND id <> $1 AND author_id = $2`, repair.ID, memberActor.ID).Scan(&extra); err != nil {
		t.Fatal(err)
	}
	if extra != 0 {
		t.Fatalf("失败的记录被留下 %d", extra)
	}
}

func intPtr(n int) *int { return &n }
