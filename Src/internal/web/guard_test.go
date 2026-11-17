package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/events"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/finance"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/inventory"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/people"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/audit"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/files"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/migrate"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/testkit"
)

func TestSystemRedirectsWhenAnonymous(t *testing.T) {
	handler := newTestHandler(t, fakeDB{})
	req := httptest.NewRequest(http.MethodGet, "/system", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("%d %s", rec.Code, rec.Header().Get("Location"))
	}
}

func TestLoginCookieFlags(t *testing.T) {
	handler := newTestHandler(t, fakeDB{})
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var found bool
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name != loginCSRFCookie {
			continue
		}
		found = true
		if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
			t.Fatalf("%+v", cookie)
		}
	}
	if !found {
		t.Fatal("缺少登录 cookie")
	}
}

func TestStaticPathEscape(t *testing.T) {
	handler := newTestHandler(t, fakeDB{})
	req := httptest.NewRequest(http.MethodGet, "/static/../etc/passwd", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK || strings.Contains(rec.Body.String(), "root:") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestMemberBlockedFromManagement(t *testing.T) {
	pool := testkit.Open(t)
	ctx := context.Background()
	if err := migrate.Up(ctx, pool, []migrate.Module{
		{Name: "audit", FS: audit.SQL},
		{Name: "people", FS: people.SQL},
	}); err != nil {
		t.Fatal(err)
	}
	svc, err := people.New(pool, people.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.CreateFirst(ctx, "m7leader", "leader-pass"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	leader, err := svc.Login(ctx, "m7leader", "leader-pass", "127.0.0.1", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, people.Actor{
		ID: leader.User.ID, Name: leader.Name, Role: leader.User.Role, Active: true,
	}, people.Input{Name: "组员甲", Login: "m7member", Role: "组员", Password: "member-pass"}); err != nil {
		t.Fatal(err)
	}
	if err := svc.ChangePassword(ctx, leader.User.ID, leader.Token, "leader-pass", "leader-pass"); err != nil {
		t.Fatal(err)
	}
	memberSession, err := svc.Login(ctx, "m7member", "member-pass", "127.0.0.1", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ChangePassword(ctx, memberSession.User.ID, memberSession.Token, "member-pass", "member-pass"); err != nil {
		t.Fatal(err)
	}
	store := files.New(pool, t.TempDir(), 6<<30)
	assets, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	handler, err := New(pool, assets, Option{
		People:    svc,
		Inventory: inventory.New(pool, store),
		Events:    events.New(pool, store),
		Finance:   finance.New(pool, store),
	})
	if err != nil {
		t.Fatal(err)
	}
	member := loginCookie(t, handler, "m7member", "member-pass")
	blocked := map[string]int{
		"/system":               http.StatusNotFound,
		"/finance":              http.StatusNotFound,
		"/finance/new":          http.StatusNotFound,
		"/finance/stats":        http.StatusNotFound,
		"/finance/export.csv":   http.StatusNotFound,
		"/audit":                http.StatusForbidden,
		"/people/new":           http.StatusForbidden,
		"/people/import":        http.StatusForbidden,
		"/inventory/new":        http.StatusForbidden,
		"/inventory/import":     http.StatusForbidden,
		"/inventory/stocktakes": http.StatusForbidden,
		"/events/new":           http.StatusForbidden,
	}
	for path, want := range blocked {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(member)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("%s 状态 %d, 期望 %d, %s", path, rec.Code, want, rec.Body.String())
		}
	}
	home := httptest.NewRequest(http.MethodGet, "/", nil)
	home.AddCookie(member)
	homeRec := httptest.NewRecorder()
	handler.ServeHTTP(homeRec, home)
	if homeRec.Code != http.StatusOK || strings.Contains(homeRec.Body.String(), `href="/system"`) {
		t.Fatalf("组员首页 %d", homeRec.Code)
	}

	leaderCookie := loginCookie(t, handler, "m7leader", "leader-pass")
	req := httptest.NewRequest(http.MethodGet, "/system", nil)
	req.AddCookie(leaderCookie)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("组长状态 %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, part := range []string{"系统状态", "磁盘剩余", "还没有备份记录", `href="/system"`} {
		if !strings.Contains(body, part) {
			t.Fatalf("缺少 %s", part)
		}
	}
}

func loginCookie(t *testing.T, handler http.Handler, name, password string) *http.Cookie {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var csrf *http.Cookie
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == loginCSRFCookie {
			csrf = cookie
		}
	}
	if csrf == nil {
		t.Fatal("缺少登录校验")
	}
	form := url.Values{"login": {name}, "password": {password}, "csrf": {csrf.Value}}
	req = httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(csrf)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("登录 %d %s", rec.Code, rec.Body.String())
	}
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == sessionCookie && cookie.Value != "" {
			return cookie
		}
	}
	t.Fatal("没有会话")
	return nil
}
