// Package testkit 为集成测试准备独立数据库.
package testkit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Open 在 MCLUB_TEST_DSN 指向的服务器上新建一个数据库并返回连接池.
// 测试结束时删除该数据库. CI 里没有 DSN 则失败, 本机没有则跳过.
func Open(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("MCLUB_TEST_DSN")
	if dsn == "" {
		if os.Getenv("CI") == "true" {
			t.Fatal("CI 中必须设置 MCLUB_TEST_DSN")
		}
		t.Skip("未设置 MCLUB_TEST_DSN")
	}

	ctx := context.Background()
	adminCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	adminCfg.MaxConns = 1
	admin, err := pgxpool.NewWithConfig(ctx, adminCfg)
	if err != nil {
		t.Fatal(err)
	}

	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	name := "t_" + hex.EncodeToString(raw[:])
	ident := `"` + name + `"`
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+ident); err != nil {
		admin.Close()
		t.Fatalf("创建测试库: %v", err)
	}

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		admin.Close()
		t.Fatal(err)
	}
	cfg.ConnConfig.Database = name
	cfg.MaxConns = 4
	cfg.MinConns = 0
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		_, _ = admin.Exec(ctx, "DROP DATABASE "+ident+" WITH (FORCE)")
		admin.Close()
		t.Fatal(err)
	}

	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(), "DROP DATABASE "+ident+" WITH (FORCE)")
		admin.Close()
	})
	return pool
}
