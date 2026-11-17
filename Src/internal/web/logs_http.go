package web

import (
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/inventory"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/logs"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/files"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/mdown"
)

type logView struct {
	Entry    logs.Entry
	Entries  []logs.Entry
	Stock    []logStock
	Gear     []logStock
	BodyHTML template.HTML
	CanEdit  bool
	Kind     string
	Status   string
	Mine     bool
	Query    string
	Page     int
	HasNext  bool
	Total    int
}

func (v logView) PageHref(page int) string {
	q := url.Values{}
	if v.Mine {
		q.Set("mine", "1")
	}
	if v.Kind != "" {
		q.Set("kind", v.Kind)
	}
	if v.Status != "" {
		q.Set("status", v.Status)
	}
	if v.Query != "" {
		q.Set("q", v.Query)
	}
	if page > 1 {
		q.Set("page", strconv.Itoa(page))
	}
	if encoded := q.Encode(); encoded != "" {
		return "/logs?" + encoded
	}
	return "/logs"
}

type logStock struct {
	logs.Stock
	Chosen int
}

func (s *server) logsList(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.logUser(w, r)
	if !ok {
		return
	}
	filter := logs.Filter{
		Mine: r.URL.Query().Get("mine") == "1", Kind: r.URL.Query().Get("kind"),
		Status: r.URL.Query().Get("status"), Query: r.URL.Query().Get("q"), Limit: 30,
	}
	pageNo := 1
	if raw := r.URL.Query().Get("page"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 1 {
			pageNo = n
		}
	}
	filter.Offset = (pageNo - 1) * filter.Limit
	items, total, err := s.logs.List(r.Context(), actor, filter)
	if err != nil {
		s.showLogErr(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, page{
		Title: "日志", Body: "logs-body",
		Log: logView{
			Entries: items, Kind: filter.Kind, Status: filter.Status, Mine: filter.Mine, Query: filter.Query,
			Page: pageNo, HasNext: filter.Offset+len(items) < total, Total: total,
		},
	})
}

func (s *server) logsNew(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.logUser(w, r)
	if !ok {
		return
	}
	kind := r.URL.Query().Get("kind")
	if kind == "" {
		kind = "work"
	}
	entry := logs.Entry{Kind: kind, Date: time.Now(), Status: "accepted", Target: "other"}
	if kind == "maintain" {
		entry.Status = "done"
	}
	view, err := s.logForm(r, actor, entry)
	if err != nil {
		s.showLogErr(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, page{Title: "新建" + logs.KindLabel(kind), Body: "log-form-body", Log: view})
}

func (s *server) logsCreate(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.logUser(w, r)
	if !ok {
		return
	}
	entry, err := s.logs.Create(r.Context(), actor, readLogDraft(r))
	if err != nil {
		s.showLogErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/logs/"+strconv.FormatInt(entry.ID, 10), http.StatusSeeOther)
}

func (s *server) logsShow(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.logUser(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	entry, err := s.logs.Get(r.Context(), actor, id)
	if err != nil {
		s.showLogErr(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, page{
		Title: logs.KindLabel(entry.Kind), Body: "log-body",
		Log: logView{Entry: entry, BodyHTML: mdown.Render(entry.Body), CanEdit: logCanEdit(actor, entry)},
	})
}

func (s *server) logsEdit(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.logUser(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	entry, err := s.logs.Get(r.Context(), actor, id)
	if err != nil {
		s.showLogErr(w, r, err)
		return
	}
	if !logCanEdit(actor, entry) || entry.Voided {
		s.showLogErr(w, r, logs.ErrForbidden)
		return
	}
	view, err := s.logForm(r, actor, entry)
	if err != nil {
		s.showLogErr(w, r, err)
		return
	}
	view.CanEdit = true
	s.render(w, r, http.StatusOK, page{Title: "编辑" + logs.KindLabel(entry.Kind), Body: "log-form-body", Editing: true, Log: view})
}

func (s *server) logsUpdate(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.logUser(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	if _, err := s.logs.Update(r.Context(), actor, id, readLogDraft(r)); err != nil {
		s.showLogErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/logs/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *server) logsStatus(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.logUser(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	if err := s.logs.SetStatus(r.Context(), actor, id, r.FormValue("status")); err != nil {
		s.showLogErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/logs/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *server) logsVoid(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.logUser(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	if err := s.logs.Void(r.Context(), actor, id, r.FormValue("reason")); err != nil {
		s.showLogErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/logs/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *server) logsUpload(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.logUser(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		s.showLogErr(w, r, logs.ErrBadInput)
		return
	}
	defer file.Close()
	if err := s.logs.Attach(r.Context(), actor, id, header.Filename, file); err != nil {
		s.showLogErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/logs/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *server) logForm(r *http.Request, actor logs.Actor, entry logs.Entry) (logView, error) {
	stock, err := s.logs.Stock(r.Context(), actor)
	if err != nil {
		return logView{}, err
	}
	chosen := map[int64]int{}
	for _, part := range entry.Parts {
		chosen[part.ItemID] = part.Qty
	}
	rows := make([]logStock, 0, len(stock))
	for _, item := range stock {
		rows = append(rows, logStock{Stock: item, Chosen: chosen[item.ID]})
	}
	return logView{Entry: entry, Stock: rows, Gear: s.logGear(r, actor, entry)}, nil
}

func (s *server) logGear(r *http.Request, actor logs.Actor, entry logs.Entry) []logStock {
	var out []logStock
	seen := map[int64]bool{}
	if s.inv != nil {
		items, err := s.inv.List(r.Context(), inventory.Actor{
			ID: actor.ID, Name: actor.Name, Role: actor.Role, Active: actor.Active, IP: actor.IP,
		}, "", "")
		if err == nil {
			for _, item := range items {
				if !item.Tracked && item.ID != entry.ItemID {
					continue
				}
				out = append(out, logStock{Stock: logs.Stock{ID: item.ID, Code: item.Code, Name: item.Name, Qty: item.Qty}})
				seen[item.ID] = true
			}
		}
	}
	if entry.ItemID > 0 && !seen[entry.ItemID] {
		out = append(out, logStock{Stock: logs.Stock{ID: entry.ItemID, Code: "", Name: entry.ItemName}})
	}
	return out
}

func (s *server) logUser(w http.ResponseWriter, r *http.Request) (logs.Actor, bool) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return logs.Actor{}, false
	}
	if s.logs == nil {
		s.internal(w, r, errors.New("日志服务未配置"))
		return logs.Actor{}, false
	}
	return logs.Actor{
		ID: user.Session.User.ID, Name: user.Session.Name, Role: user.Session.User.Role,
		Active: user.Session.User.Active, IP: user.IP,
	}, true
}

func readLogDraft(r *http.Request) logs.Draft {
	var day *time.Time
	if raw := strings.TrimSpace(r.FormValue("date")); raw != "" {
		if parsed, err := time.Parse("2006-01-02", raw); err == nil {
			day = &parsed
		}
	}
	var minutes *int
	if raw := strings.TrimSpace(r.FormValue("minutes")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			minutes = &n
		}
	}
	itemID, _ := strconv.ParseInt(r.FormValue("item"), 10, 64)
	draft := logs.Draft{
		Kind: r.FormValue("kind"), Date: day, Body: r.FormValue("body"), Minutes: minutes,
		Target: r.FormValue("target"), ItemID: itemID, Symptom: r.FormValue("symptom"),
		Action: r.FormValue("action"), Result: r.FormValue("result"), Status: r.FormValue("status"),
		Source: r.FormValue("source"), Contact: r.FormValue("contact"),
	}
	if draft.Kind == "work" {
		return draft
	}
	for key, values := range r.Form {
		if !strings.HasPrefix(key, "qty_") || len(values) == 0 {
			continue
		}
		id, err := strconv.ParseInt(strings.TrimPrefix(key, "qty_"), 10, 64)
		if err != nil || id < 1 {
			continue
		}
		qty, err := strconv.Atoi(values[0])
		if err != nil || qty < 1 {
			continue
		}
		draft.Parts = append(draft.Parts, logs.Part{ItemID: id, Qty: qty})
	}
	return draft
}

func logCanEdit(actor logs.Actor, entry logs.Entry) bool {
	if !actor.Active || entry.Voided {
		return false
	}
	if actor.Role == "leader" || actor.Role == "deputy" {
		return true
	}
	return actor.ID == entry.AuthorID
}

func (s *server) showLogErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, logs.ErrNotFound):
		s.notFound(w, r)
	case errors.Is(err, logs.ErrForbidden):
		s.render(w, r, http.StatusForbidden, page{Title: "提示", Body: "error-body", Status: http.StatusForbidden, Message: err.Error()})
	case errors.Is(err, logs.ErrBadInput), errors.Is(err, logs.ErrState),
		errors.Is(err, files.ErrBadFile), errors.Is(err, files.ErrTooBig), errors.Is(err, files.ErrPixels), errors.Is(err, files.ErrQuota):
		s.render(w, r, http.StatusBadRequest, page{Title: "提示", Body: "error-body", Status: http.StatusBadRequest, Message: err.Error()})
	default:
		s.internal(w, r, err)
	}
}
