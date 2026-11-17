package web

import (
	"context"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/skip2/go-qrcode"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/inventory"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/people"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/files"
)

type invView struct {
	Items      []inventory.Item
	Item       inventory.Item
	Moves      []inventory.Movement
	Events     []inventory.Event
	Locations  []inventory.Location
	Stocktakes []inventory.Stocktake
	Stocktake  inventory.Stocktake
	Lines      []inventory.Line
	Panels     []inventory.ItemPanel
	Query      string
	Category   string
	Purpose    string
	Condition  string
	Manage     bool
	Notice     string
	HasItem    bool
}

// RegisterInventoryWidget 把低库存登记到首页.
func RegisterInventoryWidget(svc *inventory.Service) {
	if svc == nil {
		return
	}
	RegisterWidget(Widget{
		Title: "低库存",
		Allow: func(user people.User) bool {
			return people.Can(user, people.ActInventoryUse, people.Resource{})
		},
		Render: func(ctx context.Context, _ people.User) (template.HTML, error) {
			return svc.LowStockHTML(ctx)
		},
	})
}

func (s *server) inventoryList(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := s.invUser(w, r)
	if !ok {
		return
	}
	items, err := s.inv.List(r.Context(), actor, r.URL.Query().Get("category"), r.URL.Query().Get("q"))
	if err != nil {
		s.showInvErr(w, r, err)
		return
	}
	purpose := strings.TrimSpace(r.URL.Query().Get("purpose"))
	condition := strings.TrimSpace(r.URL.Query().Get("condition"))
	shown := make([]inventory.Item, 0, len(items))
	for _, item := range items {
		if purpose != "" && !strings.Contains(item.Purpose, purpose) {
			continue
		}
		if condition != "" && item.Condition != condition {
			continue
		}
		shown = append(shown, item)
	}
	items = shown
	voided, err := s.inv.Voided(r.Context(), actor)
	if err != nil {
		s.showInvErr(w, r, err)
		return
	}
	view := invView{
		Items: items, Query: strings.TrimSpace(r.URL.Query().Get("q")),
		Category: strings.TrimSpace(r.URL.Query().Get("category")), Purpose: purpose, Condition: condition,
		Manage: inventory.Manage(actor),
	}
	if raw := r.URL.Query().Get("id"); raw != "" {
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil && id > 0 {
			if item, err := s.inv.Get(r.Context(), actor, id); err == nil {
				view.Item = item
				view.HasItem = true
			}
		}
	}
	if len(voided) > 0 {
		view.Notice = "已作废 " + strconv.Itoa(len(voided)) + " 件, 可在物品页恢复."
	}
	s.render(w, r, http.StatusOK, page{Title: "库存", Body: "inventory-body", Inv: view})
}

