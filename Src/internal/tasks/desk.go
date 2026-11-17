package tasks

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/people"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/audit"
)

// Spot 是放进日程的一条任务.
type Spot struct {
	ID    int64
	Title string
	At    time.Time
}

func (a Actor) leader() bool {
	return a.Active && a.Role == people.RoleLeader
}

// Desk 按维修组的顺序返回任务. 已完成默认隐藏. 组员只看未接取和自己接取的.
func (s *Service) Desk(ctx context.Context, actor Actor, filter Filter) ([]Task, error) {
	if !canUse(actor) {
		return nil, ErrForbidden
	}
	where := []string{"TRUE"}
	args := []any{actor.ID}
	n := 2
	if !actor.leader() {
		where = append(where, "(t.claimed_by IS NULL OR t.claimed_by = $1)")
	}
	if filter.Kind != "" {
		if !validKind(filter.Kind) {
			return nil, errInput("类型不正确")
		}
		where = append(where, "t.kind = $"+strconv.Itoa(n))
		args = append(args, filter.Kind)
		n++
	}
	if filter.Status != "" {
		if !validStatus(filter.Status) {
			return nil, errInput("状态不正确")
		}
		where = append(where, "t.status = $"+strconv.Itoa(n))
		args = append(args, filter.Status)
		n++
	} else if !filter.ShowDone {
		where = append(where, "t.status NOT IN ('done', 'cancelled')")
	}
	if q := strings.TrimSpace(filter.Query); q != "" {
		where = append(where, `t.title ILIKE $`+strconv.Itoa(n)+` ESCAPE '\'`)
		args = append(args, "%"+escapeLike(q)+"%")
	}
	rows, err := s.pool.Query(ctx, `
SELECT t.id, t.title, t.body, t.kind, t.status, t.priority, t.owner_id, t.owner_name,
       COALESCE(t.contact, ''), COALESCE(t.claimed_by, 0), COALESCE(t.claimed_name, ''), t.created_at
FROM tasks.tasks t
WHERE `+strings.Join(where, " AND ")+`
ORDER BY CASE WHEN t.claimed_by = $1 THEN 0 WHEN t.priority = 'high' THEN 1 WHEN t.claimed_by IS NULL THEN 2 ELSE 3 END,
         t.created_at, t.id
LIMIT 200`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		task, err := scanDesk(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, task)
	}
	return out, rows.Err()
}

