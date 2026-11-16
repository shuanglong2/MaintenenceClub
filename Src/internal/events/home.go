package events

import (
	"context"
	"html"
	"html/template"
	"strconv"
	"strings"
	"time"
)

const homeLimit = 5

// HomeUpcomingHTML 是 14 天内尚未结束的活动, 最多 5 条, 只查一次.
func (s *Service) HomeUpcomingHTML(ctx context.Context, actor Actor) (template.HTML, error) {
	if !join(actor) {
		return "", ErrForbidden
	}
	now := time.Now()
	rows, err := s.pool.Query(ctx, `
SELECT id, title, starts_at, ends_at, all_day
FROM events.events
WHERE status = 'planned' AND ends_at > $1 AND starts_at < $2
ORDER BY starts_at, id
LIMIT $3`, now, now.AddDate(0, 0, 14), homeLimit)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var b strings.Builder
	n := 0
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.ID, &event.Title, &event.Starts, &event.Ends, &event.AllDay); err != nil {
			return "", err
		}
		if n == 0 {
			b.WriteString("<ul>")
		}
		n++
		b.WriteString(`<li><a href="/events/`)
		b.WriteString(strconv.FormatInt(event.ID, 10))
		b.WriteString(`">`)
		b.WriteString(html.EscapeString(event.Title))
		b.WriteString("</a> ")
		b.WriteString(html.EscapeString(WhenLabel(event)))
		b.WriteString("</li>")
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if n == 0 {
		return `<p class="empty">没有近期活动.</p>`, nil
	}
	b.WriteString("</ul>")
	return template.HTML(b.String()), nil
}
