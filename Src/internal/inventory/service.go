// Package inventory 记录器材, 工具, 消耗品和元件的数量与状态.
package inventory

import (
	"context"
	"errors"
	"html"
	"html/template"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/people"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/audit"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/files"
)

var codePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,31}$`)

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

// Location 是存放位置.
type Location struct {
	ID       int64
	ParentID int64
	Name     string
	Path     string
	Sort     int
}

// Item 是一件物品. 数量和逐件状态由流水与事件推出.
type Item struct {
	ID         int64
	Code       string
	Name       string
	Category   string
	Spec       string
	Unit       string
	LocationID int64
	Location   string
	MinQty     int
	Tracked    bool
	PhotoID    int64
	Note       string
	Qty        int
	State      string
	Voided     bool
	VoidReason string
}

// Movement 是一条不可修改的数量流水.
type Movement struct {
	ID      int64
	Kind    string
	Qty     int
	Actor   string
	Note    string
	RefType string
	Created time.Time
}

// Event 是逐件器材的状态事件.
type Event struct {
	ID      int64
	Kind    string
	Actor   string
	Note    string
	Created time.Time
}

// Draft 是新建或修改物品的表单.
type Draft struct {
	Code       string
	Name       string
	Category   string
	Spec       string
	Unit       string
	LocationID int64
	MinQty     int
	Tracked    bool
	PhotoID    int64
	Note       string
	Qty        int
}

// MoveInput 是一次入库, 领用, 调整或报废.
type MoveInput struct {
	ItemID  int64
	Kind    string
	Qty     int
	Note    string
	RefType string
	RefID   int64
}

// Stocktake 是一张盘点单.
type Stocktake struct {
	ID        int64
	Title     string
	Status    string
	Created   time.Time
	Confirmed time.Time
}

// Line 是盘点单上的一行.
type Line struct {
	ItemID    int64
	Code      string
	Name      string
	Unit      string
	Tracked   bool
	Expected  int
	Actual    *int
	Filled    bool
	ActualQty int
	Diff      int
}

// Service 处理库存.
type Service struct {
	pool  *pgxpool.Pool
	files *files.Store
}

// New 创建库存服务.
func New(pool *pgxpool.Pool, store *files.Store) *Service {
	return &Service{pool: pool, files: store}
}

// Files 返回上传存储, 供照片下载复用.
func (s *Service) Files() *files.Store {
	return s.files
}

func manage(actor Actor) bool {
	return people.Can(actor.user(), people.ActInventoryManage, people.Resource{})
}

// Manage 表示这个人可以维护物品, 盘点和报废.
func Manage(actor Actor) bool {
	return manage(actor)
}

func use(actor Actor) bool {
	return people.Can(actor.user(), people.ActInventoryUse, people.Resource{})
}

// CreateLocation 新建位置.
func (s *Service) CreateLocation(ctx context.Context, actor Actor, name string, parent int64) (int64, error) {
	if !manage(actor) {
		return 0, ErrForbidden
	}
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 40 {
		return 0, ErrBadInput
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if parent != 0 {
		if err := locationExists(ctx, tx, parent); err != nil {
			return 0, err
		}
	}
	var id int64
	if err := tx.QueryRow(ctx, `
INSERT INTO inventory.locations (parent_id, name) VALUES ($1, $2) RETURNING id`, nullID(parent), name).Scan(&id); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return id, nil
}

// UpdateLocation 改名或移动位置.
func (s *Service) UpdateLocation(ctx context.Context, actor Actor, id int64, name string, parent int64) error {
	if !manage(actor) {
		return ErrForbidden
	}
	name = strings.TrimSpace(name)
	if id < 1 || name == "" || utf8.RuneCountInString(name) > 40 || parent == id {
		return ErrBadInput
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := locationExists(ctx, tx, id); err != nil {
		return err
	}
	if parent != 0 {
		if err := locationExists(ctx, tx, parent); err != nil {
			return err
		}
		if err := noLocationCycle(ctx, tx, id, parent); err != nil {
			return err
		}
	}
	tag, err := tx.Exec(ctx, `UPDATE inventory.locations SET name = $2, parent_id = $3 WHERE id = $1`, id, name, nullID(parent))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return tx.Commit(ctx)
}

// Locations 返回带路径的位置, 按路径排序.
func (s *Service) Locations(ctx context.Context) ([]Location, error) {
	rows, err := s.pool.Query(ctx, `
