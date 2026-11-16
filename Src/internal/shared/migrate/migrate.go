// Package migrate 按顺序应用嵌入的 SQL 文件, 只前进, 不回退.
package migrate

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"regexp"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
)

// 固定的会话锁. 迁移期间一直占用同一个连接, 避免锁落在连接池的其他连接上.
const lockKey int64 = 745231981234567890

var fileName = regexp.MustCompile(`^(\d+)_([a-z0-9_]+)\.sql$`)

// Module 是一个模块的迁移目录. FS 的根目录直接放 SQL 文件.
type Module struct {
	Name string
	FS   fs.FS
}

type migrationFile struct {
	version int
	name    string
	body    string
}

// Up 创建记录表并应用尚未执行的迁移. 重复调用是安全的.
func Up(ctx context.Context, pool *pgxpool.Pool, modules []Module) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("获取连接: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", lockKey); err != nil {
		return fmt.Errorf("获取迁移锁: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", lockKey)
	}()

	if err := ensureTable(ctx, conn); err != nil {
		return err
	}
	for _, module := range modules {
		if err := applyModule(ctx, conn, module); err != nil {
			return fmt.Errorf("%s: %w", module.Name, err)
		}
	}
	return nil
}

func ensureTable(ctx context.Context, conn *pgxpool.Conn) error {
	_, err := conn.Exec(ctx, `
CREATE TABLE IF NOT EXISTS public.schema_migrations (
    module text NOT NULL,
    version integer NOT NULL,
    name text NOT NULL,
    applied_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (module, version)
)`)
	if err != nil {
		return fmt.Errorf("创建迁移记录表: %w", err)
	}
	return nil
}

func applyModule(ctx context.Context, conn *pgxpool.Conn, module Module) error {
	if !moduleName.MatchString(module.Name) {
		return fmt.Errorf("模块名无效")
	}
	files, err := readFiles(module.FS)
	if err != nil {
		return err
	}
	applied, err := appliedVersions(ctx, conn, module.Name)
	if err != nil {
		return err
	}
	for _, file := range files {
		if applied[file.version] {
			continue
		}
		stmts, err := splitSQL(file.body)
		if err != nil {
			return fmt.Errorf("%s: %w", file.name, err)
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}
		for _, stmt := range stmts {
			if _, err := tx.Exec(ctx, stmt); err != nil {
				_ = tx.Rollback(ctx)
				return fmt.Errorf("%s: %w", file.name, err)
			}
		}
		_, err = tx.Exec(ctx, `
INSERT INTO public.schema_migrations (module, version, name)
VALUES ($1, $2, $3)`, module.Name, file.version, file.name)
		if err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("%s: %w", file.name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("%s: %w", file.name, err)
		}
		slog.Info("已应用迁移", "module", module.Name, "file", file.name)
	}
	return nil
}

var moduleName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

func readFiles(fsys fs.FS) ([]migrationFile, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("读取迁移目录: %w", err)
	}
	var files []migrationFile
	seen := map[int]string{}
	for _, entry := range entries {
		if entry.IsDir() {
			return nil, fmt.Errorf("迁移目录不能包含子目录 %s", entry.Name())
		}
		m := fileName.FindStringSubmatch(entry.Name())
		if m == nil {
			return nil, fmt.Errorf("迁移文件名无效: %s", entry.Name())
		}
		version, err := strconv.Atoi(m[1])
		if err != nil || version < 1 {
			return nil, fmt.Errorf("迁移版本无效: %s", entry.Name())
		}
		if prev, ok := seen[version]; ok {
			return nil, fmt.Errorf("迁移版本 %d 重复: %s 与 %s", version, prev, entry.Name())
		}
		seen[version] = entry.Name()
		body, err := fs.ReadFile(fsys, entry.Name())
		if err != nil {
			return nil, err
		}
		files = append(files, migrationFile{version: version, name: entry.Name(), body: string(body)})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].version < files[j].version })
	return files, nil
}

func appliedVersions(ctx context.Context, conn *pgxpool.Conn, module string) (map[int]bool, error) {
	rows, err := conn.Query(ctx, `SELECT version FROM public.schema_migrations WHERE module = $1`, module)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	applied := map[int]bool{}
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return applied, nil
}