func (s *server) inventoryLow(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := s.invUser(w, r)
	if !ok {
		return
	}
	items, err := s.inv.LowStock(r.Context(), 50)
	if err != nil {
		s.showInvErr(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, page{
		Title: "低库存", Body: "inventory-body",
		Inv: invView{Items: items, Manage: inventory.Manage(actor), Category: "low"},
	})
}

func (s *server) inventoryNew(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.invAdmin(w, r)
	if !ok {
		return
	}
	s.render(w, r, http.StatusOK, page{
		Title: "新建物品", Body: "item-form-body",
		Inv: invView{Manage: inventory.Manage(actor), Item: inventory.Item{Unit: "个"}},
	})
}

func (s *server) inventoryCreate(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.invAdmin(w, r)
	if !ok {
		return
	}
	draft, err := s.itemDraft(r, actor)
	if err != nil {
		s.showInvErr(w, r, err)
		return
	}
	item, err := s.inv.Create(r.Context(), actor, draft)
	if err != nil {
		s.showInvErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/inventory/items/"+strconv.FormatInt(item.ID, 10), http.StatusSeeOther)
}

func (s *server) inventoryItem(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := s.invUser(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	item, err := s.inv.Get(r.Context(), actor, id)
	if err != nil {
		s.showInvErr(w, r, err)
		return
	}
	moves, events, err := s.inv.History(r.Context(), actor, id)
	if err != nil {
		s.showInvErr(w, r, err)
		return
	}
	panels, err := inventory.ItemPanels(r.Context(), id)
	if err != nil {
		s.showInvErr(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, page{
		Title: item.Name, Body: "item-body",
		Inv: invView{Item: item, Moves: moves, Events: events, Panels: panels, Manage: inventory.Manage(actor)},
	})
}

func (s *server) inventoryEdit(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.invAdmin(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	item, err := s.inv.Get(r.Context(), actor, id)
	if err != nil {
		s.showInvErr(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, page{
		Title: "编辑 " + item.Name, Body: "item-form-body", Editing: true,
		Inv: invView{Item: item, Manage: inventory.Manage(actor)},
	})
}

func (s *server) inventoryUpdate(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.invAdmin(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	draft, err := s.itemDraft(r, actor)
	if err != nil {
		s.showInvErr(w, r, err)
		return
	}
	if _, err := s.inv.Update(r.Context(), actor, id, draft); err != nil {
		s.showInvErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/inventory/items/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *server) inventoryPhoto(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.invAdmin(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	store := s.uploads()
	if store == nil {
		s.showInvErr(w, r, inventory.ErrBadInput)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		s.showInvErr(w, r, inventory.ErrBadInput)
		return
	}
	defer file.Close()
	meta, err := store.Save(r.Context(), actor.ID, "inventory", header.Filename, file)
	if err != nil {
		s.showInvErr(w, r, err)
		return
	}
	if err := s.inv.SetPhoto(r.Context(), actor, id, meta.ID); err != nil {
		s.showInvErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/inventory/items/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *server) inventoryVoid(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.invAdmin(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	if err := s.inv.Void(r.Context(), actor, id, r.FormValue("reason")); err != nil {
		s.showInvErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/inventory/items/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *server) inventoryUnvoid(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.invAdmin(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	if err := s.inv.Unvoid(r.Context(), actor, id); err != nil {
		s.showInvErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/inventory/items/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *server) inventoryMove(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := s.invUser(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	qty, _ := strconv.Atoi(r.FormValue("qty"))
	if err := s.inv.Move(r.Context(), nil, actor, inventory.MoveInput{
		ItemID: id, Kind: r.FormValue("kind"), Qty: qty, Note: r.FormValue("note"),
	}); err != nil {
		s.showInvErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/inventory/items/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *server) inventoryEvent(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := s.invUser(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	if err := s.inv.Event(r.Context(), actor, id, r.FormValue("kind"), r.FormValue("note")); err != nil {
		s.showInvErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/inventory/items/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *server) inventoryLabel(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := s.invUser(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	item, err := s.inv.Get(r.Context(), actor, id)
	if err != nil {
		s.showInvErr(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, page{Title: "标签 " + item.Code, Body: "item-label-body", Inv: invView{Item: item}})
}

func (s *server) inventoryQR(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := s.invUser(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	if _, err := s.inv.Get(r.Context(), actor, id); err != nil {
		s.showInvErr(w, r, err)
		return
	}
	link, err := itemLink(r, id)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	png, err := qrcode.Encode(link, qrcode.Medium, 256)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(png)
}

func itemLink(r *http.Request, id int64) (string, error) {
	if r.Host == "" || strings.ContainsAny(r.Host, " /\r\n") {
		return "", errors.New("主机无效")
	}
	return (&url.URL{Scheme: "http", Host: r.Host, Path: "/inventory/items/" + strconv.FormatInt(id, 10)}).String(), nil
}

func (s *server) inventoryLocation(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.invAdmin(w, r)
	if !ok {
		return
	}
	parent, _ := strconv.ParseInt(r.FormValue("parent"), 10, 64)
	if _, err := s.inv.CreateLocation(r.Context(), actor, r.FormValue("name"), parent); err != nil {
		s.showInvErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/inventory", http.StatusSeeOther)
}

func (s *server) inventoryImportForm(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.invAdmin(w, r); !ok {
		return
	}
	s.render(w, r, http.StatusOK, page{Title: "导入物品", Body: "inventory-import-body"})
}

func (s *server) inventoryImport(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.invAdmin(w, r)
	if !ok {
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		s.showInvErr(w, r, inventory.ErrBadInput)
		return
	}
	defer file.Close()
	rows, err := inventory.ParseImport(file)
	if err != nil {
		s.showInvErr(w, r, err)
		return
	}
	n, err := s.inv.Import(r.Context(), actor, rows)
	if err != nil {
		s.showInvErr(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, page{
		Title: "导入物品", Body: "inventory-import-body", Notice: "已导入 " + strconv.Itoa(n) + " 件",
	})
}

func (s *server) inventoryTemplate(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.invAdmin(w, r); !ok {
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="items-template.csv"`)
	_, _ = w.Write(inventory.TemplateCSV())
}

func (s *server) inventoryExport(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := s.invUser(w, r)
	if !ok {
		return
	}
	body, err := s.inv.Export(r.Context(), actor)
	if err != nil {
		s.showInvErr(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="items.csv"`)
	_, _ = w.Write(body)
}

func (s *server) stocktakes(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.invAdmin(w, r)
	if !ok {
		return
	}
	list, err := s.inv.Stocktakes(r.Context(), actor)
	if err != nil {
		s.showInvErr(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, page{Title: "盘点", Body: "stocktakes-body", Inv: invView{Stocktakes: list, Manage: true}})
}

func (s *server) stocktakeCreate(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.invAdmin(w, r)
	if !ok {
		return
	}
	id, err := s.inv.CreateStocktake(r.Context(), actor, r.FormValue("title"))
	if err != nil {
		s.showInvErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/inventory/stocktakes/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *server) stocktakeShow(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.invAdmin(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	head, lines, err := s.inv.StocktakeDetail(r.Context(), actor, id)
	if err != nil {
		s.showInvErr(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, page{
		Title: head.Title, Body: "stocktake-body",
		Inv: invView{Stocktake: head, Lines: lines, Manage: true},
	})
}

func (s *server) stocktakeSave(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.invAdmin(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	actual := map[int64]int{}
	for key, values := range r.Form {
		if !strings.HasPrefix(key, "qty_") || len(values) == 0 || strings.TrimSpace(values[0]) == "" {
			continue
		}
		itemID, err := strconv.ParseInt(strings.TrimPrefix(key, "qty_"), 10, 64)
		if err != nil {
			continue
		}
		qty, err := strconv.Atoi(values[0])
		if err != nil {
			s.showInvErr(w, r, inventory.ErrBadInput)
			return
		}
		actual[itemID] = qty
	}
	if err := s.inv.SaveLines(r.Context(), actor, id, actual); err != nil {
		s.showInvErr(w, r, err)
		return
	}
	if r.FormValue("confirm") == "1" {
		if err := s.inv.ConfirmStocktake(r.Context(), actor, id); err != nil {
			s.showInvErr(w, r, err)
			return
		}
	}
	http.Redirect(w, r, "/inventory/stocktakes/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *server) invAdmin(w http.ResponseWriter, r *http.Request) (inventory.Actor, bool) {
	_, actor, ok := s.invUser(w, r)
	if !ok {
		return inventory.Actor{}, false
	}
	if !inventory.Manage(actor) {
		s.showInvErr(w, r, inventory.ErrForbidden)
		return inventory.Actor{}, false
	}
	return actor, true
}

func (s *server) invUser(w http.ResponseWriter, r *http.Request) (visitor, inventory.Actor, bool) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return visitor{}, inventory.Actor{}, false
	}
	if s.inv == nil {
		s.internal(w, r, errors.New("库存服务未配置"))
		return visitor{}, inventory.Actor{}, false
	}
	return user, inventory.Actor{
		ID: user.Session.User.ID, Name: user.Session.Name, Role: user.Session.User.Role,
		Active: user.Session.User.Active, IP: user.IP,
	}, true
}

func (s *server) showInvErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, inventory.ErrNotFound), errors.Is(err, files.ErrNotFound):
		s.notFound(w, r)
	case errors.Is(err, inventory.ErrForbidden):
		s.render(w, r, http.StatusForbidden, page{Title: "提示", Body: "error-body", Status: http.StatusForbidden, Message: err.Error()})
	case errors.Is(err, inventory.ErrBadInput), errors.Is(err, inventory.ErrStock), errors.Is(err, inventory.ErrState),
		errors.Is(err, files.ErrBadFile), errors.Is(err, files.ErrTooBig), errors.Is(err, files.ErrPixels), errors.Is(err, files.ErrQuota):
		s.render(w, r, http.StatusBadRequest, page{Title: "提示", Body: "error-body", Status: http.StatusBadRequest, Message: err.Error()})
	default:
		s.internal(w, r, err)
	}
}

func (s *server) uploads() *files.Store {
	if s.docs != nil && s.docs.Files() != nil {
		return s.docs.Files()
	}
	if s.inv != nil && s.inv.Files() != nil {
		return s.inv.Files()
	}
	if s.events != nil && s.events.Files() != nil {
		return s.events.Files()
	}
	if s.logs != nil && s.logs.Files() != nil {
		return s.logs.Files()
	}
	if s.finance != nil {
		return s.finance.Files()
	}
	return nil
}

func (s *server) itemDraft(r *http.Request, actor inventory.Actor) (inventory.Draft, error) {
	draft := readItemDraft(r)
	place, err := s.inv.Place(r.Context(), actor, r.FormValue("place"))
	if err != nil {
		return inventory.Draft{}, err
	}
	draft.LocationID = place
	return draft, nil
}

func readItemDraft(r *http.Request) inventory.Draft {
	minQty, _ := strconv.Atoi(r.FormValue("min_qty"))
	qty, _ := strconv.Atoi(r.FormValue("qty"))
	var received *time.Time
	if raw := r.FormValue("received"); raw != "" {
		if parsed, err := time.ParseInLocation("2006-01-02", raw, time.FixedZone("CST", 8*3600)); err == nil {
			received = &parsed
		}
	}
	return inventory.Draft{
		Code: r.FormValue("code"), Name: r.FormValue("name"), Category: r.FormValue("category"),
		Spec: r.FormValue("spec"), Unit: r.FormValue("unit"),
		MinQty: minQty, Tracked: r.FormValue("tracked") == "1", Note: r.FormValue("note"), Qty: qty,
		Purpose: r.FormValue("purpose"), Model: r.FormValue("model"), Value: r.FormValue("value"),
		Condition: r.FormValue("condition"), Received: received,
	}
}
