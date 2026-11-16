package people

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/audit"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/migrate"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/testkit"
)

func TestPeopleFlow(t *testing.T) {
	pool := testkit.Open(t)
	ctx := context.Background()
	if err := migrate.Up(ctx, pool, []migrate.Module{
		{Name: "audit", FS: audit.SQL},
		{Name: "people", FS: SQL},
	}); err != nil {
		t.Fatal(err)
	}
	svc, err := New(pool, MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.CreateFirst(ctx, "leader", "leader-pass"); err != nil {
		t.Fatal(err)
	}
	if err := svc.CreateFirst(ctx, "other", "leader-pass"); !errors.Is(err, ErrExists) {
		t.Fatalf("第二次创建: %v", err)
	}
	now := time.Now()
	session, err := svc.Login(ctx, "Leader", "leader-pass", "127.0.0.1", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Lookup(ctx, session.Token, now); err != nil {
		t.Fatal(err)
	}
	actor := Actor{ID: session.User.ID, Name: session.Name, Role: session.User.Role, Active: true}
	if _, err := svc.Create(ctx, actor, Input{Name: "学号用户", Login: "07240506", Role: "组员", Password: "member-pass"}); err != nil {
		t.Fatal(err)
	}
	password, err := svc.Create(ctx, actor, Input{Name: "组员甲", Login: "member1", Role: "组员", Contact: "13800000000", Password: "member-pass"})
	if err != nil || password != "member-pass" {
		t.Fatalf("%s %v", password, err)
	}
	memberSession, err := svc.Login(ctx, "member1", password, "10.0.0.8", now)
	if err != nil {
		t.Fatal(err)
	}
	views, err := svc.List(ctx, memberSession.User)
	if err != nil {
		t.Fatal(err)
	}
	for _, view := range views {
		if view.ID != memberSession.User.ID && (view.Contact != "" || view.StudentID != "") {
			t.Fatalf("组员看见了别人的联系方式: %+v", view)
		}
	}
	leaderView, err := svc.Get(ctx, session.User, memberSession.User.ID)
	if err != nil || leaderView.Contact != "13800000000" {
		t.Fatalf("%+v %v", leaderView, err)
	}

	second, err := svc.Login(ctx, "member1", password, "10.0.0.9", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ChangePassword(ctx, memberSession.User.ID, memberSession.Token, password, "member-pass2"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Lookup(ctx, second.Token, now); !errors.Is(err, ErrNoSession) {
		t.Fatalf("其他会话应失效: %v", err)
	}
	if _, err := svc.Lookup(ctx, memberSession.Token, now); err != nil {
		t.Fatal(err)
	}

	if err := svc.Exit(ctx, actor, session.User.ID); !errors.Is(err, ErrLastLeader) {
		t.Fatalf("不能退出最后的组长: %v", err)
	}
	if err := svc.Update(ctx, actor, session.User.ID, Input{Name: "组长", Role: RoleMember}); !errors.Is(err, ErrLastLeader) {
		t.Fatalf("不能降级最后的组长: %v", err)
	}
	if err := svc.Update(ctx, actor, memberSession.User.ID, Input{Name: "组员甲", Role: RoleLeader}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Update(ctx, actor, session.User.ID, Input{Name: "原组长", Role: RoleDeputy}); err != nil {
		t.Fatal(err)
	}
	if err := svc.Exit(ctx, Actor{ID: memberSession.User.ID, Name: "组员甲", Role: RoleLeader, Active: true}, session.User.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Lookup(ctx, session.Token, now); !errors.Is(err, ErrNoSession) {
		t.Fatalf("退出后会话应失效: %v", err)
	}

	imported, err := svc.Import(ctx, Actor{ID: memberSession.User.ID, Name: "组员甲", Role: RoleLeader, Active: true}, []Imported{
		{Name: "张三", Login: "zhang", Role: "组员"},
	})
	if err != nil || len(imported) != 1 || imported[0].Password == "" {
		t.Fatalf("%+v %v", imported, err)
	}
	_, err = pool.Exec(ctx, `DELETE FROM audit.events`)
	if err == nil {
		t.Fatal("审计记录不应允许删除")
	}
	if !strings.Contains(err.Error(), "审计记录不能修改或删除") {
		t.Fatal(err)
	}
}
