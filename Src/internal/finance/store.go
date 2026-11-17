package finance

import (
	"context"
	"errors"
	"fmt"
	"html"
	"html/template"
	"io"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/audit"
)

func errNoRows() error { return pgx.ErrNoRows }

// Get 读取一条流水.
func (s *Service) Get(ctx context.Context, actor Actor, id int64) (Entry, error) {
	if !canRead(actor) {
		return Entry{}, ErrForbidden
	}
	row := s.pool.QueryRow(ctx, entrySelect+` WHERE e.id = $1`, id)
	entry, err := scanEntry(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Entry{}, ErrNotFound
	}
	if err != nil {
		return Entry{}, err
	}
	entry.Files, err = s.filesOf(ctx, id)
	return entry, err
}

// List 按月份, 类型, 分类和经手人筛选. 作废的仍列出.
func (s *Service) List(ctx context.Context, actor Actor, filter Filter) ([]Entry, int, error) {
	if !canRead(actor) {
		return nil, 0, ErrForbidden
	}
	where, args, err := listWhere(filter)
	if err != nil {
		return nil, 0, err
	}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM finance.entries e WHERE `+where, args...).Scan(&total); err != nil {
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
	args = append(args, limit, offset)
	rows, err := s.pool.Query(ctx, fmt.Sprintf(entrySelect+` WHERE %s ORDER BY e.happened_on DESC, e.id DESC LIMIT $%d OFFSET $%d`, where, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, entry)
	}
	return out, total, rows.Err()
}

// Categories 返回全部分类.
func (s *Service) Categories(ctx context.Context, actor Actor) ([]Category, error) {
	if !canRead(actor) {
		return nil, ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `SELECT id, name FROM finance.categories ORDER BY name, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Category
	for rows.Next() {
		var item Category
		if err := rows.Scan(&item.ID, &item.Name); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// Actors 返回出现过的经手人.
func (s *Service) Actors(ctx context.Context, actor Actor) ([]PersonRef, error) {
	if !canRead(actor) {
		return nil, ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `
SELECT DISTINCT actor_id, actor_name FROM finance.entries ORDER BY actor_name, actor_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PersonRef
	for rows.Next() {
		var item PersonRef
		if err := rows.Scan(&item.ID, &item.Name); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// Rows 返回未作废的流水, 供统计与手工账核对.
func (s *Service) Rows(ctx context.Context, actor Actor) ([]Row, error) {
	if !canRead(actor) {
		return nil, ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `
SELECT e.kind, e.amount_cents, to_char(e.happened_on, 'YYYY-MM'), c.name
FROM finance.entries e
JOIN finance.categories c ON c.id = e.category_id
WHERE e.voided_at IS NULL
ORDER BY e.happened_on, e.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Row
	for rows.Next() {
		var row Row
		if err := rows.Scan(&row.Kind, &row.Cents, &row.Month, &row.Category); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// Balance 返回未作废流水的手头, 储蓄和笔数.
func (s *Service) Balance(ctx context.Context, actor Actor) (cash, savings int64, n int, err error) {
	if !canRead(actor) {
		return 0, 0, 0, ErrForbidden
	}
	err = s.pool.QueryRow(ctx, `
SELECT count(*),
       coalesce(sum(CASE kind
           WHEN 'in' THEN amount_cents
           WHEN 'out' THEN -amount_cents
           WHEN 'save' THEN -amount_cents
           WHEN 'draw' THEN amount_cents
           ELSE 0 END), 0),
       coalesce(sum(CASE kind
           WHEN 'save' THEN amount_cents
           WHEN 'draw' THEN -amount_cents
           ELSE 0 END), 0)
FROM finance.entries
WHERE voided_at IS NULL`).Scan(&n, &cash, &savings)
	return cash, savings, n, err
}

// Attach 给流水加一张发票或照片.
func (s *Service) Attach(ctx context.Context, actor Actor, id int64, name string, body io.Reader) error {
	if !canWrite(actor) {
		return ErrForbidden
	}
	if s.files == nil {
		return ErrState
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var voided bool
	err = tx.QueryRow(ctx, `SELECT voided_at IS NOT NULL FROM finance.entries WHERE id = $1 FOR UPDATE`, id).Scan(&voided)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if voided {
		return ErrState
	}
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM finance.entry_files WHERE entry_id = $1`, id).Scan(&n); err != nil {
		return err
	}
	if n >= 8 {
		return errInput("附件最多 8 个")
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	meta, err := s.files.Save(ctx, actor.ID, "finance", name, body)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO finance.entry_files (entry_id, file_id) VALUES ($1, $2)`, id, meta.ID)
	return err
}

// CanSeeFile 报告这个人能否下载财务附件.
func (s *Service) CanSeeFile(ctx context.Context, actor Actor, fileID int64) (bool, error) {
	if !canRead(actor) {
		return false, nil
	}
	var ok bool
	err := s.pool.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1 FROM finance.entry_files f
    WHERE f.file_id = $1
)`, fileID).Scan(&ok)
	return ok, err
}

// HomeHTML 是首页上的手头和储蓄. 没有未作废流水时给出空状态.
func (s *Service) HomeHTML(ctx context.Context) (template.HTML, error) {
	var n int
	var cash, savings int64
	err := s.pool.QueryRow(ctx, `
SELECT count(*),
       coalesce(sum(CASE kind
           WHEN 'in' THEN amount_cents
           WHEN 'out' THEN -amount_cents
           WHEN 'save' THEN -amount_cents
           WHEN 'draw' THEN amount_cents
           ELSE 0 END), 0),
       coalesce(sum(CASE kind
           WHEN 'save' THEN amount_cents
           WHEN 'draw' THEN -amount_cents
           ELSE 0 END), 0)
FROM finance.entries
WHERE voided_at IS NULL`).Scan(&n, &cash, &savings)
	if err != nil {
		return "", err
	}
	if n == 0 {
		return `<p class="empty">还没有流水.</p>`, nil
	}
	var b strings.Builder
	b.WriteString(`<p>手头 `)
	b.WriteString(html.EscapeString(yuan(cash)))
	b.WriteString(` 元</p><p>储蓄 `)
	b.WriteString(html.EscapeString(yuan(savings)))
	b.WriteString(` 元</p>`)
	return template.HTML(b.String()), nil
}

func yuan(cents int64) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	return fmt.Sprintf("%s%d.%02d", sign, cents/100, cents%100)
}

const entrySelect = `
SELECT e.id, e.kind, e.amount_cents, e.happened_on, e.category_id, c.name, e.note,
       e.actor_id, e.actor_name, e.voided_at IS NOT NULL, e.void_reason
FROM finance.entries e
JOIN finance.categories c ON c.id = e.category_id`

type scanner interface {
	Scan(dest ...any) error
}

func scanEntry(row scanner) (Entry, error) {
	var entry Entry
	err := row.Scan(
		&entry.ID, &entry.Kind, &entry.Cents, &entry.Date, &entry.CategoryID, &entry.Category, &entry.Note,
		&entry.ActorID, &entry.ActorName, &entry.Voided, &entry.VoidReason,
	)
	return entry, err
}

func (s *Service) filesOf(ctx context.Context, id int64) ([]File, error) {
	rows, err := s.pool.Query(ctx, `
SELECT f.id, f.original_name
FROM finance.entry_files ef
JOIN files.files f ON f.id = ef.file_id
WHERE ef.entry_id = $1
ORDER BY f.id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []File
	for rows.Next() {
		var file File
		if err := rows.Scan(&file.ID, &file.Name); err != nil {
			return nil, err
		}
		out = append(out, file)
	}
	return out, rows.Err()
}

func listWhere(filter Filter) (string, []any, error) {
	where := []string{"TRUE"}
	var args []any
	n := 1
	if filter.Month != "" {
		if len(filter.Month) != 7 || filter.Month[4] != '-' {
			return "", nil, errInput("月份不正确")
		}
		where = append(where, fmt.Sprintf("to_char(e.happened_on, 'YYYY-MM') = $%d", n))
		args = append(args, filter.Month)
		n++
	}
	if filter.Kind != "" {
		if !validKind(filter.Kind) {
			return "", nil, errInput("类型不正确")
		}
		where = append(where, fmt.Sprintf("e.kind = $%d", n))
		args = append(args, filter.Kind)
		n++
	}
	if filter.CategoryID > 0 {
		where = append(where, fmt.Sprintf("e.category_id = $%d", n))
		args = append(args, filter.CategoryID)
		n++
	}
	if filter.ActorID > 0 {
		where = append(where, fmt.Sprintf("e.actor_id = $%d", n))
		args = append(args, filter.ActorID)
	}
	return strings.Join(where, " AND "), args, nil
}

// Export 导出筛选后的流水, 并写入审计.
func (s *Service) Export(ctx context.Context, actor Actor, filter Filter) ([]byte, error) {
	if !canRead(actor) {
		return nil, ErrForbidden
	}
	filter.Limit = 100
	filter.Offset = 0
	var all []Entry
	for {
		items, total, err := s.List(ctx, actor, filter)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		filter.Offset += len(items)
		if len(items) == 0 || filter.Offset >= total || filter.Offset >= 10000 {
			break
		}
	}
	raw, err := encodeCSV(all)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err := audit.Write(ctx, tx, audit.Event{
		ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: "finance.export",
		Entity: "finance", Summary: "导出财务流水",
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return raw, nil
}

func encodeCSV(items []Entry) ([]byte, error) {
	return writeCSV(items)
}
