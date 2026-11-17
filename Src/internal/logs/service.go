package logs

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/inventory"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/people"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/audit"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/files"
)

var cst = time.FixedZone("CST", 8*3600)

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

func (a Actor) stock() inventory.Actor {
	return inventory.Actor{ID: a.ID, Name: a.Name, Role: a.Role, Active: a.Active, IP: a.IP}
}

// Part 是更换的一件元件.
type Part struct {
	ItemID int64
	Code   string
	Name   string
	Qty    int
}

// File 是附图.
type File struct {
	ID   int64
	Name string
}

// Stock 是表单里可选的元件.
type Stock struct {
	ID   int64
	Code string
	Name string
	Qty  int
}

// Entry 是一条日志. Contact 对无权查看的人保持空白.
type Entry struct {
	ID         int64
	Kind       string
	Date       time.Time
	AuthorID   int64
	AuthorName string
	Body       string
	Minutes    *int
	Target     string
	ItemID     int64
	ItemName   string
	Symptom    string
	Action     string
	Result     string
	Status     string
	Source     string
	Contact    string
	Voided     bool
	VoidReason string
	Parts      []Part
	Files      []File
}

// DateText 是记录日期.
func (e Entry) DateText() string {
	if e.Date.IsZero() {
		return ""
	}
	return e.Date.Format("2006-01-02")
}

// Snippet 是列表上的短句.
func (e Entry) Snippet() string {
	text := e.Symptom
	if text == "" {
		text = e.Body
	}
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) <= 40 {
		return text
	}
	return string([]rune(text)[:40])
}

// Draft 是新建或修改时提交的内容.
type Draft struct {
	Kind    string
	Date    *time.Time
	Body    string
	Minutes *int
	Target  string
	ItemID  int64
	Symptom string
	Action  string
	Result  string
	Status  string
	Source  string
	Contact string
	Parts   []Part
}

// Filter 是列表筛选.
type Filter struct {
	Mine   bool
	Kind   string
	Status string
	Query  string
	Offset int
	Limit  int
}

// Service 管理日志和维修记录.
type Service struct {
	pool  *pgxpool.Pool
	files *files.Store
	inv   *inventory.Service
}

// New 创建服务.
func New(pool *pgxpool.Pool, store *files.Store, inv *inventory.Service) *Service {
	return &Service{pool: pool, files: store, inv: inv}
}

// Files 返回文件存储.
func (s *Service) Files() *files.Store { return s.files }

func canUse(actor Actor) bool { return actor.Active }

func canEdit(actor Actor, authorID int64) bool {
	if people.Can(actor.user(), people.ActLogEditAll, people.Resource{}) {
		return true
	}
	if actor.ID != 0 && actor.ID == authorID {
		return people.Can(actor.user(), people.ActLogEditOwn, people.Resource{OwnerID: actor.ID})
	}
	return false
}

func canSeeContact(actor Actor, authorID int64) bool {
	if actor.ID != 0 && actor.ID == authorID {
		return true
	}
	return people.Can(actor.user(), people.ActLogEditAll, people.Resource{})
}

