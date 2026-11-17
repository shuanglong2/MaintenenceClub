package tasks

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/events"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/inventory"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/people"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/audit"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/files"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/migrate"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/testkit"
)

func TestTaskFlow(t *testing.T) {
	pool := testkit.Open(t)
	ctx := context.Background()
	if err := migrate.Up(ctx, pool, []migrate.Module{
		{Name: "audit", FS: audit.SQL},
		{Name: "people", FS: people.SQL},
		{Name: "files", FS: files.SQL},
		{Name: "inventory", FS: inventory.SQL},
		{Name: "events", FS: events.SQL},
		{Name: "tasks", FS: SQL},
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
	if _, err := peopleSvc.Create(ctx, peopleActor, people.Input{
		Name: "组员甲", Login: "member1", Role: "组员", Password: "member-pass",
	}); err != nil {
		t.Fatal(err)
	}
	member, err := peopleSvc.Login(ctx, "member1", "member-pass", "127.0.0.1", now)
	if err != nil {
		t.Fatal(err)
	}
	memberActor := Actor{ID: member.User.ID, Name: member.Name, Role: member.User.Role, Active: true}
	inv := inventory.New(pool, nil)
	item, err := inv.Create(ctx, inventory.Actor{ID: leader.User.ID, Name: leader.Name, Role: leader.User.Role, Active: true}, inventory.Draft{
		Code: "T1", Name: "电烙铁", Category: "工具",
	})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 11, 17, 19, 0, 0, 0, events.CST)
	meeting, err := events.New(pool, nil).Create(ctx, events.Actor{ID: leader.User.ID, Name: leader.Name, Role: leader.User.Role, Active: true}, events.Draft{
		Title: "本周例会", Kind: "例会", Starts: start, Ends: start.Add(time.Hour), OwnerID: leader.User.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := New(pool)
	due := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	own, err := svc.Create(ctx, memberActor, Draft{
		Title: "整理台面", Kind: "实验室保养", Priority: "high", OwnerID: memberActor.ID, Due: &due,
		EventID: meeting.ID, Items: []int64{item.ID},
	})
	if err != nil || own.EventTitle != "本周例会" || len(own.Items) != 1 || own.Items[0].Code != "T1" {
		t.Fatalf("%v %+v", err, own)
	}
	if _, err := svc.Update(ctx, memberActor, own.ID, Draft{
		Title: "整理台面", Kind: "实验室保养", Status: "doing", OwnerID: memberActor.ID, EventID: meeting.ID, Items: []int64{item.ID},
	}); err != nil {
		t.Fatal(err)
	}
	foreign, err := svc.Create(ctx, leaderActor, Draft{Title: "组长的任务", Kind: "其他", OwnerID: leaderActor.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Update(ctx, memberActor, foreign.ID, Draft{Title: "改掉", Kind: "其他", Status: statusTodo, OwnerID: memberActor.ID}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("组员不能改别人的任务: %v", err)
	}
	if err := svc.SetStatus(ctx, memberActor, foreign.ID, statusDone); !errors.Is(err, ErrForbidden) {
		t.Fatalf("组员不能改别人的状态: %v", err)
	}
	assigned, err := svc.Create(ctx, leaderActor, Draft{Title: "交给组员", Kind: "其他", OwnerID: memberActor.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Update(ctx, memberActor, assigned.ID, Draft{Title: "交给组员了", Kind: "其他", Status: statusTodo, OwnerID: memberActor.ID}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetStatus(ctx, memberActor, assigned.ID, statusDoing); err != nil {
		t.Fatal(err)
	}
	var summary string
	if err := pool.QueryRow(ctx, `SELECT summary FROM audit.events WHERE action = 'tasks.status' AND entity_id = $1 ORDER BY id DESC LIMIT 1`, assigned.ID).Scan(&summary); err != nil {
		t.Fatal(err)
	}
	if summary != "把状态从 待处理 改成 进行中" {
		t.Fatal(summary)
	}
	if err := svc.Comment(ctx, memberActor, assigned.ID, "明天开始"); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Get(ctx, leaderActor, assigned.ID)
	if err != nil || len(got.Comments) != 1 || got.Comments[0].Body != "明天开始" || got.Status != statusDoing {
		t.Fatalf("%v %+v", err, got)
	}
	mine, total, err := svc.List(ctx, memberActor, Filter{Mine: true, Limit: 1})
	if err != nil || total != 2 || len(mine) != 1 {
		t.Fatalf("我的 %v %d %+v", err, total, mine)
	}
	board, err := svc.Board(ctx, memberActor)
	if err != nil || len(board) != 4 {
		t.Fatal(err, len(board))
	}
	if _, err := svc.Create(ctx, memberActor, Draft{Title: "已做完", Kind: "其他", Status: statusDone, OwnerID: memberActor.ID}); err != nil {
		t.Fatal(err)
	}
	soon := dateOnly(time.Now().AddDate(0, 0, 2))
	far := dateOnly(time.Now().AddDate(0, 0, 40))
	if _, err := svc.Create(ctx, memberActor, Draft{Title: "快到期", Kind: "其他", OwnerID: memberActor.ID, Due: &soon}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, memberActor, Draft{Title: "很远", Kind: "其他", OwnerID: memberActor.ID, Due: &far}); err != nil {
		t.Fatal(err)
	}
	openHTML, err := svc.HomeOpenHTML(ctx, memberActor)
	if err != nil || !strings.Contains(string(openHTML), "整理台面") || !strings.Contains(string(openHTML), "交给组员了") || strings.Contains(string(openHTML), "已做完") || strings.Contains(string(openHTML), "组长的任务") {
		t.Fatalf("组员首页 %v %s", err, openHTML)
	}
	leadHTML, err := svc.HomeOpenHTML(ctx, leaderActor)
	if err != nil || !strings.Contains(string(leadHTML), "组长的任务") || !strings.Contains(string(leadHTML), "交给组员了") || strings.Contains(string(leadHTML), "整理台面") {
		t.Fatalf("组长首页 %v %s", err, leadHTML)
	}
	dueHTML, err := svc.HomeDueHTML(ctx, memberActor)
	if err != nil || !strings.Contains(string(dueHTML), "快到期") || strings.Contains(string(dueHTML), "很远") {
		t.Fatalf("即将到期 %v %s", err, dueHTML)
	}
}
