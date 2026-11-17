package tasks

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/people"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/audit"
)

// Actor 是当前操作的人.
type Actor struct {
	ID     int64
	Name   string
	Role   string
	Active bool
	IP     string
}

func (a Actor) user() people.User {
	return people.User{ID: a.ID, Role: a.Role, Active: a.Active}
}

// Person 是负责人或参与人.
type Person struct {
	ID   int64
	Name string
}

// ItemRef 是关联的器材.
type ItemRef struct {
	ID   int64
	Code string
	Name string
}

// Comment 是一条评论.
type Comment struct {
	ID     int64
	Author string
	Body   string
	At     time.Time
}

// Task 是一条任务.
type Task struct {
	ID          int64
	Title       string
	Body        string
	Kind        string
	Status      string
	Priority    string
	OwnerID     int64
	OwnerName   string
	Due         time.Time
	EventID     int64
	EventTitle  string
	CreatedBy   int64
	CreatedName string
	Updated     time.Time
	People      []Person
	Items       []ItemRef
	Comments    []Comment
}

// DueText 是截止日期, 没有则空.
func (t Task) DueText() string {
	if t.Due.IsZero() {
		return ""
	}
	return t.Due.Format("2006-01-02")
}

// Draft 是创建或修改时提交的内容.
type Draft struct {
	Title    string
	Body     string
	Kind     string
	Status   string
	Priority string
	OwnerID  int64
	Due      *time.Time
	EventID  int64
	People   []int64
	Items    []int64
}

// Filter 是列表筛选.
type Filter struct {
	Mine   bool
	Kind   string
	Status string
	Query  string
	Sort   string
	Offset int
	Limit  int
}

// Column 是看板的一列.
type Column struct {
	Status string
	Tasks  []Task
}

// Service 管理任务.
type Service struct {
	pool *pgxpool.Pool
}

