package events

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/audit"
)

// Signup 在截止前报名.
func (s *Service) Signup(ctx context.Context, actor Actor, id int64) error {
	if !join(actor) {
		return ErrForbidden
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	event, err := loadTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if !event.Open(time.Now()) {
		return errState("报名已截止")
	}
	var inserted int64
	err = tx.QueryRow(ctx, `
INSERT INTO events.attendees (event_id, person_id, person_name)
VALUES ($1, $2, $3)
ON CONFLICT DO NOTHING
RETURNING person_id`, id, actor.ID, actor.Name).Scan(&inserted)
	if errors.Is(err, pgx.ErrNoRows) {
		return errState("已经报名")
	}
	if err != nil {
		return err
	}
	if err := audit.Write(ctx, tx, audit.Event{
		ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: "events.signup",
		Entity: "event", EntityID: &id, Summary: actor.Name + " 报名 " + event.Title,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Cancel 在截止前取消尚未签到的报名.
func (s *Service) Cancel(ctx context.Context, actor Actor, id int64) error {
	if !join(actor) {
		return ErrForbidden
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	event, err := loadTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if !event.Open(time.Now()) {
		return errState("报名已截止")
	}
	var checked bool
	err = tx.QueryRow(ctx, `
SELECT checked_in FROM events.attendees WHERE event_id = $1 AND person_id = $2`, id, actor.ID).Scan(&checked)
	if errors.Is(err, pgx.ErrNoRows) {
		return errState("还没有报名")
	}
	if err != nil {
		return err
	}
	if checked {
		return errState("已签到不能取消")
	}
	if _, err := tx.Exec(ctx, `DELETE FROM events.attendees WHERE event_id = $1 AND person_id = $2`, id, actor.ID); err != nil {
		return err
	}
	if err := audit.Write(ctx, tx, audit.Event{
		ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: "events.cancel",
		Entity: "event", EntityID: &id, Summary: actor.Name + " 取消报名 " + event.Title,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Check 由负责人或管理层登记签到.
func (s *Service) Check(ctx context.Context, actor Actor, id, personID int64, on bool) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	event, err := loadTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if !canCheck(actor, event.OwnerID) {
		return ErrForbidden
	}
	var summary string
	if on {
		tag, err := tx.Exec(ctx, `
UPDATE events.attendees
SET checked_in = true, checked_by = $3, checked_name = $4, checked_at = now()
WHERE event_id = $1 AND person_id = $2`, id, personID, actor.ID, actor.Name)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errState("还没有报名")
		}
		summary = "签到"
	} else {
		tag, err := tx.Exec(ctx, `
UPDATE events.attendees
SET checked_in = false, checked_by = NULL, checked_name = '', checked_at = NULL
WHERE event_id = $1 AND person_id = $2`, id, personID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errState("还没有报名")
		}
		summary = "取消签到"
	}
	if err := audit.Write(ctx, tx, audit.Event{
		ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: "events.check",
		Entity: "event", EntityID: &id, Summary: summary,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SaveSummary 保存 Markdown 总结.
func (s *Service) SaveSummary(ctx context.Context, actor Actor, id int64, summary string) error {
	if !edit(actor) {
		return ErrForbidden
	}
	summary = strings.TrimSpace(summary)
	if utf8.RuneCountInString(summary) > 20000 {
		return errInput("总结太长")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := loadTx(ctx, tx, id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
UPDATE events.events SET summary = $2, updated_at = now() WHERE id = $1`, id, summary); err != nil {
		return err
	}
	if err := audit.Write(ctx, tx, audit.Event{
		ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: "events.summary",
		Entity: "event", EntityID: &id, Summary: "更新活动总结",
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Attach 把已经保存的图片或 PDF 挂到活动上.
func (s *Service) Attach(ctx context.Context, actor Actor, id, fileID int64) error {
	if !edit(actor) {
		return ErrForbidden
	}
	if s.files == nil {
		return errInput("不能上传文件")
	}
	meta, file, err := s.files.Open(ctx, fileID)
	if err != nil {
		return err
	}
	file.Close()
	if !strings.HasPrefix(meta.Type, "image/") && meta.Type != "application/pdf" {
		return errInput("只能上传图片或 PDF")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := loadTx(ctx, tx, id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO events.event_files (event_id, file_id) VALUES ($1, $2)
ON CONFLICT DO NOTHING`, id, fileID); err != nil {
		return err
	}
	if err := audit.Write(ctx, tx, audit.Event{
		ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: "events.file",
		Entity: "event", EntityID: &id, Summary: "添加附件 " + meta.Name,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Detach 去掉活动上的附件. 文件本身留给清理任务.
func (s *Service) Detach(ctx context.Context, actor Actor, id, fileID int64) error {
	if !edit(actor) {
		return ErrForbidden
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := loadTx(ctx, tx, id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM events.event_files WHERE event_id = $1 AND file_id = $2`, id, fileID); err != nil {
		return err
	}
	if err := audit.Write(ctx, tx, audit.Event{
		ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: "events.file",
		Entity: "event", EntityID: &id, Summary: "移除附件",
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) attendees(ctx context.Context, id int64) ([]Attendee, error) {
	rows, err := s.pool.Query(ctx, `
SELECT person_id, person_name, signed_up_at, checked_in, checked_name, checked_at
FROM events.attendees WHERE event_id = $1 ORDER BY signed_up_at, person_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Attendee
	for rows.Next() {
		var row Attendee
		var at *time.Time
		if err := rows.Scan(&row.PersonID, &row.Name, &row.SignedUp, &row.Checked, &row.CheckedName, &at); err != nil {
			return nil, err
		}
		if at != nil {
			row.CheckedAt = *at
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Service) attachments(ctx context.Context, id int64) ([]Attachment, error) {
	rows, err := s.pool.Query(ctx, `
SELECT f.id, f.original_name, f.content_type
FROM events.event_files ef
JOIN files.files f ON f.id = ef.file_id
WHERE ef.event_id = $1
ORDER BY f.id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Attachment
	for rows.Next() {
		var row Attachment
		if err := rows.Scan(&row.ID, &row.Name, &row.Type); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
