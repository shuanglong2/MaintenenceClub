package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/docs"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/events"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/inventory"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/people"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/audit"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/buildinfo"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/config"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/db"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/files"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/migrate"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/tasks"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/web"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = cmdServe()
	case "migrate":
		err = cmdMigrate()
	case "admin":
		err = cmdAdmin(os.Args[2:])
	case "version":
		fmt.Printf("mclub %s %s %s\n", buildinfo.Version, buildinfo.Commit, buildinfo.Built)
	case "help", "-h", "--help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		slog.Error("命令失败", "err", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `用法:
  mclub serve                         启动网站
  mclub migrate                       把数据库补到最新
  mclub version                       显示版本
  mclub admin create <登录名>         创建首个组长, 密码从终端读取
  mclub admin reset-password <登录名> 重置密码并打印一次
`)
}

func cmdServe() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	slog.Info("配置",
		"env", cfg.Env,
		"addr", cfg.Addr,
		"data", cfg.Data,
		"quota_mb", cfg.UploadQuotaMB,
		"cookie_secure", cfg.CookieSecure,
		"trusted_proxy", cfg.TrustedProxy,
		"dev_assets", cfg.DevAssets != "",
	)
	if err := os.MkdirAll(cfg.Data, 0o750); err != nil {
		return fmt.Errorf("创建上传目录: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, err := db.Open(ctx, cfg.DB)
	if err != nil {
		return err
	}
	defer pool.Close()
	assets, err := web.Open(cfg.DevAssets)
	if err != nil {
		return err
	}
	svc, err := people.New(pool, people.ProdCost)
	if err != nil {
		return err
	}
	store := files.New(pool, cfg.Data, int64(cfg.UploadQuotaMB)<<20)
	docSvc := docs.New(pool, store)
	if err := docSvc.EnsureWelcome(ctx); err != nil {
		slog.Warn("写入说明文档失败", "err", err)
	}
	inv := inventory.New(pool, store)
	web.RegisterInventoryWidget(inv)
	eventSvc := events.New(pool, store)
	taskSvc := tasks.New(pool)
	go cleanUploads(ctx, store)
	handler, err := web.New(pool, assets, web.Option{
		People:       svc,
		Docs:         docSvc,
		Inventory:    inv,
		Events:       eventSvc,
		Tasks:        taskSvc,
		WeekStart:    cfg.WeekStart,
		Secure:       cfg.CookieSecure,
		TrustedProxy: cfg.TrustedProxy,
	})
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		slog.Info("开始监听", "addr", cfg.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	select {
	case <-ctx.Done():
		shut, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return server.Shutdown(shut)
	case err := <-errCh:
		return err
	}
}

func cmdMigrate() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, err := db.Open(ctx, cfg.DB)
	if err != nil {
		return err
	}
	defer pool.Close()
	return migrate.Up(ctx, pool, modules())
}

// 模块顺序在这里固定. 后面的模块只能依赖前面的模块.
func modules() []migrate.Module {
	return []migrate.Module{
		{Name: "audit", FS: audit.SQL},
		{Name: "people", FS: people.SQL},
		{Name: "files", FS: files.SQL},
		{Name: "docs", FS: docs.SQL},
		{Name: "inventory", FS: inventory.SQL},
		{Name: "events", FS: events.SQL},
		{Name: "tasks", FS: tasks.SQL},
	}
}

func cleanUploads(ctx context.Context, store *files.Store) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	if _, err := store.CleanOrphans(ctx, time.Hour); err != nil {
		slog.Warn("清理上传文件失败", "err", err)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := store.CleanOrphans(ctx, time.Hour); err != nil {
				slog.Warn("清理上传文件失败", "err", err)
			}
		}
	}
}