// New 创建服务.
func New(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

func canUse(actor Actor) bool {
	return people.Can(actor.user(), people.ActTaskCreate, people.Resource{})
}

func canEdit(actor Actor, ownerID, createdBy int64) bool {
	if people.Can(actor.user(), people.ActTaskEditAll, people.Resource{}) {
		return true
	}
	if actor.ID != 0 && (actor.ID == ownerID || actor.ID == createdBy) {
		return people.Can(actor.user(), people.ActTaskEditOwn, people.Resource{OwnerID: actor.ID})
	}
	return false
}

// CanEdit 报告这个人能否改这条任务.
func CanEdit(actor Actor, task Task) bool {
	return canEdit(actor, task.OwnerID, task.CreatedBy)
}

// People 返回仍在组的人员.
func (s *Service) People(ctx context.Context, actor Actor) ([]Person, error) {
	if !canUse(actor) {
		return nil, ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `SELECT id, name FROM people.people WHERE status = 'active' ORDER BY name, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Person
	for rows.Next() {
		var person Person
		if err := rows.Scan(&person.ID, &person.Name); err != nil {
			return nil, err
		}
		out = append(out, person)
	}
	return out, rows.Err()
}

// Items 返回未作废的器材, 供关联.
func (s *Service) Items(ctx context.Context, actor Actor) ([]ItemRef, error) {
	if !canUse(actor) {
		return nil, ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `
SELECT id, code, name FROM inventory.items WHERE voided_at IS NULL ORDER BY code, id LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ItemRef
	for rows.Next() {
		var item ItemRef
		if err := rows.Scan(&item.ID, &item.Code, &item.Name); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// Create 新建任务. 在组的人都可以建.
func (s *Service) Create(ctx context.Context, actor Actor, draft Draft) (Task, error) {
	if !canUse(actor) {
		return Task{}, ErrForbidden
	}
	clean, err := cleanDraft(draft, true)
	if err != nil {
		return Task{}, err
	}
	if clean.OwnerID == 0 {
		clean.OwnerID = actor.ID
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback(ctx)
	owner, err := personName(ctx, tx, clean.OwnerID)
	if err != nil {
		return Task{}, err
	}
	if err := s.ensureEvent(ctx, tx, clean.EventID); err != nil {
		return Task{}, err
	}
	var id int64
	err = tx.QueryRow(ctx, `
INSERT INTO tasks.tasks (
    title, body, kind, status, priority, owner_id, owner_name, due_on, event_id, created_by, created_name
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING id`,
		clean.Title, clean.Body, clean.Kind, clean.Status, clean.Priority, clean.OwnerID, owner, clean.Due, nullID(clean.EventID), actor.ID, actor.Name,
	).Scan(&id)
	if err != nil {
		return Task{}, err
	}
	if err := s.replaceLinks(ctx, tx, id, clean.People, clean.Items); err != nil {
		return Task{}, err
	}
	if err := audit.Write(ctx, tx, audit.Event{
		ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: "tasks.create",
		Entity: "task", EntityID: &id, Summary: "创建任务 " + clean.Title,
	}); err != nil {
		return Task{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Task{}, err
	}
	return s.Get(ctx, actor, id)
}

// Update 修改任务. 组员只能改自己负责或自己创建的.
func (s *Service) Update(ctx context.Context, actor Actor, id int64, draft Draft) (Task, error) {
	if !canUse(actor) {
		return Task{}, ErrForbidden
	}
	clean, err := cleanDraft(draft, false)
	if err != nil {
		return Task{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Task{}, err
	}
	defer tx.Rollback(ctx)
	old, err := loadTx(ctx, tx, id)
	if err != nil {
		return Task{}, err
	}
	if !canEdit(actor, old.OwnerID, old.CreatedBy) {
		return Task{}, ErrForbidden
	}
	owner, err := personName(ctx, tx, clean.OwnerID)
	if err != nil {
		return Task{}, err
	}
	if err := s.ensureEvent(ctx, tx, clean.EventID); err != nil {
		return Task{}, err
	}
	tag, err := tx.Exec(ctx, `
UPDATE tasks.tasks
SET title = $2, body = $3, kind = $4, status = $5, priority = $6, owner_id = $7, owner_name = $8,
    due_on = $9, event_id = $10, updated_at = now()
WHERE id = $1`,
		id, clean.Title, clean.Body, clean.Kind, clean.Status, clean.Priority, clean.OwnerID, owner, clean.Due, nullID(clean.EventID),
	)
	if err != nil {
		return Task{}, err
	}
	if tag.RowsAffected() == 0 {
		return Task{}, ErrNotFound
	}
	if err := s.replaceLinks(ctx, tx, id, clean.People, clean.Items); err != nil {
		return Task{}, err
	}
	if err := audit.Write(ctx, tx, audit.Event{
		ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: "tasks.update",
		Entity: "task", EntityID: &id, Summary: "修改任务 " + clean.Title,
	}); err != nil {
		return Task{}, err
	}
	if old.Status != clean.Status {
		if err := writeStatusAudit(ctx, tx, actor, id, old.Status, clean.Status); err != nil {
			return Task{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Task{}, err
	}
	return s.Get(ctx, actor, id)
}

// SetStatus 只改状态, 并写入审计.
func (s *Service) SetStatus(ctx context.Context, actor Actor, id int64, status string) error {
	if !validStatus(status) {
		return errInput("状态不正确")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	old, err := loadTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if !canEdit(actor, old.OwnerID, old.CreatedBy) {
		return ErrForbidden
	}
	if old.Status == status {
		return tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `UPDATE tasks.tasks SET status = $2, updated_at = now() WHERE id = $1`, id, status); err != nil {
		return err
	}
	if err := writeStatusAudit(ctx, tx, actor, id, old.Status, status); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Comment 追加一条评论. 在组的人都可以写.
func (s *Service) Comment(ctx context.Context, actor Actor, id int64, body string) error {
	if !canUse(actor) {
		return ErrForbidden
	}
	body = strings.TrimSpace(body)
	if body == "" || utf8.RuneCountInString(body) > 4000 {
		return errInput("评论需要 1 到 4000 个字")
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
INSERT INTO tasks.comments (task_id, author_id, author_name, body) VALUES ($1, $2, $3, $4)`,
		id, actor.ID, actor.Name, body); err != nil {
		return err
	}
	if err := audit.Write(ctx, tx, audit.Event{
		ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: "tasks.comment",
		Entity: "task", EntityID: &id, Summary: "评论任务",
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Get 读取任务, 含参与人, 器材和评论.
func (s *Service) Get(ctx context.Context, actor Actor, id int64) (Task, error) {
	if !canUse(actor) {
		return Task{}, ErrForbidden
	}
	row := s.pool.QueryRow(ctx, `
SELECT t.id, t.title, t.body, t.kind, t.status, t.priority, t.owner_id, t.owner_name, t.due_on,
       t.event_id, COALESCE(e.title, ''), t.created_by, t.created_name, t.updated_at
FROM tasks.tasks t
LEFT JOIN events.events e ON e.id = t.event_id
WHERE t.id = $1`, id)
	task, err := scanFull(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Task{}, ErrNotFound
		}
		return Task{}, err
	}
	task.People, err = s.participants(ctx, id)
	if err != nil {
		return Task{}, err
	}
	task.Items, err = s.itemRefs(ctx, id)
	if err != nil {
		return Task{}, err
	}
	task.Comments, err = s.comments(ctx, id)
	if err != nil {
		return Task{}, err
	}
	return task, nil
}

// List 按筛选分页. 排序只接受截止日期, 更新时间或优先级.
func (s *Service) List(ctx context.Context, actor Actor, filter Filter) ([]Task, int, error) {
	if !canUse(actor) {
		return nil, 0, ErrForbidden
	}
	where, args, err := filterSQL(actor, filter)
	if err != nil {
		return nil, 0, err
	}
	order, err := filterOrder(filter.Sort)
	if err != nil {
		return nil, 0, err
	}
	limit := filter.Limit
	if limit < 1 || limit > 100 {
		limit = 30
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM tasks.tasks t WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, offset)
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
SELECT t.id, t.title, t.kind, t.status, t.priority, t.owner_id, t.owner_name, t.due_on,
       t.event_id, COALESCE(e.title, ''), t.created_by, t.created_name, t.updated_at
FROM tasks.tasks t
LEFT JOIN events.events e ON e.id = t.event_id
WHERE %s
ORDER BY %s
LIMIT $%d OFFSET $%d`, where, order, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		task, err := scanLight(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, task)
	}
	return out, total, rows.Err()
}

// Board 按状态分列, 每列最多 40 条.
func (s *Service) Board(ctx context.Context, actor Actor) ([]Column, error) {
	if !canUse(actor) {
		return nil, ErrForbidden
	}
	var out []Column
	for _, status := range statuses {
		rows, err := s.pool.Query(ctx, `
SELECT t.id, t.title, t.kind, t.status, t.priority, t.owner_id, t.owner_name, t.due_on,
       t.event_id, COALESCE(e.title, ''), t.created_by, t.created_name, t.updated_at
FROM tasks.tasks t
LEFT JOIN events.events e ON e.id = t.event_id
WHERE t.status = $1
ORDER BY CASE t.priority WHEN 'high' THEN 0 WHEN 'normal' THEN 1 ELSE 2 END, t.due_on NULLS LAST, t.id
LIMIT 40`, status)
		if err != nil {
			return nil, err
		}
		var tasks []Task
		for rows.Next() {
			task, err := scanLight(rows)
			if err != nil {
				rows.Close()
				return nil, err
			}
			tasks = append(tasks, task)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
		out = append(out, Column{Status: status, Tasks: tasks})
	}
	return out, nil
}

func filterSQL(actor Actor, filter Filter) (string, []any, error) {
	where := []string{"TRUE"}
	var args []any
	n := 1
	if filter.Mine {
		where = append(where, fmt.Sprintf(`(t.owner_id = $%d OR t.created_by = $%d OR t.id IN (SELECT task_id FROM tasks.participants WHERE person_id = $%d))`, n, n, n))
		args = append(args, actor.ID)
		n++
	}
	if filter.Kind != "" {
		if !validKind(filter.Kind) {
			return "", nil, errInput("类型不正确")
		}
		where = append(where, fmt.Sprintf("t.kind = $%d", n))
		args = append(args, filter.Kind)
		n++
	}
	if filter.Status != "" {
		if !validStatus(filter.Status) {
			return "", nil, errInput("状态不正确")
		}
		where = append(where, fmt.Sprintf("t.status = $%d", n))
		args = append(args, filter.Status)
		n++
	}
	if q := strings.TrimSpace(filter.Query); q != "" {
		where = append(where, fmt.Sprintf(`t.title ILIKE $%d ESCAPE '\'`, n))
		args = append(args, "%"+escapeLike(q)+"%")
	}
	return strings.Join(where, " AND "), args, nil
}

func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func filterOrder(sort string) (string, error) {
	switch sort {
	case "", "due":
		return "t.due_on NULLS LAST, t.id", nil
	case "updated":
		return "t.updated_at DESC, t.id DESC", nil
	case "priority":
		return "CASE t.priority WHEN 'high' THEN 0 WHEN 'normal' THEN 1 ELSE 2 END, t.due_on NULLS LAST, t.id", nil
	default:
		return "", errInput("排序不正确")
	}
}

func cleanDraft(draft Draft, creating bool) (Draft, error) {
	draft.Title = strings.TrimSpace(draft.Title)
	draft.Body = strings.TrimSpace(draft.Body)
	draft.Kind = strings.TrimSpace(draft.Kind)
	draft.People = uniqueIDs(draft.People)
	draft.Items = uniqueIDs(draft.Items)
	if draft.Title == "" || utf8.RuneCountInString(draft.Title) > 80 {
		return Draft{}, errInput("标题需要 1 到 80 个字")
	}
	if utf8.RuneCountInString(draft.Body) > 8000 {
		return Draft{}, errInput("说明太长")
	}
	if !validKind(draft.Kind) {
		return Draft{}, errInput("类型不正确")
	}
	if creating && draft.Status == "" {
		draft.Status = statusTodo
	}
	if !validStatus(draft.Status) {
		return Draft{}, errInput("状态不正确")
	}
	if draft.Priority == "" {
		draft.Priority = priorityNormal
	}
	if !validPriority(draft.Priority) {
		return Draft{}, errInput("优先级不正确")
	}
	return draft, nil
}

func uniqueIDs(ids []int64) []int64 {
	seen := map[int64]bool{}
	var out []int64
	for _, id := range ids {
		if id < 1 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

func personName(ctx context.Context, tx pgx.Tx, id int64) (string, error) {
	var name string
	err := tx.QueryRow(ctx, `SELECT name FROM people.people WHERE id = $1 AND status = 'active'`, id).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errInput("负责人不存在")
	}
	return name, err
}

func (s *Service) ensureEvent(ctx context.Context, tx pgx.Tx, id int64) error {
	if id == 0 {
		return nil
	}
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM events.events WHERE id = $1)`, id).Scan(&ok)
	if err != nil {
		return err
	}
	if !ok {
		return errInput("活动不存在")
	}
	return nil
}

func (s *Service) replaceLinks(ctx context.Context, tx pgx.Tx, id int64, peopleIDs, itemIDs []int64) error {
	names, err := lookupNames(ctx, tx, `SELECT id, name FROM people.people WHERE id = ANY($1::bigint[]) AND status = 'active'`, peopleIDs)
	if err != nil {
		return err
	}
	if len(names) != len(peopleIDs) {
		return errInput("参与人不存在")
	}
	items, err := lookupNames(ctx, tx, `SELECT id, code FROM inventory.items WHERE id = ANY($1::bigint[]) AND voided_at IS NULL`, itemIDs)
	if err != nil {
		return err
	}
	if len(items) != len(itemIDs) {
		return errInput("器材不存在")
	}
	if _, err := tx.Exec(ctx, `DELETE FROM tasks.participants WHERE task_id = $1`, id); err != nil {
		return err
	}
	for _, personID := range peopleIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO tasks.participants (task_id, person_id, person_name) VALUES ($1, $2, $3)`, id, personID, names[personID]); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM tasks.task_items WHERE task_id = $1`, id); err != nil {
		return err
	}
	for _, itemID := range itemIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO tasks.task_items (task_id, item_id) VALUES ($1, $2)`, id, itemID); err != nil {
			return err
		}
	}
	return nil
}

func lookupNames(ctx context.Context, tx pgx.Tx, query string, ids []int64) (map[int64]string, error) {
	out := map[int64]string{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, query, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}

func writeStatusAudit(ctx context.Context, tx pgx.Tx, actor Actor, id int64, from, to string) error {
	return audit.Write(ctx, tx, audit.Event{
		ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: "tasks.status",
		Entity: "task", EntityID: &id,
		Summary: "把状态从 " + StatusLabel(from) + " 改成 " + StatusLabel(to),
		Detail:  map[string]string{"from": from, "to": to},
	})
}

func loadTx(ctx context.Context, tx pgx.Tx, id int64) (Task, error) {
	row := tx.QueryRow(ctx, `SELECT id, status, owner_id, created_by FROM tasks.tasks WHERE id = $1 FOR UPDATE`, id)
	var task Task
	err := row.Scan(&task.ID, &task.Status, &task.OwnerID, &task.CreatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return Task{}, ErrNotFound
	}
	return task, err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanLight(row scanner) (Task, error) {
	var task Task
	var due *time.Time
	var eventID *int64
	err := row.Scan(
		&task.ID, &task.Title, &task.Kind, &task.Status, &task.Priority, &task.OwnerID, &task.OwnerName,
		&due, &eventID, &task.EventTitle, &task.CreatedBy, &task.CreatedName, &task.Updated,
	)
	if due != nil {
		task.Due = *due
	}
	if eventID != nil {
		task.EventID = *eventID
	}
	return task, err
}

func scanFull(row scanner) (Task, error) {
	var task Task
	var due *time.Time
	var eventID *int64
	err := row.Scan(
		&task.ID, &task.Title, &task.Body, &task.Kind, &task.Status, &task.Priority, &task.OwnerID, &task.OwnerName,
		&due, &eventID, &task.EventTitle, &task.CreatedBy, &task.CreatedName, &task.Updated,
	)
	if due != nil {
		task.Due = *due
	}
	if eventID != nil {
		task.EventID = *eventID
	}
	return task, err
}

func (s *Service) participants(ctx context.Context, id int64) ([]Person, error) {
	rows, err := s.pool.Query(ctx, `SELECT person_id, person_name FROM tasks.participants WHERE task_id = $1 ORDER BY person_name, person_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Person
	for rows.Next() {
		var person Person
		if err := rows.Scan(&person.ID, &person.Name); err != nil {
			return nil, err
		}
		out = append(out, person)
	}
	return out, rows.Err()
}

func (s *Service) itemRefs(ctx context.Context, id int64) ([]ItemRef, error) {
	rows, err := s.pool.Query(ctx, `
SELECT i.id, i.code, i.name
FROM tasks.task_items ti
JOIN inventory.items i ON i.id = ti.item_id
WHERE ti.task_id = $1
ORDER BY i.code, i.id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ItemRef
	for rows.Next() {
		var item ItemRef
		if err := rows.Scan(&item.ID, &item.Code, &item.Name); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) comments(ctx context.Context, id int64) ([]Comment, error) {
	rows, err := s.pool.Query(ctx, `
SELECT id, author_name, body, created_at FROM tasks.comments WHERE task_id = $1 ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Comment
	for rows.Next() {
		var comment Comment
		if err := rows.Scan(&comment.ID, &comment.Author, &comment.Body, &comment.At); err != nil {
			return nil, err
		}
		out = append(out, comment)
	}
	return out, rows.Err()
}