// Open 读取一条任务. 组员看不到别人已经接走的.
func (s *Service) Open(ctx context.Context, actor Actor, id int64) (Task, error) {
	if !canUse(actor) {
		return Task{}, ErrForbidden
	}
	row := s.pool.QueryRow(ctx, `
SELECT t.id, t.title, t.body, t.kind, t.status, t.priority, t.owner_id, t.owner_name,
       COALESCE(t.contact, ''), COALESCE(t.claimed_by, 0), COALESCE(t.claimed_name, ''), t.created_at
FROM tasks.tasks t
WHERE t.id = $1
  AND ($2::boolean OR t.claimed_by IS NULL OR t.claimed_by = $3)`, id, actor.leader(), actor.ID)
	task, err := scanDesk(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	if err != nil {
		return Task{}, err
	}
	task.Comments, err = s.comments(ctx, id)
	return task, err
}

// Claim 接取一条还没有人接的任务.
func (s *Service) Claim(ctx context.Context, actor Actor, id int64) error {
	return s.claimTo(ctx, actor, id, actor.ID, actor.Name, false)
}

// Assign 由组长把任务交给某人.
func (s *Service) Assign(ctx context.Context, actor Actor, id, personID int64) error {
	if !actor.leader() {
		return ErrForbidden
	}
	var name string
	err := s.pool.QueryRow(ctx, `SELECT name FROM people.people WHERE id = $1 AND status = 'active'`, personID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return errInput("人员不存在")
	}
	if err != nil {
		return err
	}
	return s.claimTo(ctx, actor, id, personID, name, true)
}

// Finish 确认完成. 接取的人或组长可以.
func (s *Service) Finish(ctx context.Context, actor Actor, id int64) error {
	return s.shift(ctx, actor, id, statusDone, "完成任务")
}

// Release 放弃已接取的任务, 重新放回未接取.
func (s *Service) Release(ctx context.Context, actor Actor, id int64) error {
	if !canUse(actor) {
		return ErrForbidden
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status string
	var claimed *int64
	err = tx.QueryRow(ctx, `SELECT status, claimed_by FROM tasks.tasks WHERE id = $1 FOR UPDATE`, id).Scan(&status, &claimed)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if status == statusDone || status == statusCancelled {
		return errInput("已经结束的任务不能放弃")
	}
	if !actor.leader() && (claimed == nil || *claimed != actor.ID) {
		return ErrForbidden
	}
	_, err = tx.Exec(ctx, `
UPDATE tasks.tasks
SET claimed_by = NULL, claimed_name = '', claimed_at = NULL, status = 'todo',
    owner_id = created_by, owner_name = created_name, updated_at = now()
WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if err := writeStatusAudit(ctx, tx, actor, id, status, statusTodo); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Spots 把任务放到日程上. 有截止日期用截止日期, 否则用提交时间.
func (s *Service) Spots(ctx context.Context, actor Actor, from, to time.Time) ([]Spot, error) {
	if !canUse(actor) {
		return nil, ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `
SELECT id, title, COALESCE(((due_on::timestamp) AT TIME ZONE 'Asia/Shanghai'), created_at)
FROM tasks.tasks
WHERE status <> 'cancelled'
  AND COALESCE(((due_on::timestamp) AT TIME ZONE 'Asia/Shanghai'), created_at) >= $1
  AND COALESCE(((due_on::timestamp) AT TIME ZONE 'Asia/Shanghai'), created_at) < $2
  AND ($3::boolean OR claimed_by IS NULL OR claimed_by = $4)
ORDER BY 3, id
LIMIT 300`, from, to, actor.leader(), actor.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Spot
	for rows.Next() {
		var spot Spot
		if err := rows.Scan(&spot.ID, &spot.Title, &spot.At); err != nil {
			return nil, err
		}
		out = append(out, spot)
	}
	return out, rows.Err()
}

func (s *Service) claimTo(ctx context.Context, actor Actor, id, personID int64, name string, force bool) error {
	if !canUse(actor) {
		return ErrForbidden
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status string
	var claimed *int64
	err = tx.QueryRow(ctx, `SELECT status, claimed_by FROM tasks.tasks WHERE id = $1 FOR UPDATE`, id).Scan(&status, &claimed)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if status == statusDone || status == statusCancelled {
		return errInput("已经结束的任务不能再接")
	}
	if !force && claimed != nil {
		return errInput("已经有人接了")
	}
	_, err = tx.Exec(ctx, `
UPDATE tasks.tasks
SET claimed_by = $2, claimed_name = $3, claimed_at = now(), owner_id = $2, owner_name = $3,
    status = CASE WHEN status = 'todo' THEN 'doing' ELSE status END, updated_at = now()
WHERE id = $1`, id, personID, name)
	if err != nil {
		return err
	}
	next := status
	if status == statusTodo {
		next = statusDoing
	}
	if err := writeStatusAudit(ctx, tx, actor, id, status, next); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) shift(ctx context.Context, actor Actor, id int64, next, summary string) error {
	if !canUse(actor) {
		return ErrForbidden
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status string
	var claimed *int64
	err = tx.QueryRow(ctx, `SELECT status, claimed_by FROM tasks.tasks WHERE id = $1 FOR UPDATE`, id).Scan(&status, &claimed)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if status == statusDone || status == statusCancelled {
		return errInput("任务已经结束")
	}
	if !actor.leader() && (claimed == nil || *claimed != actor.ID) {
		return ErrForbidden
	}
	_, err = tx.Exec(ctx, `UPDATE tasks.tasks SET status = $2, updated_at = now() WHERE id = $1`, id, next)
	if err != nil {
		return err
	}
	if err := audit.Write(ctx, tx, audit.Event{
		ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: "tasks.status",
		Entity: "task", EntityID: &id, Summary: summary,
		Detail: map[string]string{"from": status, "to": next},
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func scanDesk(row scanner) (Task, error) {
	var task Task
	err := row.Scan(
		&task.ID, &task.Title, &task.Body, &task.Kind, &task.Status, &task.Priority, &task.OwnerID, &task.OwnerName,
		&task.Contact, &task.ClaimedID, &task.ClaimedName, &task.Created,
	)
	return task, err
}
