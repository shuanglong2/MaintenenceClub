package inventory

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/audit"
)

// CreateStocktake 为当前未作废物品生成一张盘点单.
func (s *Service) CreateStocktake(ctx context.Context, actor Actor, title string) (int64, error) {
	if !manage(actor) {
		return 0, ErrForbidden
	}
	title = strings.TrimSpace(title)
	if title == "" || utf8.RuneCountInString(title) > 40 {
		return 0, ErrBadInput
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var open int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM inventory.stocktakes WHERE status = 'open'`).Scan(&open); err != nil {
		return 0, err
	}
	if open > 0 {
		return 0, errText("已有未完成的盘点")
	}
	rows, err := tx.Query(ctx, `
SELECT id, coalesce((SELECT sum(qty) FROM inventory.movements m WHERE m.item_id = inventory.items.id), 0)
FROM inventory.items WHERE voided_at IS NULL ORDER BY id`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	type pair struct {
		id  int64
		qty int
	}
	var items []pair
	for rows.Next() {
		var item pair
		if err := rows.Scan(&item.id, &item.qty); err != nil {
			return 0, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if len(items) == 0 {
		return 0, errText("没有可盘点的物品")
	}
	var id int64
	if err := tx.QueryRow(ctx, `
INSERT INTO inventory.stocktakes (title, created_by) VALUES ($1, $2) RETURNING id`, title, nullID(actor.ID)).Scan(&id); err != nil {
		return 0, err
	}
	for _, item := range items {
		if _, err := tx.Exec(ctx, `
INSERT INTO inventory.stocktake_lines (stocktake_id, item_id, expected_qty) VALUES ($1, $2, $3)`, id, item.id, item.qty); err != nil {
			return 0, err
		}
	}
	if err := audit.Write(ctx, tx, audit.Event{
		ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: "inventory.stocktake",
		Entity: "stocktake", EntityID: &id, Summary: "创建盘点 " + title,
	}); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return id, nil
}

// Stocktakes 返回最近的盘点单.
func (s *Service) Stocktakes(ctx context.Context, actor Actor) ([]Stocktake, error) {
	if !manage(actor) {
		return nil, ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `
SELECT id, title, status, created_at, confirmed_at
FROM inventory.stocktakes ORDER BY id DESC LIMIT 20`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Stocktake
	for rows.Next() {
		var item Stocktake
		var confirmed *time.Time
		if err := rows.Scan(&item.ID, &item.Title, &item.Status, &item.Created, &confirmed); err != nil {
			return nil, err
		}
		if confirmed != nil {
			item.Confirmed = *confirmed
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// StocktakeDetail 读取盘点单和明细.
func (s *Service) StocktakeDetail(ctx context.Context, actor Actor, id int64) (Stocktake, []Line, error) {
	if !manage(actor) {
		return Stocktake{}, nil, ErrForbidden
	}
	var head Stocktake
	var confirmed *time.Time
	err := s.pool.QueryRow(ctx, `
SELECT id, title, status, created_at, confirmed_at
FROM inventory.stocktakes WHERE id = $1`, id).Scan(&head.ID, &head.Title, &head.Status, &head.Created, &confirmed)
	if err != nil {
		return Stocktake{}, nil, ErrNotFound
	}
	if confirmed != nil {
		head.Confirmed = *confirmed
	}
	rows, err := s.pool.Query(ctx, `
SELECT l.item_id, i.code, i.name, i.unit, i.tracked, l.expected_qty, l.actual_qty
FROM inventory.stocktake_lines l
JOIN inventory.items i ON i.id = l.item_id
WHERE l.stocktake_id = $1
ORDER BY i.code, i.id`, id)
	if err != nil {
		return Stocktake{}, nil, err
	}
	defer rows.Close()
	var lines []Line
	for rows.Next() {
		var line Line
		if err := rows.Scan(&line.ItemID, &line.Code, &line.Name, &line.Unit, &line.Tracked, &line.Expected, &line.Actual); err != nil {
			return Stocktake{}, nil, err
		}
		line.Filled = line.Actual != nil
		if line.Actual != nil {
			line.ActualQty = *line.Actual
			line.Diff = *line.Actual - line.Expected
		}
		lines = append(lines, line)
	}
	return head, lines, rows.Err()
}

// SaveLines 保存实盘数量. 未出现的行保持原样.
func (s *Service) SaveLines(ctx context.Context, actor Actor, id int64, actual map[int64]int) error {
	if !manage(actor) {
		return ErrForbidden
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM inventory.stocktakes WHERE id = $1 FOR UPDATE`, id).Scan(&status); err != nil {
		return ErrNotFound
	}
	if status != "open" {
		return ErrState
	}
	for itemID, qty := range actual {
		if qty < 0 {
			return ErrBadInput
		}
		tag, err := tx.Exec(ctx, `
UPDATE inventory.stocktake_lines SET actual_qty = $3
WHERE stocktake_id = $1 AND item_id = $2`, id, itemID, qty)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrBadInput
		}
	}
	return tx.Commit(ctx)
}

// ConfirmStocktake 按实盘数量生成调整流水, 使账面等于实盘.
func (s *Service) ConfirmStocktake(ctx context.Context, actor Actor, id int64) error {
	if !manage(actor) {
		return ErrForbidden
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var status, title string
	if err := tx.QueryRow(ctx, `SELECT status, title FROM inventory.stocktakes WHERE id = $1 FOR UPDATE`, id).Scan(&status, &title); err != nil {
		return ErrNotFound
	}
	if status != "open" {
		return ErrState
	}
	rows, err := tx.Query(ctx, `SELECT item_id, actual_qty FROM inventory.stocktake_lines WHERE stocktake_id = $1 ORDER BY item_id`, id)
	if err != nil {
		return err
	}
	type row struct {
		id     int64
		actual *int
	}
	var lines []row
	for rows.Next() {
		var item row
		if err := rows.Scan(&item.id, &item.actual); err != nil {
			rows.Close()
			return err
		}
		lines = append(lines, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, line := range lines {
		if line.actual == nil {
			return errText("还有未填写的数量")
		}
		item, err := lockItem(ctx, tx, line.id)
		if err != nil {
			return err
		}
		if item.Tracked && (*line.actual != 0 && *line.actual != 1) {
			return ErrBadInput
		}
		if item.Tracked && item.State == stateScrapped && *line.actual == 1 {
			return ErrState
		}
		delta := *line.actual - item.Qty
		if delta == 0 {
			continue
		}
		if err := s.moveTx(ctx, tx, actor, MoveInput{
			ItemID: item.ID, Kind: "adjust", Qty: delta, Note: "盘点 " + title, RefType: "stocktake", RefID: id,
		}); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `
UPDATE inventory.stocktakes SET status = 'confirmed', confirmed_at = now(), confirmed_by = $2 WHERE id = $1`, id, actor.ID); err != nil {
		return err
	}
	if err := audit.Write(ctx, tx, audit.Event{
		ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: "inventory.stocktake",
		Entity: "stocktake", EntityID: &id, Summary: "确认盘点 " + title,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