WITH RECURSIVE chain AS (
    SELECT id, coalesce(parent_id, 0) AS parent_id, name, sort_order, name AS path, 1 AS depth
    FROM inventory.locations WHERE parent_id IS NULL
    UNION ALL
    SELECT l.id, coalesce(l.parent_id, 0), l.name, l.sort_order, chain.path || '/' || l.name, chain.depth + 1
    FROM inventory.locations l
    JOIN chain ON l.parent_id = chain.id
    WHERE chain.depth < 8
)
SELECT id, parent_id, name, path, sort_order FROM chain ORDER BY path, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Location
	for rows.Next() {
		var item Location
		if err := rows.Scan(&item.ID, &item.ParentID, &item.Name, &item.Path, &item.Sort); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// Create 新建物品. 初始数量写成一条入库流水. 逐件物品的数量必须是 1.
func (s *Service) Create(ctx context.Context, actor Actor, draft Draft) (Item, error) {
	if !manage(actor) {
		return Item{}, ErrForbidden
	}
	draft, err := cleanDraft(draft, true)
	if err != nil {
		return Item{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Item{}, err
	}
	defer tx.Rollback(ctx)
	item, err := insertItem(ctx, tx, draft)
	if err != nil {
		return Item{}, err
	}
	if draft.Qty > 0 {
		if err := insertMove(ctx, tx, actor, item.ID, "in", draft.Qty, "初始数量", "", 0); err != nil {
			return Item{}, err
		}
	}
	if err := audit.Write(ctx, tx, audit.Event{
		ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: "inventory.create",
		Entity: "item", EntityID: &item.ID, Summary: "新建物品 " + item.Name,
	}); err != nil {
		return Item{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Item{}, err
	}
	return s.Get(ctx, actor, item.ID)
}

// Update 修改资料. 逐件标记和数量不在这里改.
func (s *Service) Update(ctx context.Context, actor Actor, id int64, draft Draft) (Item, error) {
	if !manage(actor) {
		return Item{}, ErrForbidden
	}
	current, err := s.load(ctx, id)
	if err != nil {
		return Item{}, err
	}
	draft.Tracked = current.Tracked
	draft.Qty = current.Qty
	draft, err = cleanDraft(draft, false)
	if err != nil {
		return Item{}, err
	}
	if draft.PhotoID == 0 {
		draft.PhotoID = current.PhotoID
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Item{}, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
UPDATE inventory.items
SET code = $2, name = $3, category = $4, spec = $5, unit = $6, location_id = $7,
    min_qty = $8, photo_id = $9, note = $10, updated_at = now()
WHERE id = $1`, id, draft.Code, draft.Name, draft.Category, draft.Spec, draft.Unit, nullID(draft.LocationID), draft.MinQty, nullID(draft.PhotoID), draft.Note)
	if err != nil {
		return Item{}, mapUnique(err)
	}
	if tag.RowsAffected() == 0 {
		return Item{}, ErrNotFound
	}
	if err := audit.Write(ctx, tx, audit.Event{
		ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: "inventory.update",
		Entity: "item", EntityID: &id, Summary: "修改物品 " + draft.Name,
	}); err != nil {
		return Item{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Item{}, err
	}
	return s.Get(ctx, actor, id)
}

// SetPhoto 给物品换照片. photoID 为 0 表示去掉.
func (s *Service) SetPhoto(ctx context.Context, actor Actor, id, photoID int64) error {
	if !manage(actor) {
		return ErrForbidden
	}
	if photoID != 0 && s.files != nil {
		meta, err := s.files.Get(ctx, photoID)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(meta.Type, "image/") {
			return ErrBadInput
		}
	}
	tag, err := s.pool.Exec(ctx, `UPDATE inventory.items SET photo_id = $2, updated_at = now() WHERE id = $1 AND voided_at IS NULL`, id, nullID(photoID))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Void 作废物品. 流水保留.
func (s *Service) Void(ctx context.Context, actor Actor, id int64, reason string) error {
	return s.setVoid(ctx, actor, id, reason, true)
}

// Unvoid 恢复物品.
func (s *Service) Unvoid(ctx context.Context, actor Actor, id int64) error {
	return s.setVoid(ctx, actor, id, "", false)
}

func (s *Service) setVoid(ctx context.Context, actor Actor, id int64, reason string, void bool) error {
	if !manage(actor) {
		return ErrForbidden
	}
	reason = strings.TrimSpace(reason)
	if void && (reason == "" || utf8.RuneCountInString(reason) > 200) {
		return ErrBadInput
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	item, err := loadTx(ctx, tx, id)
	if err != nil {
		return err
	}
	action := "inventory.restore"
	summary := "恢复物品 " + item.Name
	var tag pgconn.CommandTag
	if void {
		action = "inventory.void"
		summary = "作废物品 " + item.Name
		tag, err = tx.Exec(ctx, `
UPDATE inventory.items SET voided_at = now(), void_reason = $2, voided_by = $3, updated_at = now()
WHERE id = $1 AND voided_at IS NULL`, id, reason, actor.ID)
	} else {
		tag, err = tx.Exec(ctx, `
UPDATE inventory.items SET voided_at = NULL, void_reason = '', voided_by = NULL, updated_at = now()
WHERE id = $1 AND voided_at IS NOT NULL`, id)
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err := audit.Write(ctx, tx, audit.Event{
		ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: action,
		Entity: "item", EntityID: &id, Summary: summary,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Get 读取一件物品, 含数量和状态.
func (s *Service) Get(ctx context.Context, actor Actor, id int64) (Item, error) {
	if !use(actor) {
		return Item{}, ErrForbidden
	}
	item, err := s.load(ctx, id)
	if err != nil {
		return Item{}, err
	}
	if item.Voided && !manage(actor) {
		return Item{}, ErrNotFound
	}
	paths, err := s.locationPaths(ctx)
	if err != nil {
		return Item{}, err
	}
	item.Location = paths[item.LocationID]
	return item, nil
}

// List 按类别和关键词列出未作废物品.
func (s *Service) List(ctx context.Context, actor Actor, category, query string) ([]Item, error) {
	if !use(actor) {
		return nil, ErrForbidden
	}
	category = strings.TrimSpace(category)
	if category != "" && !validCategory(category) {
		return nil, ErrBadInput
	}
	pattern := likePattern(query)
	rows, err := s.pool.Query(ctx, `
SELECT i.id FROM inventory.items i
LEFT JOIN inventory.locations l ON l.id = i.location_id
WHERE i.voided_at IS NULL
  AND ($1 = '' OR i.category = $1)
  AND ($2 = '' OR i.name ILIKE $2 ESCAPE '\' OR i.code ILIKE $2 ESCAPE '\' OR i.spec ILIKE $2 ESCAPE '\' OR l.name ILIKE $2 ESCAPE '\')
ORDER BY i.code, i.id
LIMIT 500`, category, pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return s.loadMany(ctx, ids)
}

// Voided 返回已作废物品, 只给管理层.
func (s *Service) Voided(ctx context.Context, actor Actor) ([]Item, error) {
	if !manage(actor) {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT id FROM inventory.items WHERE voided_at IS NOT NULL ORDER BY code, id LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return s.loadMany(ctx, ids)
}

// LowStock 返回未作废, 非逐件, 且数量低于最低库存的物品.
func (s *Service) LowStock(ctx context.Context, limit int) ([]Item, error) {
	if limit < 1 || limit > 50 {
		limit = 8
	}
	rows, err := s.pool.Query(ctx, `
SELECT i.id
FROM inventory.items i
LEFT JOIN inventory.movements m ON m.item_id = i.id
WHERE i.voided_at IS NULL AND NOT i.tracked AND i.min_qty > 0
GROUP BY i.id
HAVING coalesce(sum(m.qty), 0) < i.min_qty
ORDER BY i.name, i.id
LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return s.loadMany(ctx, ids)
}

// LowStockHTML 是首页组件用的一小段列表.
func (s *Service) LowStockHTML(ctx context.Context) (template.HTML, error) {
	items, err := s.LowStock(ctx, 8)
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return `<p class="empty">没有低于最低库存的物品.</p>`, nil
	}
	var b strings.Builder
	b.WriteString("<ul>")
	for _, item := range items {
		b.WriteString(`<li><a href="/inventory/items/`)
		b.WriteString(strconv.FormatInt(item.ID, 10))
		b.WriteString(`">`)
		b.WriteString(html.EscapeString(item.Name))
		b.WriteString("</a> ")
		b.WriteString(strconv.Itoa(item.Qty))
		b.WriteString(" ")
		b.WriteString(html.EscapeString(item.Unit))
		b.WriteString("</li>")
	}
	b.WriteString("</ul>")
	return template.HTML(b.String()), nil
}

// History 返回最近的流水和事件.
func (s *Service) History(ctx context.Context, actor Actor, id int64) ([]Movement, []Event, error) {
	if _, err := s.Get(ctx, actor, id); err != nil {
		return nil, nil, err
	}
	moves, err := s.movements(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	events, err := s.events(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	return moves, events, nil
}

// CanSeeFile 判断这个人能否查看作为物品照片的文件.
func (s *Service) CanSeeFile(ctx context.Context, actor Actor, fileID int64) (bool, error) {
	if !use(actor) || fileID < 1 {
		return false, nil
	}
	var voided bool
	err := s.pool.QueryRow(ctx, `
SELECT voided_at IS NOT NULL FROM inventory.items WHERE photo_id = $1 ORDER BY id LIMIT 1`, fileID).Scan(&voided)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if voided && !manage(actor) {
		return false, nil
	}
	return true, nil
}

func (s *Service) loadMany(ctx context.Context, ids []int64) ([]Item, error) {
	out := make([]Item, 0, len(ids))
	paths, err := s.locationPaths(ctx)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		item, err := s.load(ctx, id)
		if err != nil {
			return nil, err
		}
		item.Location = paths[item.LocationID]
		out = append(out, item)
	}
	return out, nil
}

func (s *Service) load(ctx context.Context, id int64) (Item, error) {
	return loadRow(s.pool.QueryRow(ctx, itemSelect+` WHERE i.id = $1`, id))
}

func loadTx(ctx context.Context, tx pgx.Tx, id int64) (Item, error) {
	return loadRow(tx.QueryRow(ctx, itemSelect+` WHERE i.id = $1`, id))
}

const itemSelect = `
SELECT i.id, i.code, i.name, i.category, i.spec, i.unit, coalesce(i.location_id, 0), i.min_qty, i.tracked,
       coalesce(i.photo_id, 0), i.note, i.voided_at IS NOT NULL, i.void_reason,
       coalesce((SELECT sum(m.qty) FROM inventory.movements m WHERE m.item_id = i.id), 0),
       coalesce((SELECT e.kind FROM inventory.equipment_events e WHERE e.item_id = i.id ORDER BY e.id DESC LIMIT 1), '')
FROM inventory.items i`

func loadRow(row pgx.Row) (Item, error) {
	var item Item
	var last string
	err := row.Scan(&item.ID, &item.Code, &item.Name, &item.Category, &item.Spec, &item.Unit, &item.LocationID, &item.MinQty, &item.Tracked, &item.PhotoID, &item.Note, &item.Voided, &item.VoidReason, &item.Qty, &last)
	if errors.Is(err, pgx.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	if err != nil {
		return Item{}, err
	}
	item.State = stateOf(last)
	return item, nil
}

func (s *Service) locationPaths(ctx context.Context) (map[int64]string, error) {
	list, err := s.Locations(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]string, len(list))
	for _, item := range list {
		out[item.ID] = item.Path
	}
	return out, nil
}

func (s *Service) movements(ctx context.Context, id int64) ([]Movement, error) {
	rows, err := s.pool.Query(ctx, `
SELECT id, kind, qty, actor_name, note, ref_type, created_at
FROM inventory.movements WHERE item_id = $1 ORDER BY id DESC LIMIT 20`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Movement
	for rows.Next() {
		var item Movement
		if err := rows.Scan(&item.ID, &item.Kind, &item.Qty, &item.Actor, &item.Note, &item.RefType, &item.Created); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) events(ctx context.Context, id int64) ([]Event, error) {
	rows, err := s.pool.Query(ctx, `
SELECT id, kind, actor_name, note, created_at
FROM inventory.equipment_events WHERE item_id = $1 ORDER BY id DESC LIMIT 20`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var item Event
		if err := rows.Scan(&item.ID, &item.Kind, &item.Actor, &item.Note, &item.Created); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func insertItem(ctx context.Context, tx pgx.Tx, draft Draft) (Item, error) {
	var item Item
	err := tx.QueryRow(ctx, `
INSERT INTO inventory.items (code, name, category, spec, unit, location_id, min_qty, tracked, photo_id, note)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING id, code, name, category, spec, unit, coalesce(location_id, 0), min_qty, tracked, coalesce(photo_id, 0), note, false::boolean, ''::text`,
		draft.Code, draft.Name, draft.Category, draft.Spec, draft.Unit, nullID(draft.LocationID), draft.MinQty, draft.Tracked, nullID(draft.PhotoID), draft.Note,
	).Scan(&item.ID, &item.Code, &item.Name, &item.Category, &item.Spec, &item.Unit, &item.LocationID, &item.MinQty, &item.Tracked, &item.PhotoID, &item.Note, &item.Voided, &item.VoidReason)
	if err != nil {
		return Item{}, mapUnique(err)
	}
	item.State = stateAvailable
	item.Qty = 0
	return item, nil
}

func cleanDraft(draft Draft, creating bool) (Draft, error) {
	draft.Code = strings.TrimSpace(draft.Code)
	draft.Name = strings.TrimSpace(draft.Name)
	draft.Category = strings.TrimSpace(draft.Category)
	draft.Spec = strings.TrimSpace(draft.Spec)
	draft.Unit = strings.TrimSpace(draft.Unit)
	draft.Note = strings.TrimSpace(draft.Note)
	if draft.Unit == "" {
		draft.Unit = "个"
	}
	if !codePattern.MatchString(draft.Code) || draft.Name == "" || utf8.RuneCountInString(draft.Name) > 80 || !validCategory(draft.Category) {
		return Draft{}, ErrBadInput
	}
	if utf8.RuneCountInString(draft.Spec) > 80 || utf8.RuneCountInString(draft.Unit) > 10 || utf8.RuneCountInString(draft.Note) > 500 || draft.MinQty < 0 || draft.Qty < 0 {
		return Draft{}, ErrBadInput
	}
	if draft.Tracked {
		draft.MinQty = 0
		if creating && draft.Qty == 0 {
			draft.Qty = 1
		}
		if creating && draft.Qty != 1 {
			return Draft{}, ErrBadInput
		}
	}
	return draft, nil
}

func validCategory(value string) bool {
	switch value {
	case "器材", "消耗品", "工具", "元件":
		return true
	default:
		return false
	}
}

func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

func mapUnique(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return errText("编号已存在")
	}
	return err
}

func locationExists(ctx context.Context, tx pgx.Tx, id int64) error {
	var found int64
	err := tx.QueryRow(ctx, `SELECT id FROM inventory.locations WHERE id = $1`, id).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrBadInput
	}
	return err
}

func noLocationCycle(ctx context.Context, tx pgx.Tx, id, parent int64) error {
	current := parent
	for range 8 {
		if current == id {
			return ErrBadInput
		}
		var next int64
		err := tx.QueryRow(ctx, `SELECT coalesce(parent_id, 0) FROM inventory.locations WHERE id = $1`, current).Scan(&next)
		if err != nil {
			return err
		}
		if next == 0 {
			return nil
		}
		current = next
	}
	return ErrBadInput
}

func likePattern(query string) string {
	query = strings.TrimSpace(query)
	if query == "" {
		return ""
	}
	return "%" + escapeLike(query) + "%"
}

func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func stateOf(last string) string {
	switch last {
	case "borrow":
		return stateBorrowed
	case "repair":
		return stateRepairing
	case "scrap":
		return stateScrapped
	default:
		return stateAvailable
	}
}

const (
	stateAvailable = "available"
	stateBorrowed  = "borrowed"
	stateRepairing = "repairing"
	stateScrapped  = "scrapped"
)

// StateLabel 返回状态名称.
func StateLabel(state string) string {
	switch state {
	case stateAvailable:
		return "可用"
	case stateBorrowed:
		return "借出"
	case stateRepairing:
		return "维修中"
	case stateScrapped:
		return "报废"
	default:
		return state
	}
}

// KindLabel 返回流水或事件的名称.
func KindLabel(kind string) string {
	switch kind {
	case "in":
		return "入库"
	case "use":
		return "领用"
	case "adjust":
		return "盘点调整"
	case "scrap":
		return "报废"
	case "borrow":
		return "借出"
	case "return":
		return "归还"
	case "repair":
		return "送修"
	case "repaired":
		return "修好"
	default:
		return kind
	}
}
