package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeDB struct {
	err error
}

func (f fakeDB) Ping(context.Context) error { return f.err }

func newTestHandler(t *testing.T, db Pinger) http.Handler {
	t.Helper()
	assets, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	handler, err := New(db, assets)
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func TestHome(t *testing.T) {
	handler := newTestHandler(t, fakeDB{})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "维修组") || !strings.Contains(body, "/static/app.css?v=") {
		t.Fatalf("%s", body)
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'self'") || strings.Contains(csp, "unsafe-inline") {
		t.Fatal(csp)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal(rec.Header().Get("X-Content-Type-Options"))
	}
}

func TestNotFound(t *testing.T) {
	handler := newTestHandler(t, fakeDB{})
	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("状态 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "页面不存在") {
		t.Fatal(rec.Body.String())
	}
}

func TestHealth(t *testing.T) {
	handler := newTestHandler(t, fakeDB{})
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"database":"ok"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}

	handler = newTestHandler(t, fakeDB{err: context.DeadlineExceeded})
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable || strings.Contains(rec.Body.String(), "Deadline") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestStaticCache(t *testing.T) {
	handler := newTestHandler(t, fakeDB{})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	start := strings.Index(rec.Body.String(), "/static/app.css?v=")
	if start < 0 {
		t.Fatal(rec.Body.String())
	}
	ref := rec.Body.String()[start:]
	end := strings.IndexByte(ref, '"')
	ref = ref[:end]

	req = httptest.NewRequest(http.MethodGet, ref, nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态 %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Cache-Control"), "immutable") {
		t.Fatal(rec.Header().Get("Cache-Control"))
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "text/css") {
		t.Fatal(rec.Header().Get("Content-Type"))
	}
}

func TestVendorChecksums(t *testing.T) {
	text, err := fs.ReadFile(embedded, "vendor.sha256")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(text)), "\n")
	if len(lines) == 0 {
		t.Fatal("校验清单为空")
	}
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("校验行无效: %s", line)
		}
		data, err := fs.ReadFile(embedded, "static/"+fields[1])
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != fields[0] {
			t.Fatalf("%s 校验和不符", fields[1])
		}
	}
}

func TestBodyLimit(t *testing.T) {
	handler := limitBody(8, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if err == nil {
			t.Error("应拒绝过大的请求体")
		}
		w.WriteHeader(http.StatusRequestEntityTooLarge)
	}))
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("a", 64)))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("状态 %d", rec.Code)
	}
}

func TestRecover(t *testing.T) {
	handler := recoverPanic(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("状态 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "内部错误") {
		t.Fatal(rec.Body.String())
	}
}
