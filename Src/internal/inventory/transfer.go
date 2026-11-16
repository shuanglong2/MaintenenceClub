package inventory

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/audit"
)

// Import 一次写入全部物品. 任一行失败则整批取消.
func (s *Service) Import(ctx context.Context, actor Actor, rows []ImportRow) (int, error) {
	if !manage(actor) {
		return 0, ErrForbidden
	}
	if len(rows) == 0 {
		return 0, ErrBadInput
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	for _, row := range rows {
		if _, err := s.importRow(ctx, tx, actor, row); err != nil {
			return 0, err
		}
	}
	if err := audit.Write(ctx, tx, audit.Event{
		ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: "inventory.import",
		Entity: "item", Summary: "导入物品 " + strconv.Itoa(len(rows)) + " 件",
	}); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return len(rows), nil
}

func (s *Service) importRow(ctx context.Context, tx pgx.Tx, actor Actor, row ImportRow) (int64, error) {
	locationID, err := ensurePath(ctx, tx, row.Location)
	if err != nil {
		return 0, err
	}
	draft, err := cleanDraft(Draft{
		Code: row.Code, Name: row.Name, Category: row.Category, Spec: row.Spec, Unit: row.Unit,
		LocationID: locationID, MinQty: row.MinQty, Tracked: row.Tracked, Note: row.Note, Qty: row.Qty,
	}, true)
	if err != nil {
		return 0, err
	}
	item, err := insertItem(ctx, tx, draft)
	if err != nil {
		return 0, err
	}
	if draft.Qty > 0 {
		if err := insertMove(ctx, tx, actor, item.ID, "in", draft.Qty, "导入", "", 0); err != nil {
			return 0, err
		}
	}
	return item.ID, nil
}

func ensurePath(ctx context.Context, tx pgx.Tx, path string) (int64, error) {
	path = strings.Trim(strings.TrimSpace(path), "/")
	if path == "" {
		return 0, nil
	}
	var parent int64
	for _, name := range splitPath(path) {
		if name == "" {
			return 0, ErrBadInput
		}
		var id int64
		err := tx.QueryRow(ctx, `
SELECT id FROM inventory.locations
WHERE name = $1 AND coalesce(parent_id, 0) = $2`, name, parent).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			if err := tx.QueryRow(ctx, `
INSERT INTO inventory.locations (parent_id, name) VALUES ($1, $2) RETURNING id`, nullID(parent), name).Scan(&id); err != nil {
				return 0, err
			}
		} else if err != nil {
			return 0, err
		}
		parent = id
	}
	return parent, nil
}

func splitPath(path string) []string {
	path = strings.ReplaceAll(path, "／", "/")
	parts := strings.Split(path, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// Export 写出未作废物品的 CSV.
func (s *Service) Export(ctx context.Context, actor Actor) ([]byte, error) {
	if !use(actor) {
		return nil, ErrForbidden
	}
	items, err := s.List(ctx, actor, "", "")
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.Write([]byte{0xEF, 0xBB, 0xBF})
	w := csv.NewWriter(&buf)
	if err := w.Write(CSVHeader); err != nil {
		return nil, err
	}
	for _, item := range items {
		tracked := "否"
		if item.Tracked {
			tracked = "是"
		}
		record := []string{
			csvCell(item.Code), csvCell(item.Name), csvCell(item.Category), csvCell(item.Spec), csvCell(item.Unit),
			csvCell(item.Location), strconv.Itoa(item.MinQty), tracked, strconv.Itoa(item.Qty), csvCell(item.Note),
		}
		if err := w.Write(record); err != nil {
			return nil, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if err := audit.Write(ctx, tx, audit.Event{
		ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: "inventory.export",
		Entity: "item", Summary: "导出物品",
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func csvCell(value string) string {
	if value == "" {
		return value
	}
	switch value[0] {
	case '=', '+', '-', '@':
		return "'" + value
	default:
		return value
	}
}
