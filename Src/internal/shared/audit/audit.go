// Package audit 写入只能追加的操作记录.
package audit

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed sql/*.sql
var embedded embed.FS

// SQL 是迁移文件, 根目录就是 sql 文件本身.
var SQL = mustSub()

func mustSub() fs.FS {
	sub, err := fs.Sub(embedded, "sql")
	if err != nil {
		panic(err)
	}
	return sub
}

// Event 是一条审计. Detail 不要放密码哈希或联系方式.
type Event struct {
	ActorID   int64
	ActorName string
	IP        string
	Action    string
	Entity    string
	EntityID  *int64
	Summary   string
	Detail    any
}

// Write 在调用方的事务里插入一条记录.
func Write(ctx context.Context, tx pgx.Tx, event Event) error {
	detail := event.Detail
	if detail == nil {
		detail = map[string]any{}
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("审计详情: %w", err)
	}
	_, err = tx.Exec(ctx, `
INSERT INTO audit.events (actor_id, actor_name, ip, action, entity, entity_id, summary, detail)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)`,
		event.ActorID, event.ActorName, event.IP, event.Action, event.Entity, event.EntityID, event.Summary, raw)
	if err != nil {
		return fmt.Errorf("写入审计: %w", err)
	}
	return nil
}

// Entry 是查询结果.
type Entry struct {
	ID        int64
	At        string
	ActorID   int64
	ActorName string
	IP        string
	Action    string
	Entity    string
	EntityID  *int64
	Summary   string
}

// List 按条件分页. 时间筛选由调用方转成 timestamptz.
func List(ctx context.Context, pool *pgxpool.Pool, q Query) ([]Entry, error) {
	limit := q.Limit
	if limit < 1 || limit > 100 {
		limit = 50
	}
	rows, err := pool.Query(ctx, `
SELECT id, to_char(at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
       actor_id, actor_name, ip, action, entity, entity_id, summary
FROM audit.events
WHERE ($1::bigint IS NULL OR actor_id = $1)
  AND ($2 = '' OR action = $2)
  AND ($3::timestamptz IS NULL OR at >= $3)
  AND ($4::timestamptz IS NULL OR at < $4)
ORDER BY at DESC, id DESC
LIMIT $5 OFFSET $6`, q.ActorID, q.Action, q.From, q.Until, limit, q.Offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		var item Entry
		if err := rows.Scan(&item.ID, &item.At, &item.ActorID, &item.ActorName, &item.IP, &item.Action, &item.Entity, &item.EntityID, &item.Summary); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// Query 是审计筛选. From 含起点, Until 不含终点.
type Query struct {
	ActorID *int64
	Action  string
	From    *time.Time
	Until   *time.Time
	Limit   int
	Offset  int
}
