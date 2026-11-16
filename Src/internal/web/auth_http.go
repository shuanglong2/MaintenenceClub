package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/people"
)

const (
	sessionCookie   = "mclub_session"
	loginCSRFCookie = "mclub_login_csrf"
)

type visitor struct {
	Session people.Session
	IP      string
}

type ctxKey int

const visitorKey ctxKey = 2

func (s *server) loadSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.people == nil {
			next.ServeHTTP(w, r)
			return
		}
		cookie, err := r.Cookie(sessionCookie)
		if err != nil || cookie.Value == "" {
			next.ServeHTTP(w, r)
			return
		}
		session, err := s.people.Lookup(r.Context(), cookie.Value, time.Now())
		if err != nil {
			clearCookie(w, sessionCookie, s.secure)
			next.ServeHTTP(w, r)
			return
		}
		ctx := context.WithValue(r.Context(), visitorKey, visitor{Session: session, IP: clientIP(r, s.proxy)})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *server) checkCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "请求无法读取", http.StatusBadRequest)
			return
		}
		token := r.FormValue("csrf")
		if token == "" {
			token = r.Header.Get("X-CSRF-Token")
		}
		expected := ""
		if r.URL.Path == "/login" {
			if cookie, err := r.Cookie(loginCSRFCookie); err == nil {
				expected = cookie.Value
			}
		} else if user, ok := visitorFrom(r); ok {
			expected = user.Session.CSRF
		}
		if !sameToken(expected, token) {
			http.Error(w, "请求未通过校验", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) gatePassword(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := visitorFrom(r)
		if ok && user.Session.MustChange && !allowedWhileChanging(r) {
			http.Redirect(w, r, "/account", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func allowedWhileChanging(r *http.Request) bool {
	switch r.URL.Path {
	case "/account", "/account/password", "/logout", "/healthz", "/login":
		return true
	}
	return strings.HasPrefix(r.URL.Path, "/static/")
}

func (s *server) loginForm(w http.ResponseWriter, r *http.Request) {
	if _, ok := visitorFrom(r); ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	token, err := randomToken()
	if err != nil {
		s.internal(w, r, err)
		return
	}
	setCookie(w, loginCSRFCookie, token, s.secure, 30*60)
	s.render(w, r, http.StatusOK, page{Title: "登录", Body: "login-body", CSRF: token})
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	if s.people == nil {
		s.internal(w, r, errors.New("人员服务未配置"))
		return
	}
	session, err := s.people.Login(r.Context(), r.FormValue("login"), r.FormValue("password"), clientIP(r, s.proxy), time.Now())
	if err != nil {
		code := http.StatusUnauthorized
		if errors.Is(err, people.ErrRateLimited) {
			code = http.StatusTooManyRequests
		}
		s.render(w, r, code, page{Title: "登录", Body: "login-body", Error: err.Error(), CSRF: csrfFrom(r)})
		return
	}
	setCookie(w, sessionCookie, session.Token, s.secure, int((14 * 24 * time.Hour).Seconds()))
	clearCookie(w, loginCSRFCookie, s.secure)
	if session.MustChange {
		http.Redirect(w, r, "/account", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if s.people != nil {
		if cookie, err := r.Cookie(sessionCookie); err == nil {
			_ = s.people.Logout(r.Context(), cookie.Value)
		}
	}
	clearCookie(w, sessionCookie, s.secure)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *server) fill(r *http.Request, data *page) {
	user, ok := visitorFrom(r)
	if !ok {
		return
	}
	if data.CSRF == "" {
		data.CSRF = user.Session.CSRF
	}
	data.User = &userView{Name: user.Session.Name, MustChange: user.Session.MustChange}
	data.ShowAudit = people.Can(user.Session.User, people.ActAuditRead, people.Resource{})
	data.ShowManage = people.Can(user.Session.User, people.ActPeopleManage, people.Resource{})
}

func (s *server) requireUser(w http.ResponseWriter, r *http.Request) (visitor, bool) {
	user, ok := visitorFrom(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return visitor{}, false
	}
	return user, true
}

func (s *server) internal(w http.ResponseWriter, r *http.Request, err error) {
	slog.Error("请求失败", "err", err, "path", r.URL.Path)
	s.render(w, r, http.StatusInternalServerError, page{
		Title: "出错了", Body: "error-body", Status: http.StatusInternalServerError, Message: "内部错误",
	})
}

func (s *server) showErr(w http.ResponseWriter, r *http.Request, err error) {
	code := http.StatusBadRequest
	switch {
	case errors.Is(err, people.ErrForbidden):
		code = http.StatusForbidden
	case errors.Is(err, people.ErrNotFound):
		code = http.StatusNotFound
	case errors.Is(err, people.ErrRateLimited):
		code = http.StatusTooManyRequests
	case errors.Is(err, people.ErrBadLogin), errors.Is(err, people.ErrNoSession):
		code = http.StatusUnauthorized
	}
	if code == http.StatusBadRequest && !known(err) {
		s.internal(w, r, err)
		return
	}
	s.render(w, r, code, page{Title: "提示", Body: "error-body", Status: code, Message: err.Error()})
}

func known(err error) bool {
	return errors.Is(err, people.ErrForbidden) || errors.Is(err, people.ErrNotFound) || errors.Is(err, people.ErrBadInput) ||
		errors.Is(err, people.ErrLoginTaken) || errors.Is(err, people.ErrLastLeader) || errors.Is(err, people.ErrPassword) ||
		errors.Is(err, people.ErrBadLogin) || errors.Is(err, people.ErrExists) || errors.Is(err, people.ErrNoSession)
}

func visitorFrom(r *http.Request) (visitor, bool) {
	user, ok := r.Context().Value(visitorKey).(visitor)
	return user, ok
}

func csrfFrom(r *http.Request) string {
	if cookie, err := r.Cookie(loginCSRFCookie); err == nil {
		return cookie.Value
	}
	return ""
}

func clientIP(r *http.Request, trusted string) string {
	host := r.RemoteAddr
	if split, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		host = split
	}
	if ip := net.ParseIP(host); ip != nil && ip.String() == trusted {
		if real := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-IP"))); real != nil {
			return real.String()
		}
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.String()
	}
	return host
}

func sameToken(expected, got string) bool {
	if expected == "" || len(expected) != len(got) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(got)) == 1
}

func randomToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func setCookie(w http.ResponseWriter, name, value string, secure bool, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: value, Path: "/", HttpOnly: true, Secure: secure,
		SameSite: http.SameSiteLaxMode, MaxAge: maxAge,
	})
}

func clearCookie(w http.ResponseWriter, name string, secure bool) {
	setCookie(w, name, "", secure, -1)
}
