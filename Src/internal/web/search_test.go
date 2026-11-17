package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/docs"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/finance"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/people"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/audit"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/files"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/migrate"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/testkit"
)

func TestSearchRedirectsWhenAnonymous(t *testing.T) {
	handler := newTestHandler(t, fakeDB{})
	req := httptest.NewRequest(http.MethodGet, "/search", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("%d %s", rec.Code, rec.Header().Get("Location"))
	}
}

func TestItemLink(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/inventory/items/2/qr.png", nil)
	req.Host = "10.20.238.143"
	got, err := itemLink(req, 2)
	if err != nil || got != "http://10.20.238.143/inventory/items/2" {
		t.Fatalf("%s %v", got, err)
	}
	req.Host = "bad host"
	if _, err := itemLink(req, 2); err == nil {
		t.Fatal("应拒绝带空格的主机")
	}
}

func TestSearchHidesPrivateRows(t *testing.T) {
	pool := testkit.Open(t)
	ctx := context.Background()
	if err := migrate.Up(ctx, pool, []migrate.Module{
		{Name: "audit", FS: audit.SQL},
		{Name: "people", FS: people.SQL},
		{Name: "files", FS: files.SQL},
		{Name: "docs", FS: docs.SQL},
		{Name: "finance", FS: finance.SQL},
	}); err != nil {
		t.Fatal(err)
	}
	svc, err := people.New(pool, people.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.CreateFirst(ctx, "m8leader", "leader-pass"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	leader, err := svc.Login(ctx, "m8leader", "leader-pass", "127.0.0.1", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ChangePassword(ctx, leader.User.ID, leader.Token, "leader-pass", "leader-pass"); err != nil {
		t.Fatal(err)
	}
	actor := people.Actor{ID: leader.User.ID, Name: leader.Name, Role: leader.User.Role, Active: true, Finance: true}
	if err := svc.Update(ctx, actor, leader.User.ID, people.Input{Name: leader.Name, Role: people.RoleLeader, Finance: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, actor, people.Input{Name: "组员甲", Login: "m8member", Role: "组员", Password: "member-pass"}); err != nil {
		t.Fatal(err)
	}
	member, err := svc.Login(ctx, "m8member", "member-pass", "127.0.0.1", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ChangePassword(ctx, member.User.ID, member.Token, "member-pass", "member-pass"); err != nil {
		t.Fatal(err)
	}
	docSvc := docs.New(pool, files.New(pool, t.TempDir(), 1<<20))
	docActor := docs.Actor{ID: actor.ID, Name: actor.Name, Role: actor.Role, Active: true}
	if _, err := docSvc.Save(ctx, docActor, docs.Draft{Title: "内部机密词", Body: "仅管理层", Visibility: "management"}); err != nil {
		t.Fatal(err)
	}
	if _, err := docSvc.Save(ctx, docActor, docs.Draft{Title: "公开示波器", Body: "用法", Visibility: "all"}); err != nil {
		t.Fatal(err)
	}
	var categoryID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM finance.categories WHERE name = '其他'`).Scan(&categoryID); err != nil {
		t.Fatal(err)
	}
	fin := finance.New(pool, nil)
	if _, err := fin.Create(ctx, finance.Actor{
		ID: actor.ID, Name: actor.Name, Role: actor.Role, Finance: true, Active: true,
	}, finance.Draft{Kind: finance.KindIn, Cents: 100, CategoryID: categoryID, Note: "报销机密"}); err != nil {
		t.Fatal(err)
	}
	assets, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	handler, err := New(pool, assets, Option{People: svc, Docs: docSvc, Finance: fin})
	if err != nil {
		t.Fatal(err)
	}
	memberCookie := loginCookie(t, handler, "m8member", "member-pass")
	memberBody := searchBody(t, handler, memberCookie, "机密")
	if strings.Contains(memberBody, "内部机密词") || strings.Contains(memberBody, "报销机密") {
		t.Fatalf("组员看见了不该看的内容: %s", memberBody)
	}
	if !strings.Contains(memberBody, "没有结果") {
		t.Fatalf("组员应看到空的文档结果: %s", memberBody)
	}
	leaderCookie := loginCookie(t, handler, "m8leader", "leader-pass")
	leaderBody := searchBody(t, handler, leaderCookie, "机密")
	if !strings.Contains(leaderBody, "内部机密词") || !strings.Contains(leaderBody, "报销机密") {
		t.Fatalf("组长应看到文档和财务: %s", leaderBody)
	}
}

func searchBody(t *testing.T, handler http.Handler, cookie *http.Cookie, query string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/search?q="+query, nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态 %d %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}
