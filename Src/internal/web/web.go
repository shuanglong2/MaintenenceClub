// Package web 提供页面, 静态文件和健康检查.
package web

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/buildinfo"
)

// Pinger 是健康检查需要的数据库能力.
type Pinger interface {
	Ping(context.Context) error
}

// Assets 是模板和静态文件, 可以来自嵌入内容或开发目录.
type Assets struct {
	fsys fs.FS
	hash map[string]string
}

// Open 加载资源. devDir 非空时从磁盘读取, 便于改页面后直接刷新.
func Open(devDir string) (*Assets, error) {
	var fsys fs.FS = embedded
	if devDir != "" {
		fsys = os.DirFS(devDir)
	}
	if _, err := fs.Stat(fsys, "templates/layout.html"); err != nil {
		return nil, fmt.Errorf("找不到页面模板: %w", err)
	}
	hash := map[string]string{}
	err := fs.WalkDir(fsys, "static", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		hash[strings.TrimPrefix(name, "static/")] = hex.EncodeToString(sum[:8])
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("读取静态文件: %w", err)
	}
	return &Assets{fsys: fsys, hash: hash}, nil
}

// URL 返回带内容哈希的静态文件地址.
func (a *Assets) URL(name string) string {
	if hash, ok := a.hash[name]; ok {
		return "/static/" + name + "?v=" + hash
	}
	return "/static/" + name
}

type page struct {
	Title   string
	Body    string
	Status  int
	Message string
}

type server struct {
	assets *Assets
	db     Pinger
	tpl    *template.Template
}

// New 组装路由和中间件.
func New(db Pinger, assets *Assets) (http.Handler, error) {
	tpl := template.New("").Funcs(template.FuncMap{
		"static": assets.URL,
	})
	tpl, err := tpl.ParseFS(assets.fsys, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("解析模板: %w", err)
	}
	s := &server{assets: assets, db: db, tpl: tpl}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /static/{path...}", s.staticFile)
	mux.HandleFunc("/", s.notFound)
	var handler http.Handler = mux
	handler = limitBody(2 << 20, handler)
	handler = securityHeaders(handler)
	handler = accessLog(handler)
	handler = requestID(handler)
	handler = recoverPanic(handler)
	return handler, nil
}

func (s *server) home(w http.ResponseWriter, _ *http.Request) {
	s.render(w, http.StatusOK, page{Title: "维修组", Body: "home-body"})
}

func (s *server) notFound(w http.ResponseWriter, _ *http.Request) {
	s.render(w, http.StatusNotFound, page{
		Title:   "未找到",
		Body:    "error-body",
		Status:  http.StatusNotFound,
		Message: "页面不存在",
	})
}

func (s *server) render(w http.ResponseWriter, status int, data page) {
	var buf bytes.Buffer
	if err := s.tpl.ExecuteTemplate(&buf, "layout", data); err != nil {
		slog.Error("渲染页面", "err", err)
		http.Error(w, "内部错误", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

func (s *server) staticFile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("path")
	if name == "" || !fs.ValidPath(name) {
		s.notFound(w, r)
		return
	}
	data, err := fs.ReadFile(s.assets.fsys, path.Join("static", name))
	if err != nil {
		s.notFound(w, r)
		return
	}
	if v := r.URL.Query().Get("v"); v != "" && v == s.assets.hash[name] {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}

func (s *server) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	status := "ok"
	database := "ok"
	code := http.StatusOK
	if err := s.db.Ping(ctx); err != nil {
		slog.Warn("数据库不可用", "err", err)
		status = "degraded"
		database = "error"
		code = http.StatusServiceUnavailable
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":   status,
		"database": database,
		"version":  buildinfo.Version,
		"commit":   buildinfo.Commit,
		"built":    buildinfo.Built,
	})
}
