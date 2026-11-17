package docs

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/people"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/audit"
)

// Notice 是首页上的一条公告.
type Notice struct {
	ID     int64
	Body   string
	Author string
	At     time.Time
}

// Notices 返回尚未撤回的公告, 新的在前.
func (s *Service) Notices(ctx context.Context, actor Actor) ([]Notice, error) {
	if !peopleCanRead(actor) {
		return nil, ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `
SELECT id, body, author_name, created_at
FROM docs.notices
WHERE voided_at IS NULL
ORDER BY created_at DESC, id DESC
LIMIT 30`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Notice
	for rows.Next() {
		var notice Notice
		if err := rows.Scan(&notice.ID, &notice.Body, &notice.Author, &notice.At); err != nil {
			return nil, err
		}
		out = append(out, notice)
	}
	return out, rows.Err()
}

// AddNotice 由管理层发布一条公告.
func (s *Service) AddNotice(ctx context.Context, actor Actor, body string) error {
	if !management(actor) {
		return ErrForbidden
	}
	body = strings.TrimSpace(body)
	if body == "" || utf8.RuneCountInString(body) > 400 {
		return ErrBadInput
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id int64
	if err := tx.QueryRow(ctx, `
INSERT INTO docs.notices (body, author_id, author_name)
VALUES ($1, $2, $3)
RETURNING id`, body, actor.ID, actor.Name).Scan(&id); err != nil {
		return err
	}
	if err := audit.Write(ctx, tx, audit.Event{
		ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: "docs.notice",
		Entity: "notice", EntityID: &id, Summary: "发布公告",
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// VoidNotice 撤回一条公告. 原记录留在库里.
func (s *Service) VoidNotice(ctx context.Context, actor Actor, id int64) error {
	if !management(actor) {
		return ErrForbidden
	}
	if id < 1 {
		return ErrBadInput
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
UPDATE docs.notices SET voided_at = now()
WHERE id = $1 AND voided_at IS NULL`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err := audit.Write(ctx, tx, audit.Event{
		ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: "docs.notice.void",
		Entity: "notice", EntityID: &id, Summary: "撤回公告",
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func peopleCanRead(actor Actor) bool {
	return people.Can(actor.user(), people.ActDocsEdit, people.Resource{})
}
