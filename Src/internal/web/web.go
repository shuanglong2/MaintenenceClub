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

	"github.com/shuanglong2/MaintenenceClub/Src/internal/docs"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/events"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/finance"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/inventory"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/logs"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/people"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/buildinfo"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/tasks"
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
	Title      string
	Body       string
	HTML       template.HTML
	Status     int
	Message    string
	Error      string
	Notice     string
	CSRF       string
	User       *userView
	ShowAudit  bool
	ShowManage bool
	Finance    bool
	People     []people.PersonView
	Person     people.PersonView
	HasPerson  bool
	Editing    bool
	Audit      []auditRow
	Filter     auditFilter
	Widgets    []widgetView
	Offset     int
	Prev       int
	HasNext    bool
	Doc        docView
	Inv        invView
	Evt        eventView
	Task       taskView
	Log        logView
	Fin        finView
	Sys        systemView
	Find       findView
	Path       string
}

type userView struct {
	Name       string
	MustChange bool
}

type server struct {
	assets    *Assets
	db        Pinger
	people    *people.Service
	docs      *docs.Service
	inv       *inventory.Service
	events    *events.Service
	tasks     *tasks.Service
	logs      *logs.Service
	finance   *finance.Service
	weekStart time.Weekday
	secure    bool
	proxy     string
	tpl       *template.Template
}

// Option 是网站运行时需要的设置.
type Option struct {
	People       *people.Service
	Docs         *docs.Service
	Inventory    *inventory.Service
	Events       *events.Service
	Tasks        *tasks.Service
	Logs         *logs.Service
	Finance      *finance.Service
	WeekStart    time.Weekday
	Secure       bool
	TrustedProxy string
}

// New 组装路由和中间件.
func New(db Pinger, assets *Assets, opt Option) (http.Handler, error) {
	tpl := template.New("").Funcs(template.FuncMap{
		"static":       assets.URL,
		"time8":        formatTime,
		"yuan":         formatYuan,
		"add":          func(a, b int) int { return a + b },
		"stateLabel":   inventory.StateLabel,
		"kindLabel":    inventory.KindLabel,
		"statusLabel":  events.StatusLabel,
		"whenLabel":    events.WhenLabel,
		"taskStatus":   tasks.StatusLabel,
		"taskPriority": tasks.PriorityLabel,
		"logKind":      logs.KindLabel,
		"logStatus":    logs.StatusLabel,
		"logTarget":    logs.TargetLabel,
		"finKind":      finance.KindLabel,
		"navOn": func(path, prefix string) bool {
			return path == prefix || strings.HasPrefix(path, prefix+"/")
		},
	})
	tpl, err := tpl.ParseFS(assets.fsys, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("解析模板: %w", err)
	}
	s := &server{
		assets: assets, db: db, people: opt.People, docs: opt.Docs, inv: opt.Inventory, events: opt.Events, tasks: opt.Tasks, logs: opt.Logs, finance: opt.Finance,
		weekStart: opt.WeekStart, secure: opt.Secure, proxy: opt.TrustedProxy, tpl: tpl,
	}
	mux := http.NewServeMux()
	s.routes(mux)
	var handler http.Handler = mux
	handler = s.gatePassword(handler)
	handler = s.checkCSRF(handler)
	handler = s.loadSession(handler)
	handler = http.NewCrossOriginProtection().Handler(handler)
	handler = limitBody(20<<20, handler)
	handler = securityHeaders(handler)
	handler = accessLog(handler)
	handler = requestID(handler)
	handler = recoverPanic(handler)
	return handler, nil
}

