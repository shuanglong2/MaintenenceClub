package finance

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/people"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/audit"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/files"
)

var cst = time.FixedZone("CST", 8*3600)

// Actor 是当前操作的人.
type Actor struct {
	ID      int64
	Name    string
	Role    string
	Finance bool
	Active  bool
	IP      string
}

func (a Actor) user() people.User {
	return people.User{ID: a.ID, Role: a.Role, Finance: a.Finance, Active: a.Active}
}

// File 是发票或照片.
type File struct {
	ID   int64
	Name string
}

// Category 是可配置的分类.
type Category struct {
	ID   int64
	Name string
}

// PersonRef 是筛选用的经手人.
type PersonRef struct {
	ID   int64
	Name string
}

// Entry 是一条财务流水.
type Entry struct {
	ID         int64
	Kind       string
	Cents      int64
	Date       time.Time
	CategoryID int64
	Category   string
	Note       string
	ActorID    int64
	ActorName  string
	Voided     bool
	VoidReason string
	Files      []File
}

// DateText 是记账日期.
func (e Entry) DateText() string {
	if e.Date.IsZero() {
		return ""
	}
	return e.Date.Format("2006-01-02")
}

// Draft 是新记的一笔.
type Draft struct {
	Kind       string
	Cents      int64
	Date       *time.Time
	CategoryID int64
	Note       string
}

// Filter 是列表和导出的筛选.
type Filter struct {
	Month      string
	Kind       string
	CategoryID int64
	ActorID    int64
	Offset     int
	Limit      int
}

// Service 管理财务流水.
type Service struct {
	pool  *pgxpool.Pool
	files *files.Store
}

// New 创建服务.
func New(pool *pgxpool.Pool, store *files.Store) *Service {
	return &Service{pool: pool, files: store}
}

// Files 返回文件存储.
func (s *Service) Files() *files.Store { return s.files }

func canRead(actor Actor) bool {
	return people.Can(actor.user(), people.ActFinanceRead, people.Resource{})
}

func canWrite(actor Actor) bool {
	return people.Can(actor.user(), people.ActFinanceWrite, people.Resource{})
}

// CanWrite 表示这个人可以记流水和作废.
func CanWrite(actor Actor) bool { return canWrite(actor) }

// Create 记一笔流水. 记错后作废, 更正再记一笔.
func (s *Service) Create(ctx context.Context, actor Actor, draft Draft) (Entry, error) {
	if !canWrite(actor) {
		return Entry{}, ErrForbidden
	}
	clean, err := cleanDraft(draft)
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
INSERT INTO finance.entries (kind, amount_cents, happened_on, category_id, note, actor_id, actor_name)
SELECT $1, $2, $3, c.id, $4, $5, $6
FROM finance.categories c
WHERE c.id = $7
RETURNING finance.entries.id`,
		clean.Kind, clean.Cents, clean.Date, clean.Note, actor.ID, actor.Name, clean.CategoryID).Scan(&id)
	if errors.Is(err, errNoRows()) {
		return Entry{}, errInput("分类不存在")
	}
	if err != nil {
		return Entry{}, err
	}
	if err := audit.Write(ctx, tx, audit.Event{
		ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: "finance.create",
		Entity: "finance", EntityID: &id, Summary: "记了一笔" + KindLabel(clean.Kind),
	}); err != nil {
		return Entry{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Entry{}, err
	}
	return s.Get(ctx, actor, id)
}

// Void 作废一笔流水. 原记录保留, 不再计入余额.
func (s *Service) Void(ctx context.Context, actor Actor, id int64, reason string) error {
	if !canWrite(actor) {
		return ErrForbidden
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || utf8.RuneCountInString(reason) > 200 {
		return errInput("请填写作废原因")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
UPDATE finance.entries
SET voided_at = now(), voided_by = $2, void_reason = $3
WHERE id = $1 AND voided_at IS NULL`, id, actor.ID, reason)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT true FROM finance.entries WHERE id = $1`, id).Scan(&exists); err != nil {
			return ErrNotFound
		}
		return ErrState
	}
	if err := audit.Write(ctx, tx, audit.Event{
		ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: "finance.void",
		Entity: "finance", EntityID: &id, Summary: "作废了一笔流水",
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// AddCategory 增加一个分类.
func (s *Service) AddCategory(ctx context.Context, actor Actor, name string) error {
	if !canWrite(actor) {
		return ErrForbidden
	}
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 40 {
		return errInput("分类名为 1 到 40 个字")
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO finance.categories (name) VALUES ($1)`, name)
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return errInput("分类已存在")
	}
	return err
}

func cleanDraft(draft Draft) (Draft, error) {
	draft.Kind = strings.TrimSpace(draft.Kind)
	draft.Note = strings.TrimSpace(draft.Note)
	if !validKind(draft.Kind) {
		return Draft{}, errInput("类型不正确")
	}
	if draft.Cents <= 0 {
		return Draft{}, errInput("金额要大于 0")
	}
	if draft.CategoryID < 1 {
		return Draft{}, errInput("请选择分类")
	}
	if utf8.RuneCountInString(draft.Note) > 2000 {
		return Draft{}, errInput("备注太长")
	}
	if draft.Date == nil || draft.Date.IsZero() {
		day := dateOnly(time.Now())
		draft.Date = &day
	}
	return draft, nil
}

func dateOnly(t time.Time) time.Time {
	y, m, d := t.In(cst).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
