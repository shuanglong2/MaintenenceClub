package events

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

func TestEventFlow(t *testing.T) {
	pool := testkit.Open(t)
	ctx := context.Background()
	if err := migrate.Up(ctx, pool, []migrate.Module{
		{Name: "audit", FS: audit.SQL},
		{Name: "people", FS: people.SQL},
		{Name: "files", FS: files.SQL},
		{Name: "events", FS: SQL},
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
	if _, err := peopleSvc.Create(ctx, people.Actor{ID: leader.User.ID, Name: leader.Name, Role: leader.User.Role, Active: true}, people.Input{
		Name: "组员甲", Login: "member1", Role: "组员", Password: "member-pass",
	}); err != nil {
		t.Fatal(err)
	}
	member, err := peopleSvc.Login(ctx, "member1", "member-pass", "127.0.0.1", now)
	if err != nil {
		t.Fatal(err)
	}
	memberActor := Actor{ID: member.User.ID, Name: member.Name, Role: member.User.Role, Active: true}
	store := files.New(pool, t.TempDir(), 20<<20)
	svc := New(pool, store)

	start := time.Date(2026, 1, 31, 22, 0, 0, 0, CST)
	end := time.Date(2026, 2, 1, 2, 0, 0, 0, CST)
	if _, err := svc.Create(ctx, memberActor, Draft{Title: "不该成功", Kind: "例会", Starts: start, Ends: end}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("组员不应创建活动: %v", err)
	}
	item, err := svc.Create(ctx, leaderActor, Draft{
		Title: "跨日例会", Kind: "例会", Place: "实验室", Body: "带上笔记本",
		Starts: start, Ends: end, OwnerID: leaderActor.ID,
	})
	if err != nil || item.OwnerName == "" {
		t.Fatal(err, item.OwnerName)
	}
	jan, err := svc.Month(ctx, memberActor, 2026, time.January, time.Monday)
	if err != nil || !dayHas(jan, "2026-01-31", item.ID) {
		t.Fatalf("一月 %v", err)
	}
	feb, err := svc.Month(ctx, memberActor, 2026, time.February, time.Monday)
	if err != nil || !dayHas(feb, "2026-02-01", item.ID) {
		t.Fatalf("二月 %v", err)
	}
	allStart, allEnd, err := ParseRange(true, "2026-01-31", "", "2026-01-31", "")
	if err != nil {
		t.Fatal(err)
	}
	allDay, err := svc.Create(ctx, leaderActor, Draft{
		Title: "保养日", Kind: "保养日", AllDay: true, Starts: allStart, Ends: allEnd, OwnerID: leaderActor.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	jan, err = svc.Month(ctx, memberActor, 2026, time.January, time.Monday)
	feb, err2 := svc.Month(ctx, memberActor, 2026, time.February, time.Monday)
	if err != nil || err2 != nil || !dayHas(jan, "2026-01-31", allDay.ID) || dayHas(feb, "2026-02-01", allDay.ID) {
		t.Fatalf("全天 %v %v", err, err2)
	}

	soonStart := time.Now().Add(48 * time.Hour)
	soon, err := svc.Create(ctx, leaderActor, Draft{
		Title: "即将例会", Kind: "例会", Starts: soonStart, Ends: soonStart.Add(2 * time.Hour), OwnerID: leaderActor.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Signup(ctx, memberActor, soon.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.Check(ctx, memberActor, soon.ID, memberActor.ID, true); !errors.Is(err, ErrForbidden) {
		t.Fatalf("组员不能代签到: %v", err)
	}
	if err := svc.Check(ctx, leaderActor, soon.ID, memberActor.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := svc.Cancel(ctx, memberActor, soon.ID); err == nil || !strings.Contains(err.Error(), "已签到") {
		t.Fatal(err)
	}
	if err := svc.Check(ctx, leaderActor, soon.ID, memberActor.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := svc.Cancel(ctx, memberActor, soon.ID); err != nil {
		t.Fatal(err)
	}

	closedStart := time.Now().Add(48 * time.Hour)
	closedUntil := time.Now().Add(-time.Hour)
	closed, err := svc.Create(ctx, leaderActor, Draft{
		Title: "已截止", Kind: "其他", Starts: closedStart, Ends: closedStart.Add(time.Hour),
		OwnerID: leaderActor.ID, SignupUntil: &closedUntil,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Signup(ctx, memberActor, closed.ID); err == nil || !strings.Contains(err.Error(), "截止") {
		t.Fatal(err)
	}

	copied, err := svc.CopyNextWeek(ctx, leaderActor, item.ID)
	if err != nil || !copied.Starts.Equal(item.Starts.AddDate(0, 0, 7)) || copied.Summary != "" {
		t.Fatalf("%v %+v", err, copied)
	}
	if err := svc.SaveSummary(ctx, memberActor, item.ID, "不行"); !errors.Is(err, ErrForbidden) {
		t.Fatal(err)
	}
	if err := svc.SaveSummary(ctx, leaderActor, item.ID, "到了 **8** 人"); err != nil {
		t.Fatal(err)
	}
	meta, err := store.Save(ctx, leaderActor.ID, "events", "note.pdf", strings.NewReader("%PDF-1.4\n1 0 obj<<>>endobj\ntrailer<<>>\n%%EOF\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Attach(ctx, leaderActor, item.ID, meta.ID); err != nil {
		t.Fatal(err)
	}
	ok, err := svc.CanSeeFile(ctx, memberActor, meta.ID)
	if err != nil || !ok {
		t.Fatal(err, ok)
	}
	ok, err = svc.CanSeeFile(ctx, memberActor, meta.ID+99)
	if err != nil || ok {
		t.Fatal(err, ok)
	}
}
