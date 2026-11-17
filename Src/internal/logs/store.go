package logs

import (
	"context"
	"errors"
	"fmt"
	"html"
	"html/template"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/inventory"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/audit"
)

// Get 读取一条日志.
func (s *Service) Get(ctx context.Context, actor Actor, id int64) (Entry, error) {
	if !canUse(actor) {
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
	entry.Parts, err = s.parts(ctx, id)
	if err != nil {
		return Entry{}, err
	}
	entry.Files, err = s.filesOf(ctx, id)
	if err != nil {
		return Entry{}, err
	}
	if !canSeeContact(actor, entry.AuthorID) {
		entry.Contact = ""
	}
	return entry, nil
}

// List 按类型, 状态, 我和关键字筛选.
func (s *Service) List(ctx context.Context, actor Actor, filter Filter) ([]Entry, int, error) {
	if !canUse(actor) {
		return nil, 0, ErrForbidden
	}
	where, args, err := listWhere(actor, filter)
	if err != nil {
		return nil, 0, err
	}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM logs.entries e LEFT JOIN logs.repairs r ON r.entry_id = e.id WHERE `+where, args...).Scan(&total); err != nil {
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
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
SELECT e.id, e.kind, e.happened_on, e.author_id, e.author_name, e.body, e.minutes,
       coalesce(r.target, ''), coalesce(r.item_id, 0), coalesce(i.name, ''),
       coalesce(r.symptom, ''), coalesce(r.action, ''), coalesce(r.result, ''), coalesce(r.status, ''),
       coalesce(r.source, ''), ''::text, e.voided_at IS NOT NULL, e.void_reason
FROM logs.entries e
LEFT JOIN logs.repairs r ON r.entry_id = e.id
LEFT JOIN inventory.items i ON i.id = r.item_id
WHERE %s
ORDER BY e.happened_on DESC, e.id DESC
LIMIT $%d OFFSET $%d`, where, len(args)-1, len(args)), args...)
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

// Stock 返回可当作元件领用的物品.
func (s *Service) Stock(ctx context.Context, actor Actor) ([]Stock, error) {
	if !canUse(actor) {
		return nil, ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `
SELECT i.id, i.code, i.name, coalesce(sum(m.qty), 0)::int
FROM inventory.items i
LEFT JOIN inventory.movements m ON m.item_id = i.id
WHERE i.voided_at IS NULL AND NOT i.tracked
GROUP BY i.id
ORDER BY i.code, i.id
LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Stock
	for rows.Next() {
		var item Stock
		if err := rows.Scan(&item.ID, &item.Code, &item.Name, &item.Qty); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// Attach 给记录加一张图或一个附件.
func (s *Service) Attach(ctx context.Context, actor Actor, id int64, name string, body io.Reader) error {
	if s.files == nil {
		return ErrState
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
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM logs.entry_files WHERE entry_id = $1`, id).Scan(&n); err != nil {
		return err
	}
	if n >= 8 {
		return errInput("附图最多 8 个")
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	meta, err := s.files.Save(ctx, actor.ID, "logs", name, body)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO logs.entry_files (entry_id, file_id) VALUES ($1, $2)`, id, meta.ID)
	return err
}

// CanSeeFile 报告这个人能否下载日志附图.
func (s *Service) CanSeeFile(ctx context.Context, actor Actor, fileID int64) (bool, error) {
	if !canUse(actor) {
		return false, nil
	}
	var ok bool
	err := s.pool.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1 FROM logs.entry_files f
    JOIN logs.entries e ON e.id = f.entry_id
    WHERE f.file_id = $1 AND e.voided_at IS NULL
)`, fileID).Scan(&ok)
	return ok, err
}