// Create 新建一条日志. 维修记录里的元件在同一事务中领用.
func (s *Service) Create(ctx context.Context, actor Actor, draft Draft) (Entry, error) {
	if !canUse(actor) {
		return Entry{}, ErrForbidden
	}
	clean, err := cleanDraft(draft, true)
	if err != nil {
		return Entry{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Entry{}, err
	}
	defer tx.Rollback(ctx)
	var id int64
	err = tx.QueryRow(ctx, `
INSERT INTO logs.entries (kind, happened_on, author_id, author_name, body, minutes)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id`, clean.Kind, clean.Date, actor.ID, actor.Name, clean.Body, clean.Minutes).Scan(&id)
	if err != nil {
		return Entry{}, err
	}
	if clean.Kind != kindWork {
		if err := insertRepair(ctx, tx, id, clean); err != nil {
			return Entry{}, err
		}
		if err := s.syncParts(ctx, tx, actor, id, nil, clean.Parts); err != nil {
			return Entry{}, err
		}
	}
	if err := audit.Write(ctx, tx, audit.Event{
		ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: "logs.create",
		Entity: "log", EntityID: &id, Summary: "新建" + KindLabel(clean.Kind),
	}); err != nil {
		return Entry{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Entry{}, err
	}
	return s.Get(ctx, actor, id)
}

// Update 修改日志. 元件数量变化会领用或冲销, 失败则整笔回滚.
func (s *Service) Update(ctx context.Context, actor Actor, id int64, draft Draft) (Entry, error) {
	if !canUse(actor) {
		return Entry{}, ErrForbidden
	}
	clean, err := cleanDraft(draft, false)
	if err != nil {
		return Entry{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Entry{}, err
	}
	defer tx.Rollback(ctx)
	old, err := lockEntry(ctx, tx, id)
	if err != nil {
		return Entry{}, err
	}
	if old.Voided {
		return Entry{}, ErrState
	}
	if !canEdit(actor, old.AuthorID) {
		return Entry{}, ErrForbidden
	}
	if clean.Kind != old.Kind {
		return Entry{}, errInput("类型不能修改")
	}
	if _, err := tx.Exec(ctx, `
UPDATE logs.entries SET happened_on = $2, body = $3, minutes = $4, updated_at = now() WHERE id = $1`,
		id, clean.Date, clean.Body, clean.Minutes); err != nil {
		return Entry{}, err
	}
	if clean.Kind != kindWork {
		if _, err := tx.Exec(ctx, `
UPDATE logs.repairs
SET target = $2, item_id = $3, symptom = $4, action = $5, result = $6, status = $7, source = $8, contact = $9
WHERE entry_id = $1`, id, clean.Target, nullID(clean.ItemID), clean.Symptom, clean.Action, clean.Result, clean.Status, clean.Source, clean.Contact); err != nil {
			return Entry{}, err
		}
		if err := s.syncParts(ctx, tx, actor, id, old.Parts, clean.Parts); err != nil {
			return Entry{}, err
		}
	}
	if err := audit.Write(ctx, tx, audit.Event{
		ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: "logs.update",
		Entity: "log", EntityID: &id, Summary: "修改" + KindLabel(clean.Kind),
	}); err != nil {
		return Entry{}, err
	}
	if clean.Kind == kindRepair && old.Status != clean.Status {
		if err := writeStatusAudit(ctx, tx, actor, id, old.Status, clean.Status); err != nil {
			return Entry{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Entry{}, err
	}
	return s.Get(ctx, actor, id)
}

// SetStatus 推进报修单状态, 并写入审计.
func (s *Service) SetStatus(ctx context.Context, actor Actor, id int64, status string) error {
	if !validStatus(status) {
		return errInput("状态不正确")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	old, err := lockEntry(ctx, tx, id)
	if err != nil {
		return err
	}
	if old.Voided || old.Kind != kindRepair {
		return ErrState
	}
	if !canEdit(actor, old.AuthorID) {
		return ErrForbidden
	}
	if old.Status == status {
		return tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `UPDATE logs.repairs SET status = $2 WHERE entry_id = $1`, id, status); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE logs.entries SET updated_at = now() WHERE id = $1`, id); err != nil {
		return err
	}
	if err := writeStatusAudit(ctx, tx, actor, id, old.Status, status); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Void 作废记录. 已领用的元件按原数量冲销.
func (s *Service) Void(ctx context.Context, actor Actor, id int64, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" || utf8.RuneCountInString(reason) > 200 {
		return errInput("请填写作废原因")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	old, err := lockEntry(ctx, tx, id)
	if err != nil {
		return err
	}
	if old.Voided {
		return ErrState
	}
	if !canEdit(actor, old.AuthorID) {
		return ErrForbidden
	}
	if err := s.syncParts(ctx, tx, actor, id, old.Parts, nil); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
UPDATE logs.entries SET voided_at = now(), void_reason = $2, updated_at = now() WHERE id = $1`, id, reason); err != nil {
		return err
	}
	if err := audit.Write(ctx, tx, audit.Event{
		ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: "logs.void",
		Entity: "log", EntityID: &id, Summary: "作废" + KindLabel(old.Kind),
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
