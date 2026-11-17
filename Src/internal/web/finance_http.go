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

	"github.com/shuanglong2/MaintenenceClub/Src/internal/finance"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/people"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/files"
)

// RegisterFinanceWidget 把手头和储蓄登记到首页. 只有能看财务的人会看到.
func RegisterFinanceWidget(svc *finance.Service) {
	if svc == nil {
		return
	}
	RegisterWidget(Widget{
		Title: "财务摘要",
		Allow: func(user people.User) bool {
			return people.Can(user, people.ActFinanceRead, people.Resource{})
		},
		Render: func(ctx context.Context, user people.User) (template.HTML, error) {
			return svc.HomeHTML(ctx)
		},
	})
}

type finView struct {
	Entry      finance.Entry
	Entries    []finance.Entry
	Categories []finance.Category
	Actors     []finance.PersonRef
	Months     []finBucket
	Groups     []finBucket
	Cash       int64
	Savings    int64
	Kind       string
	Month      string
	CategoryID int64
	ActorID    int64
	CanWrite   bool
	Page       int
	HasNext    bool
	Total      int
}

type finBucket struct {
	finance.Sum
	Ticks []bool
}

func (v finView) PageHref(page int) string {
	q := url.Values{}
	if v.Month != "" {
		q.Set("month", v.Month)
	}
	if v.Kind != "" {
		q.Set("kind", v.Kind)
	}
	if v.CategoryID > 0 {
		q.Set("category", strconv.FormatInt(v.CategoryID, 10))
	}
	if v.ActorID > 0 {
		q.Set("actor", strconv.FormatInt(v.ActorID, 10))
	}
	if page > 1 {
		q.Set("page", strconv.Itoa(page))
	}
	if encoded := q.Encode(); encoded != "" {
		return "/finance?" + encoded
	}
	return "/finance"
}

func (s *server) financeList(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.financeUser(w, r, false)
	if !ok {
		return
	}
	filter := readFinanceFilter(r)
	pageNo := 1
	if raw := r.URL.Query().Get("page"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 1 {
			pageNo = n
		}
	}
	filter.Limit = 30
	filter.Offset = (pageNo - 1) * filter.Limit
	items, total, err := s.finance.List(r.Context(), actor, filter)
	if err != nil {
		s.showFinErr(w, r, err)
		return
	}
	view, err := s.financeShell(r, actor)
	if err != nil {
		s.showFinErr(w, r, err)
		return
	}
	view.Entries = items
	view.Kind = filter.Kind
	view.Month = filter.Month
	view.CategoryID = filter.CategoryID
	view.ActorID = filter.ActorID
	view.Page = pageNo
	view.HasNext = filter.Offset+len(items) < total
	view.Total = total
	s.render(w, r, http.StatusOK, page{Title: "财务", Body: "finance-body", Fin: view})
}

