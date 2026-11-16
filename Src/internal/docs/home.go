package docs

import (
	"context"
	"html"
	"html/template"
	"strconv"
	"strings"
	"time"
)

const homeLimit = 5

// HomeRecentHTML 是最近修改的文档, 组员看不到仅管理层的页面. 最多 5 条, 只查一次.
func (s *Service) HomeRecentHTML(ctx context.Context, actor Actor) (template.HTML, error) {
	if !canSee(actor, Page{}) {
		return "", ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `
SELECT id, title, updated_at
FROM docs.pages
WHERE voided_at IS NULL AND ($1 OR visibility = 'all')
ORDER BY updated_at DESC, id DESC
LIMIT $2`, management(actor), homeLimit)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var b strings.Builder
	n := 0
	for rows.Next() {
		var id int64
		var title string
		var updated time.Time
		if err := rows.Scan(&id, &title, &updated); err != nil {
			return "", err
		}
		if n == 0 {
			b.WriteString("<ul>")
		}
		n++
		b.WriteString(`<li><a href="/docs/p/`)
		b.WriteString(strconv.FormatInt(id, 10))
		b.WriteString(`">`)
		b.WriteString(html.EscapeString(title))
		b.WriteString("</a> ")
		b.WriteString(updated.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04"))
		b.WriteString("</li>")
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if n == 0 {
		return `<p class="empty">没有最近修改的文档.</p>`, nil
	}
	b.WriteString("</ul>")
	return template.HTML(b.String()), nil
}
