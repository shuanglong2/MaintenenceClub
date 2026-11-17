package inventory

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/audit"
)

// Move 写入一条数量流水. tx 为空时自己开事务并提交, 否则交给调用方提交.
// 出库前会锁住物品行, 结果数量不能为负.
func (s *Service) Move(ctx context.Context, tx pgx.Tx, actor Actor, in MoveInput) error {
	own := tx == nil
	if own {
		var err error
		tx, err = s.pool.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
	}
	if err := s.moveTx(ctx, tx, actor, in); err != nil {
		return err
	}
	if own {
		return tx.Commit(ctx)
	}
	return nil
}

func (s *Service) moveTx(ctx context.Context, tx pgx.Tx, actor Actor, in MoveInput) error {
	if !use(actor) {
		return ErrForbidden
	}
	in.Note = strings.TrimSpace(in.Note)
	if in.ItemID < 1 || utf8.RuneCountInString(in.Note) > 200 {
		return ErrBadInput
	}
	switch in.Kind {
	case "in", "use":
	case "adjust", "scrap":
		if !manage(actor) {
			return ErrForbidden
		}
	default:
		return ErrBadInput
	}
	item, err := lockItem(ctx, tx, in.ItemID)
	if err != nil {
		return err
	}
	if item.Voided {
		return ErrNotFound
	}
	delta, err := deltaOf(item, in)
	if err != nil {
		return err
	}
	if item.Qty+delta < 0 {
		return ErrStock
	}
	if item.Tracked {
		next := item.Qty + delta
		if next != 0 && next != 1 {
			return ErrBadInput
		}
		if next == 1 && item.State == stateScrapped {
			return ErrState
		}
	}
	if err := insertMove(ctx, tx, actor, item.ID, in.Kind, delta, in.Note, in.RefType, in.RefID); err != nil {
		return err
	}
	if item.Tracked && item.Qty+delta == 0 && item.State != stateScrapped {
		if err := insertEvent(ctx, tx, actor, item.ID, "scrap", in.Note); err != nil {
			return err
		}
	}
	return audit.Write(ctx, tx, audit.Event{
		ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: "inventory.move",
		Entity: "item", EntityID: &item.ID, Summary: KindLabel(in.Kind) + " " + item.Name,
	})
}

func deltaOf(item Item, in MoveInput) (int, error) {
	switch in.Kind {
	case "in":
		if in.Qty < 1 {
			return 0, ErrBadInput
		}
		if item.Tracked && item.Qty+in.Qty != 1 {
			return 0, ErrBadInput
		}
		return in.Qty, nil
	case "use":
		if item.Tracked || in.Qty < 1 {
			return 0, ErrBadInput
		}
		return -in.Qty, nil
	case "scrap":
		if in.Qty < 1 {
			return 0, ErrBadInput
		}
		if item.Tracked && (item.State == stateScrapped || in.Qty != item.Qty) {
			return 0, ErrState
		}
		return -in.Qty, nil
	case "adjust":
		if in.Qty == 0 {
			return 0, ErrBadInput
		}
		return in.Qty, nil
	default:
		return 0, ErrBadInput
	}
}

// Event 记录借出, 归还, 送修或修好. 不改变数量. 报废走 Move, 以便同时扣数量.
func (s *Service) Event(ctx context.Context, actor Actor, itemID int64, kind, note string) error {
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > 200 {
		return ErrBadInput
	}
	switch kind {
	case "borrow", "return":
		if !use(actor) {
			return ErrForbidden
		}
	case "repair", "repaired":
		if !manage(actor) {
			return ErrForbidden
		}
	default:
		return ErrBadInput
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	item, err := lockItem(ctx, tx, itemID)
	if err != nil {
		return err
	}
	if item.Voided {
		return ErrNotFound
	}
	if !item.Tracked || item.Qty < 1 {
		return ErrState
	}
	if !eventAllowed(item.State, kind) {
		return ErrState
	}
	if err := insertEvent(ctx, tx, actor, item.ID, kind, note); err != nil {
		return err
	}
	if err := audit.Write(ctx, tx, audit.Event{
		ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: "inventory.event",
		Entity: "item", EntityID: &item.ID, Summary: KindLabel(kind) + " " + item.Name,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func eventAllowed(state, kind string) bool {
	switch kind {
	case "borrow", "repair":
		return state == stateAvailable
	case "return":
		return state == stateBorrowed
	case "repaired":
		return state == stateRepairing
	default:
		return false
	}
}

func lockItem(ctx context.Context, tx pgx.Tx, id int64) (Item, error) {
	var found int64
	err := tx.QueryRow(ctx, `SELECT id FROM inventory.items WHERE id = $1 FOR UPDATE`, id).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	if err != nil {
		return Item{}, err
	}
	return loadTx(ctx, tx, id)
}

func insertMove(ctx context.Context, tx pgx.Tx, actor Actor, itemID int64, kind string, qty int, note, refType string, refID int64) error {
	_, err := tx.Exec(ctx, `
INSERT INTO inventory.movements (item_id, kind, qty, actor_id, actor_name, note, ref_type, ref_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, itemID, kind, qty, nullID(actor.ID), actor.Name, note, refType, nullID(refID))
	if err != nil || kind != "use" {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE inventory.items SET last_used_at = now(), updated_at = now() WHERE id = $1`, itemID)
	return err
}

func insertEvent(ctx context.Context, tx pgx.Tx, actor Actor, itemID int64, kind, note string) error {
	_, err := tx.Exec(ctx, `
INSERT INTO inventory.equipment_events (item_id, kind, actor_id, actor_name, note)
VALUES ($1, $2, $3, $4, $5)`, itemID, kind, nullID(actor.ID), actor.Name, note)
	return err
}