func (s *server) financeNew(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.financeUser(w, r, true)
	if !ok {
		return
	}
	cats, err := s.finance.Categories(r.Context(), actor)
	if err != nil {
		s.showFinErr(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, page{
		Title: "记一笔", Body: "finance-form-body",
		Fin: finView{Categories: cats, Entry: finance.Entry{Kind: finance.KindIn, Date: time.Now().In(china)}, CanWrite: true},
	})
}

func (s *server) financeCreate(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.financeUser(w, r, true)
	if !ok {
		return
	}
	cents, err := finance.ParseYuan(r.FormValue("amount"))
	if err != nil {
		s.showFinErr(w, r, err)
		return
	}
	var day *time.Time
	if raw := strings.TrimSpace(r.FormValue("date")); raw != "" {
		if parsed, err := time.Parse("2006-01-02", raw); err == nil {
			day = &parsed
		}
	}
	categoryID, _ := strconv.ParseInt(r.FormValue("category"), 10, 64)
	entry, err := s.finance.Create(r.Context(), actor, finance.Draft{
		Kind: r.FormValue("kind"), Cents: cents, Date: day, CategoryID: categoryID, Note: r.FormValue("note"),
	})
	if err != nil {
		s.showFinErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/finance/"+strconv.FormatInt(entry.ID, 10), http.StatusSeeOther)
}

func (s *server) financeShow(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.financeUser(w, r, false)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	entry, err := s.finance.Get(r.Context(), actor, id)
	if err != nil {
		s.showFinErr(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, page{
		Title: "流水", Body: "finance-entry-body",
		Fin: finView{Entry: entry, CanWrite: finance.CanWrite(actor)},
	})
}

func (s *server) financeVoid(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.financeUser(w, r, true)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	if err := s.finance.Void(r.Context(), actor, id, r.FormValue("reason")); err != nil {
		s.showFinErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/finance/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *server) financeUpload(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.financeUser(w, r, true)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		s.showFinErr(w, r, finance.ErrBadInput)
		return
	}
	defer file.Close()
	if err := s.finance.Attach(r.Context(), actor, id, header.Filename, file); err != nil {
		s.showFinErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/finance/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *server) financeCategory(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.financeUser(w, r, true)
	if !ok {
		return
	}
	if err := s.finance.AddCategory(r.Context(), actor, r.FormValue("name")); err != nil {
		s.showFinErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/finance", http.StatusSeeOther)
}

func (s *server) financeStats(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.financeUser(w, r, false)
	if !ok {
		return
	}
	rows, err := s.finance.Rows(r.Context(), actor)
	if err != nil {
		s.showFinErr(w, r, err)
		return
	}
	cash, savings := finance.BalanceOf(rows)
	s.render(w, r, http.StatusOK, page{
		Title: "财务统计", Body: "finance-stats-body",
		Fin: finView{
			Cash: cash, Savings: savings,
			Months: withTicks(finance.ByMonth(rows)), Groups: withTicks(finance.ByCategory(rows)),
		},
	})
}

func (s *server) financeExport(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.financeUser(w, r, false)
	if !ok {
		return
	}
	raw, err := s.finance.Export(r.Context(), actor, readFinanceFilter(r))
	if err != nil {
		s.showFinErr(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="finance.csv"`)
	_, _ = w.Write(raw)
}

func (s *server) financeShell(r *http.Request, actor finance.Actor) (finView, error) {
	cats, err := s.finance.Categories(r.Context(), actor)
	if err != nil {
		return finView{}, err
	}
	actors, err := s.finance.Actors(r.Context(), actor)
	if err != nil {
		return finView{}, err
	}
	cash, savings, _, err := s.finance.Balance(r.Context(), actor)
	if err != nil {
		return finView{}, err
	}
	return finView{
		Categories: cats, Actors: actors, Cash: cash, Savings: savings, CanWrite: finance.CanWrite(actor),
	}, nil
}

func (s *server) financeUser(w http.ResponseWriter, r *http.Request, write bool) (finance.Actor, bool) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return finance.Actor{}, false
	}
	if s.finance == nil {
		s.internal(w, r, errors.New("财务服务未配置"))
		return finance.Actor{}, false
	}
	actor := finance.Actor{
		ID: user.Session.User.ID, Name: user.Session.Name, Role: user.Session.User.Role,
		Finance: user.Session.User.Finance, Active: user.Session.User.Active, IP: user.IP,
	}
	action := people.ActFinanceRead
	if write {
		action = people.ActFinanceWrite
	}
	if !people.Can(user.Session.User, action, people.Resource{}) {
		s.notFound(w, r)
		return finance.Actor{}, false
	}
	return actor, true
}

func readFinanceFilter(r *http.Request) finance.Filter {
	categoryID, _ := strconv.ParseInt(r.URL.Query().Get("category"), 10, 64)
	actorID, _ := strconv.ParseInt(r.URL.Query().Get("actor"), 10, 64)
	return finance.Filter{
		Month: r.URL.Query().Get("month"), Kind: r.URL.Query().Get("kind"),
		CategoryID: categoryID, ActorID: actorID,
	}
}

func withTicks(sums []finance.Sum) []finBucket {
	var max int64
	values := make([]int64, len(sums))
	for i, sum := range sums {
		values[i] = sum.In
		if sum.Out > values[i] {
			values[i] = sum.Out
		}
		if sum.Save > values[i] {
			values[i] = sum.Save
		}
		if sum.Draw > values[i] {
			values[i] = sum.Draw
		}
		if values[i] > max {
			max = values[i]
		}
	}
	out := make([]finBucket, len(sums))
	for i, sum := range sums {
		n := 0
		if max > 0 && values[i] > 0 {
			n = int(values[i] * 10 / max)
			if n < 1 {
				n = 1
			}
			if n > 10 {
				n = 10
			}
		}
		ticks := make([]bool, 10)
		for t := 0; t < n; t++ {
			ticks[t] = true
		}
		out[i] = finBucket{Sum: sum, Ticks: ticks}
	}
	return out
}

func (s *server) showFinErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, finance.ErrNotFound), errors.Is(err, finance.ErrForbidden):
		s.notFound(w, r)
	case errors.Is(err, finance.ErrBadInput), errors.Is(err, finance.ErrState),
		errors.Is(err, files.ErrBadFile), errors.Is(err, files.ErrTooBig), errors.Is(err, files.ErrPixels), errors.Is(err, files.ErrQuota):
		s.render(w, r, http.StatusBadRequest, page{Title: "提示", Body: "error-body", Status: http.StatusBadRequest, Message: err.Error()})
	default:
		s.internal(w, r, err)
	}
}
