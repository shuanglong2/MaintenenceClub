package tasks

import (
	"context"
	"html"
	"html/template"
	"strconv"
	"strings"
	"time"
)

const homeLimit = 5

var cst = time.FixedZone("CST", 8*3600)

type homeItem struct {
	ID     int64
	Title  string
	Status string
	Due    time.Time
}

// HomeBoardHTML 是全组未完成的任务, 最多 8 条. 完成和取消的不出现.
func (s *Service) HomeBoardHTML(ctx context.Context, actor Actor) (template.HTML, error) {
	if !canUse(actor) {
		return "", ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `
SELECT t.id, t.title, t.status, t.due_on,
       CASE WHEN COALESCE(t.claimed_name, '') = '' THEN '未接取' ELSE t.claimed_name END
FROM tasks.tasks t
WHERE t.status IN ('todo', 'doing')
  AND ($1::boolean OR t.claimed_by IS NULL OR t.claimed_by = $2)
ORDER BY CASE WHEN t.claimed_by = $2 THEN 0 WHEN t.priority = 'high' THEN 1 WHEN t.claimed_by IS NULL THEN 2 ELSE 3 END,
         t.created_at, t.id
LIMIT 8`, actor.leader(), actor.ID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var b strings.Builder
	n := 0
	for rows.Next() {
		var item homeItem
		var due *time.Time
		var owner string
		if err := rows.Scan(&item.ID, &item.Title, &item.Status, &due, &owner); err != nil {
			return "", err
		}
		if due != nil {
			item.Due = *due
		}
		if n == 0 {
			b.WriteString("<ul>")
		}
		n++
		b.WriteString(`<li><a href="/tasks/`)
		b.WriteString(strconv.FormatInt(item.ID, 10))
		b.WriteString(`">`)
		b.WriteString(html.EscapeString(item.Title))
		b.WriteString("</a> ")
		b.WriteString(html.EscapeString(StatusLabel(item.Status)))
		b.WriteString(" · ")
		b.WriteString(html.EscapeString(owner))
		if !item.Due.IsZero() {
			b.WriteString(" · ")
			b.WriteString(item.Due.Format("2006-01-02"))
		}
		b.WriteString("</li>")
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if n == 0 {
		return `<p class="empty">没有未完成的任务.</p>`, nil
	}
	b.WriteString("</ul>")
	return template.HTML(b.String()), nil
}

// HomeOpenHTML 是当前用户未完成的任务, 最多 5 条, 只查一次.
func (s *Service) HomeOpenHTML(ctx context.Context, actor Actor) (template.HTML, error) {
	if !canUse(actor) {
		return "", ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `
SELECT t.id, t.title, t.status, t.due_on
FROM tasks.tasks t
WHERE t.status IN ('todo', 'doing')
  AND (t.owner_id = $1 OR t.created_by = $1 OR t.id IN (SELECT task_id FROM tasks.participants WHERE person_id = $1))
ORDER BY t.due_on NULLS LAST, t.id
LIMIT $2`, actor.ID, homeLimit)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	items, err := scanHome(rows)
	if err != nil {
		return "", err
	}
	return taskHomeHTML(items, "没有未完成的任务."), nil
}

// HomeDueHTML 是 7 天内到期或已经过期, 且尚未完成的任务, 最多 5 条, 只查一次.
func (s *Service) HomeDueHTML(ctx context.Context, actor Actor) (template.HTML, error) {
	if !canUse(actor) {
		return "", ErrForbidden
	}
	until := dateOnly(time.Now().AddDate(0, 0, 7))
	rows, err := s.pool.Query(ctx, `
SELECT t.id, t.title, t.status, t.due_on
FROM tasks.tasks t
WHERE t.status IN ('todo', 'doing') AND t.due_on IS NOT NULL AND t.due_on <= $2
  AND (t.owner_id = $1 OR t.created_by = $1 OR t.id IN (SELECT task_id FROM tasks.participants WHERE person_id = $1))
ORDER BY t.due_on, t.id
LIMIT $3`, actor.ID, until, homeLimit)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	items, err := scanHome(rows)
	if err != nil {
		return "", err
	}
	return taskHomeHTML(items, "没有即将到期的任务."), nil
}

func dateOnly(t time.Time) time.Time {
	y, m, d := t.In(cst).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

type homeScanner interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanHome(rows homeScanner) ([]homeItem, error) {
	var out []homeItem
	for rows.Next() {
		var item homeItem
		var due *time.Time
		if err := rows.Scan(&item.ID, &item.Title, &item.Status, &due); err != nil {
			return nil, err
		}
		if due != nil {
			item.Due = *due
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func taskHomeHTML(items []homeItem, empty string) template.HTML {
	if len(items) == 0 {
		return template.HTML(`<p class="empty">` + empty + `</p>`)
	}
	var b strings.Builder
	b.WriteString("<ul>")
	for _, item := range items {
		b.WriteString(`<li><a href="/tasks/`)
		b.WriteString(strconv.FormatInt(item.ID, 10))
		b.WriteString(`">`)
		b.WriteString(html.EscapeString(item.Title))
		b.WriteString("</a> ")
		b.WriteString(html.EscapeString(StatusLabel(item.Status)))
		if !item.Due.IsZero() {
			b.WriteString(" ")
			b.WriteString(item.Due.Format("2006-01-02"))
		}
		b.WriteString("</li>")
	}
	b.WriteString("</ul>")
	return template.HTML(b.String())
}