func (s *server) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /static/{path...}", s.staticFile)
	mux.HandleFunc("GET /search", s.search)
	mux.HandleFunc("GET /login", s.loginForm)
	mux.HandleFunc("POST /login", s.login)
	mux.HandleFunc("POST /logout", s.logout)
	mux.HandleFunc("GET /people", s.peopleList)
	mux.HandleFunc("GET /people/new", s.peopleNew)
	mux.HandleFunc("POST /people", s.peopleCreate)
	mux.HandleFunc("GET /people/import", s.peopleImportForm)
	mux.HandleFunc("POST /people/import", s.peopleImport)
	mux.HandleFunc("GET /people/{id}", s.peopleDetail)
	mux.HandleFunc("GET /people/{id}/edit", s.peopleEdit)
	mux.HandleFunc("POST /people/{id}", s.peopleUpdate)
	mux.HandleFunc("POST /people/{id}/exit", s.peopleExit)
	mux.HandleFunc("POST /people/{id}/restore", s.peopleRestore)
	mux.HandleFunc("POST /people/{id}/reset-password", s.peopleReset)
	mux.HandleFunc("GET /account", s.account)
	mux.HandleFunc("POST /account/password", s.accountPassword)
	mux.HandleFunc("POST /account/contact", s.accountContact)
	mux.HandleFunc("GET /audit", s.auditList)
	mux.HandleFunc("GET /docs", s.docsIndex)
	mux.HandleFunc("GET /docs/search", s.docsSearch)
	mux.HandleFunc("GET /docs/export", s.docsExport)
	mux.HandleFunc("GET /docs/new", s.docsNew)
	mux.HandleFunc("POST /docs", s.docsCreate)
	mux.HandleFunc("POST /docs/folders", s.docsFolderCreate)
	mux.HandleFunc("POST /docs/folders/{id}", s.docsFolderUpdate)
	mux.HandleFunc("POST /docs/upload", s.docsUpload)
	mux.HandleFunc("POST /md/preview", s.mdPreview)
	mux.HandleFunc("GET /docs/p/{id}/edit", s.docsEdit)
	mux.HandleFunc("POST /docs/p/{id}", s.docsSave)
	mux.HandleFunc("POST /docs/p/{id}/conflict", s.docsConflict)
	mux.HandleFunc("GET /docs/p/{id}/history/{version}", s.docsVersion)
	mux.HandleFunc("POST /docs/p/{id}/history/{version}", s.docsRestore)
	mux.HandleFunc("GET /docs/p/{id}/history", s.docsHistory)
	mux.HandleFunc("POST /docs/p/{id}/void", s.docsVoid)
	mux.HandleFunc("POST /docs/p/{id}/unvoid", s.docsUnvoid)
	mux.HandleFunc("POST /docs/p/{id}/attach", s.docsAttach)
	mux.HandleFunc("GET /docs/p/{id}/{slug}", s.docsView)
	mux.HandleFunc("GET /docs/p/{id}", s.docsView)
	mux.HandleFunc("GET /files/{id}", s.fileDownload)
	mux.HandleFunc("GET /inventory", s.inventoryList)
	mux.HandleFunc("GET /inventory/low", s.inventoryLow)
	mux.HandleFunc("GET /inventory/new", s.inventoryNew)
	mux.HandleFunc("POST /inventory", s.inventoryCreate)
	mux.HandleFunc("GET /inventory/import", s.inventoryImportForm)
	mux.HandleFunc("POST /inventory/import", s.inventoryImport)
	mux.HandleFunc("GET /inventory/template.csv", s.inventoryTemplate)
	mux.HandleFunc("GET /inventory/export.csv", s.inventoryExport)
	mux.HandleFunc("POST /inventory/locations", s.inventoryLocation)
	mux.HandleFunc("GET /inventory/items/{id}/edit", s.inventoryEdit)
	mux.HandleFunc("GET /inventory/items/{id}/label", s.inventoryLabel)
	mux.HandleFunc("GET /inventory/items/{id}/qr.png", s.inventoryQR)
	mux.HandleFunc("POST /inventory/items/{id}/photo", s.inventoryPhoto)
	mux.HandleFunc("POST /inventory/items/{id}/void", s.inventoryVoid)
	mux.HandleFunc("POST /inventory/items/{id}/unvoid", s.inventoryUnvoid)
	mux.HandleFunc("POST /inventory/items/{id}/move", s.inventoryMove)
	mux.HandleFunc("POST /inventory/items/{id}/event", s.inventoryEvent)
	mux.HandleFunc("POST /inventory/items/{id}", s.inventoryUpdate)
	mux.HandleFunc("GET /inventory/items/{id}", s.inventoryItem)
	mux.HandleFunc("GET /inventory/stocktakes", s.stocktakes)
	mux.HandleFunc("POST /inventory/stocktakes", s.stocktakeCreate)
	mux.HandleFunc("POST /inventory/stocktakes/{id}", s.stocktakeSave)
	mux.HandleFunc("GET /inventory/stocktakes/{id}", s.stocktakeShow)
	mux.HandleFunc("GET /events", s.eventsMonth)
	mux.HandleFunc("GET /events/list", s.eventsList)
	mux.HandleFunc("GET /events/new", s.eventsNew)
	mux.HandleFunc("POST /events", s.eventsCreate)
	mux.HandleFunc("GET /events/{id}/edit", s.eventsEdit)
	mux.HandleFunc("POST /events/{id}/signup", s.eventsSignup)
	mux.HandleFunc("POST /events/{id}/cancel", s.eventsCancel)
	mux.HandleFunc("POST /events/{id}/check", s.eventsCheck)
	mux.HandleFunc("POST /events/{id}/summary", s.eventsSummary)
	mux.HandleFunc("POST /events/{id}/files/remove", s.eventsDetach)
	mux.HandleFunc("POST /events/{id}/files", s.eventsUpload)
	mux.HandleFunc("POST /events/{id}/copy", s.eventsCopy)
	mux.HandleFunc("POST /events/{id}", s.eventsUpdate)
	mux.HandleFunc("GET /events/{id}", s.eventsShow)
	mux.HandleFunc("GET /tasks", s.tasksList)
	mux.HandleFunc("GET /tasks/board", s.tasksBoard)
	mux.HandleFunc("GET /tasks/new", s.tasksNew)
	mux.HandleFunc("POST /tasks", s.tasksCreate)
	mux.HandleFunc("GET /tasks/{id}/edit", s.tasksEdit)
	mux.HandleFunc("POST /tasks/{id}/status", s.tasksStatus)
	mux.HandleFunc("POST /tasks/{id}/comments", s.tasksComment)
	mux.HandleFunc("POST /tasks/{id}", s.tasksUpdate)
	mux.HandleFunc("GET /tasks/{id}", s.tasksShow)
	mux.HandleFunc("GET /logs", s.logsList)
	mux.HandleFunc("GET /logs/new", s.logsNew)
	mux.HandleFunc("POST /logs", s.logsCreate)
	mux.HandleFunc("GET /logs/{id}/edit", s.logsEdit)
	mux.HandleFunc("POST /logs/{id}/status", s.logsStatus)
	mux.HandleFunc("POST /logs/{id}/files", s.logsUpload)
	mux.HandleFunc("POST /logs/{id}/void", s.logsVoid)
	mux.HandleFunc("POST /logs/{id}", s.logsUpdate)
	mux.HandleFunc("GET /logs/{id}", s.logsShow)
	mux.HandleFunc("GET /finance", s.financeList)
	mux.HandleFunc("GET /finance/new", s.financeNew)
	mux.HandleFunc("POST /finance", s.financeCreate)
	mux.HandleFunc("GET /finance/stats", s.financeStats)
	mux.HandleFunc("GET /finance/export.csv", s.financeExport)
	mux.HandleFunc("POST /finance/categories", s.financeCategory)
	mux.HandleFunc("POST /finance/{id}/void", s.financeVoid)
	mux.HandleFunc("POST /finance/{id}/files", s.financeUpload)
	mux.HandleFunc("GET /finance/{id}", s.financeShow)
	mux.HandleFunc("GET /system", s.systemStatus)
	mux.HandleFunc("/", s.notFound)
}

