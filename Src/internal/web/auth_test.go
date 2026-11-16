package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestLoginCSRF(t *testing.T) {
	handler := newTestHandler(t, fakeDB{})
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态 %d", rec.Code)
	}
	var token string
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == loginCSRFCookie {
			token = cookie.Value
		}
	}
	if token == "" {
		t.Fatal("缺少登录校验")
	}

	form := url.Values{"login": {"a"}, "password": {"secret123"}}
	req = httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("无令牌状态 %d", rec.Code)
	}

	form.Set("csrf", token)
	req = httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: loginCSRFCookie, Value: token})
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code == http.StatusForbidden {
		t.Fatal("带令牌的请求不应被 CSRF 拒绝")
	}
}

func TestClientIPTrustsProxy(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Real-IP", "10.1.2.3")
	if got := clientIP(req, "127.0.0.1"); got != "10.1.2.3" {
		t.Fatal(got)
	}
	req.RemoteAddr = "10.9.9.9:1234"
	if got := clientIP(req, "127.0.0.1"); got != "10.9.9.9" {
		t.Fatal(got)
	}
}