// ItemHistoryHTML 是某件器材上的维修保养, 不包含联系方式.
func (s *Service) ItemHistoryHTML(ctx context.Context, itemID int64) (template.HTML, error) {
	rows, err := s.pool.Query(ctx, `
SELECT e.id, e.kind, e.happened_on, e.author_name, coalesce(r.symptom, ''), coalesce(r.status, '')
FROM logs.entries e
LEFT JOIN logs.repairs r ON r.entry_id = e.id
WHERE e.voided_at IS NULL AND e.kind IN ('repair', 'maintain')
  AND (r.item_id = $1 OR e.id IN (SELECT entry_id FROM logs.repair_parts WHERE item_id = $1))
ORDER BY e.happened_on DESC, e.id DESC
LIMIT 20`, itemID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var b strings.Builder
	n := 0
	for rows.Next() {
		var id int64
		var kind, author, symptom, status string
		var day time.Time
		if err := rows.Scan(&id, &kind, &day, &author, &symptom, &status); err != nil {
			return "", err
		}
		if n == 0 {
			b.WriteString("<ul>")
		}
		n++
		b.WriteString(`<li><a href="/logs/`)
		b.WriteString(strconv.FormatInt(id, 10))
		b.WriteString(`">`)
		b.WriteString(html.EscapeString(KindLabel(kind)))
		if symptom != "" {
			b.WriteString(" ")
			b.WriteString(html.EscapeString(symptom))
		}
		b.WriteString("</a> ")
		b.WriteString(day.Format("2006-01-02"))
		b.WriteString(" ")
		b.WriteString(html.EscapeString(author))
		if status != "" {
			b.WriteString(" ")
			b.WriteString(html.EscapeString(StatusLabel(status)))
		}
		b.WriteString("</li>")
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if n == 0 {
		return `<p class="empty">还没有维修保养记录.</p>`, nil
	}
	b.WriteString("</ul>")
	return template.HTML(b.String()), nil
}

func (s *Service) syncParts(ctx context.Context, tx pgx.Tx, actor Actor, id int64, oldParts, next []Part) error {
	if s.inv == nil && (len(oldParts) > 0 || len(next) > 0) {
		return ErrState
	}
	oldMap := partMap(oldParts)
	newMap := partMap(next)
	seen := map[int64]bool{}
	for itemID := range oldMap {
		seen[itemID] = true
	}
	for itemID := range newMap {
		seen[itemID] = true
	}
	for itemID := range seen {
		delta := newMap[itemID] - oldMap[itemID]
		if delta == 0 {
			continue
		}
		in := inventory.MoveInput{ItemID: itemID, RefType: "repair", RefID: id}
		if delta > 0 {
			in.Kind = "use"
			in.Qty = delta
			in.Note = "维修领用"
		} else {
			in.Kind = "in"
			in.Qty = -delta
			in.Note = "冲销维修领用"
		}
		if err := s.inv.Move(ctx, tx, actor.stock(), in); err != nil {
			if errors.Is(err, inventory.ErrStock) {
				return errInput("库存不足")
			}
			if errors.Is(err, inventory.ErrNotFound) {
				return errInput("器材不存在")
			}
			if errors.Is(err, inventory.ErrBadInput) || errors.Is(err, inventory.ErrState) {
				return errInput("这件器材不能当元件领用")
			}
			return err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM logs.repair_parts WHERE entry_id = $1`, id); err != nil {
		return err
	}
	for _, part := range next {
		if _, err := tx.Exec(ctx, `INSERT INTO logs.repair_parts (entry_id, item_id, qty) VALUES ($1, $2, $3)`, id, part.ItemID, part.Qty); err != nil {
			return err
		}
	}
	return nil
}

func partMap(parts []Part) map[int64]int {
	out := map[int64]int{}
	for _, part := range parts {
		if part.ItemID > 0 && part.Qty > 0 {
			out[part.ItemID] += part.Qty
		}
	}
	return out
}

func insertRepair(ctx context.Context, tx pgx.Tx, id int64, draft Draft) error {
	_, err := tx.Exec(ctx, `
INSERT INTO logs.repairs (entry_id, target, item_id, symptom, action, result, status, source, contact)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		id, draft.Target, nullID(draft.ItemID), draft.Symptom, draft.Action, draft.Result, draft.Status, draft.Source, draft.Contact)
	return err
}

func writeStatusAudit(ctx context.Context, tx pgx.Tx, actor Actor, id int64, from, to string) error {
	return audit.Write(ctx, tx, audit.Event{
		ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: "logs.status",
		Entity: "log", EntityID: &id,
		Summary: "把状态从 " + StatusLabel(from) + " 改成 " + StatusLabel(to),
		Detail:  map[string]string{"from": from, "to": to},
	})
}

const entrySelect = `
SELECT e.id, e.kind, e.happened_on, e.author_id, e.author_name, e.body, e.minutes,
       coalesce(r.target, ''), coalesce(r.item_id, 0), coalesce(i.name, ''),
       coalesce(r.symptom, ''), coalesce(r.action, ''), coalesce(r.result, ''), coalesce(r.status, ''),
       coalesce(r.source, ''), coalesce(r.contact, ''), e.voided_at IS NOT NULL, e.void_reason
FROM logs.entries e
LEFT JOIN logs.repairs r ON r.entry_id = e.id
LEFT JOIN inventory.items i ON i.id = r.item_id`

func lockEntry(ctx context.Context, tx pgx.Tx, id int64) (Entry, error) {
	var found int64
	err := tx.QueryRow(ctx, `SELECT id FROM logs.entries WHERE id = $1 FOR UPDATE`, id).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return Entry{}, ErrNotFound
	}
	if err != nil {
		return Entry{}, err
	}
	row := tx.QueryRow(ctx, entrySelect+` WHERE e.id = $1`, id)
	entry, err := scanEntry(row)
	if err != nil {
		return Entry{}, err
	}
	entry.Parts, err = partsTx(ctx, tx, id)
	return entry, err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanEntry(row scanner) (Entry, error) {
	var entry Entry
	var minutes *int
	err := row.Scan(
		&entry.ID, &entry.Kind, &entry.Date, &entry.AuthorID, &entry.AuthorName, &entry.Body, &minutes,
		&entry.Target, &entry.ItemID, &entry.ItemName, &entry.Symptom, &entry.Action, &entry.Result, &entry.Status,
		&entry.Source, &entry.Contact, &entry.Voided, &entry.VoidReason,
	)
	entry.Minutes = minutes
	return entry, err
}

func (s *Service) parts(ctx context.Context, id int64) ([]Part, error) {
	rows, err := s.pool.Query(ctx, partSelect, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectParts(rows)
}

func partsTx(ctx context.Context, tx pgx.Tx, id int64) ([]Part, error) {
	rows, err := tx.Query(ctx, partSelect, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectParts(rows)
}

const partSelect = `
SELECT p.item_id, i.code, i.name, p.qty
FROM logs.repair_parts p
JOIN inventory.items i ON i.id = p.item_id
WHERE p.entry_id = $1
ORDER BY i.code, p.item_id`

type rowScan interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func collectParts(rows rowScan) ([]Part, error) {
	var out []Part
	for rows.Next() {
		var part Part
		if err := rows.Scan(&part.ItemID, &part.Code, &part.Name, &part.Qty); err != nil {
			return nil, err
		}
		out = append(out, part)
	}
	return out, rows.Err()
}

func (s *Service) filesOf(ctx context.Context, id int64) ([]File, error) {
	rows, err := s.pool.Query(ctx, `
SELECT f.id, f.original_name
FROM logs.entry_files ef
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

func listWhere(actor Actor, filter Filter) (string, []any, error) {
	where := []string{"e.voided_at IS NULL"}
	var args []any
	n := 1
	if filter.Mine {
		where = append(where, fmt.Sprintf("e.author_id = $%d", n))
		args = append(args, actor.ID)
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
	if filter.Status != "" {
		if !validStatus(filter.Status) {
			return "", nil, errInput("状态不正确")
		}
		where = append(where, fmt.Sprintf("r.status = $%d", n))
		args = append(args, filter.Status)
		n++
	}
	if q := strings.TrimSpace(filter.Query); q != "" {
		if utf8.RuneCountInString(q) > 40 {
			return "", nil, errInput("搜索词太长")
		}
		where = append(where, fmt.Sprintf(`(e.body ILIKE $%d ESCAPE '\' OR r.symptom ILIKE $%d ESCAPE '\' OR r.action ILIKE $%d ESCAPE '\' OR r.result ILIKE $%d ESCAPE '\' OR r.source ILIKE $%d ESCAPE '\')`, n, n, n, n, n))
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

func cleanDraft(draft Draft, _ bool) (Draft, error) {
	draft.Kind = strings.TrimSpace(draft.Kind)
	draft.Body = strings.TrimSpace(draft.Body)
	draft.Target = strings.TrimSpace(draft.Target)
	draft.Symptom = strings.TrimSpace(draft.Symptom)
	draft.Action = strings.TrimSpace(draft.Action)
	draft.Result = strings.TrimSpace(draft.Result)
	draft.Status = strings.TrimSpace(draft.Status)
	draft.Source = strings.TrimSpace(draft.Source)
	draft.Contact = strings.TrimSpace(draft.Contact)
	if !validKind(draft.Kind) {
		return Draft{}, errInput("类型不正确")
	}
	if draft.Date == nil || draft.Date.IsZero() {
		day := dateOnly(time.Now())
		draft.Date = &day
	}
	if utf8.RuneCountInString(draft.Body) > 8000 {
		return Draft{}, errInput("内容太长")
	}
	if draft.Minutes != nil && (*draft.Minutes < 0 || *draft.Minutes > 1440) {
		return Draft{}, errInput("用时需要在 0 到 1440 分钟之间")
	}
	if draft.Kind == kindWork {
		if draft.Body == "" {
			return Draft{}, errInput("请填写内容")
		}
		draft.Parts = nil
		draft.Contact = ""
		return draft, nil
	}
	if !validTarget(draft.Target) {
		return Draft{}, errInput("对象不正确")
	}
	if draft.Target == "item" && draft.ItemID < 1 {
		return Draft{}, errInput("请选择器材")
	}
	if draft.Target != "item" {
		draft.ItemID = 0
	}
	if draft.Symptom == "" || utf8.RuneCountInString(draft.Symptom) > 2000 {
		return Draft{}, errInput("现象需要 1 到 2000 个字")
	}
	if utf8.RuneCountInString(draft.Action) > 2000 || utf8.RuneCountInString(draft.Result) > 2000 {
		return Draft{}, errInput("处理或结果太长")
	}
	if utf8.RuneCountInString(draft.Source) > 80 || utf8.RuneCountInString(draft.Contact) > 80 {
		return Draft{}, errInput("来源或联系方式太长")
	}
	if draft.Kind == kindMaintain {
		draft.Status = statusDone
		if draft.Result != "" {
			draft.Status = statusDone
		}
	} else if draft.Status == "" {
		draft.Status = statusAccepted
	}
	if !validStatus(draft.Status) {
		return Draft{}, errInput("状态不正确")
	}
	merged := map[int64]int{}
	for _, part := range draft.Parts {
		if part.ItemID < 1 || part.Qty < 1 || part.Qty > 999 {
			return Draft{}, errInput("元件数量不正确")
		}
		merged[part.ItemID] += part.Qty
	}
	draft.Parts = nil
	for id, qty := range merged {
		if qty > 999 {
			return Draft{}, errInput("元件数量不正确")
		}
		draft.Parts = append(draft.Parts, Part{ItemID: id, Qty: qty})
	}
	return draft, nil
}

func dateOnly(t time.Time) time.Time {
	y, m, d := t.In(cst).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}