func (s *server) home(w http.ResponseWriter, r *http.Request) {
	data := page{Title: "维修组", Body: "home-body"}
	if user, ok := visitorFrom(r); ok {
		data.Widgets = renderWidgets(r.Context(), user.Session.User)
	}
	s.render(w, r, http.StatusOK, data)
}

func (s *server) notFound(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, http.StatusNotFound, page{
		Title:   "未找到",
		Body:    "error-body",
		Status:  http.StatusNotFound,
		Message: "页面不存在",
	})
}

func (s *server) render(w http.ResponseWriter, r *http.Request, status int, data page) {
	s.fill(r, &data)
	var body bytes.Buffer
	if err := s.tpl.ExecuteTemplate(&body, data.Body, data); err != nil {
		slog.Error("渲染页面", "err", err, "body", data.Body)
		http.Error(w, "内部错误", http.StatusInternalServerError)
		return
	}
	data.HTML = template.HTML(body.String())
	var buf bytes.Buffer
	if err := s.tpl.ExecuteTemplate(&buf, "layout", data); err != nil {
		slog.Error("渲染布局", "err", err)
		http.Error(w, "内部错误", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

func (s *server) renderPart(w http.ResponseWriter, r *http.Request, name string, data page) {
	s.fill(r, &data)
	var buf bytes.Buffer
	if err := s.tpl.ExecuteTemplate(&buf, name, data); err != nil {
		slog.Error("渲染片段", "err", err, "body", name)
		http.Error(w, "内部错误", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
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
