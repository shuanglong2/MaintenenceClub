package web

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/boards"
)

type gatherView struct {
	Board    string
	Base     string
	Items    []boards.Item
	Open     boards.Item
	HasOpen  bool
	Creating bool
	Kinds    []string
	Statuses []string
	People   []boards.Person
	Kind     string
	Status   string
	Query    string
	ShowDone bool
	Leader   bool
	Joined   bool
}

func (s *server) gatherList(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.boardUser(w, r)
	if !ok {
		return
	}
	board := boardOf(r)
	filter := boards.Filter{
		Kind: r.URL.Query().Get("kind"), Status: r.URL.Query().Get("status"),
		Query: strings.TrimSpace(r.URL.Query().Get("q")), ShowDone: r.URL.Query().Get("done") == "1",
	}
	items, err := s.boards.List(r.Context(), actor, board, filter)
	if err != nil {
		s.showBoardErr(w, r, err)
		return
	}
	view := gatherView{
		Board: board, Base: boards.BasePath(board), Items: items, Kinds: boards.Kinds(board), Statuses: boards.Statuses(),
		Kind: filter.Kind, Status: filter.Status, Query: filter.Query, ShowDone: filter.ShowDone, Leader: actor.leader(),
		Creating: actor.leader() && r.URL.Query().Get("new") == "1",
	}
	if raw := r.URL.Query().Get("id"); raw != "" {
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil && id > 0 {
			if item, err := s.boards.Open(r.Context(), actor, id); err == nil && item.Board == board {
				view.Open = item
				view.HasOpen = true
				view.Joined = item.Joined
				view.Creating = false
			}
		}
	}
	if view.Leader && (view.HasOpen || view.Creating) {
		people, err := s.boards.People(r.Context(), actor)
		if err != nil {
			s.showBoardErr(w, r, err)
			return
		}
		view.People = people
	}
	title := "活动"
	if board == boards.Competition {
		title = "竞赛"
	}
	s.render(w, r, http.StatusOK, page{Title: title, Body: "gather-body", Gather: view})
}

func (s *server) gatherCreate(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.boardUser(w, r)
	if !ok {
		return
	}
	if !actor.leader() {
		s.showBoardErr(w, r, boards.ErrForbidden)
		return
	}
	draft, err := readBoardDraft(r)
	if err != nil {
		s.showBoardErr(w, r, err)
		return
	}
	item, err := s.boards.Create(r.Context(), actor, boardOf(r), draft)
	if err != nil {
		s.showBoardErr(w, r, err)
		return
	}
	http.Redirect(w, r, boards.BasePath(item.Board)+"?id="+strconv.FormatInt(item.ID, 10), http.StatusSeeOther)
}

func (s *server) gatherJoin(w http.ResponseWriter, r *http.Request) {
	s.gatherAct(w, r, func(actor boards.Actor, id int64) error {
		return s.boards.Join(r.Context(), actor, id)
	})
}

func (s *server) gatherLeave(w http.ResponseWriter, r *http.Request) {
	s.gatherAct(w, r, func(actor boards.Actor, id int64) error {
		return s.boards.Leave(r.Context(), actor, id)
	})
}

func (s *server) gatherStatus(w http.ResponseWriter, r *http.Request) {
	s.gatherAct(w, r, func(actor boards.Actor, id int64) error {
		return s.boards.SetStatus(r.Context(), actor, id, r.FormValue("status"))
	})
}

func (s *server) gatherAssign(w http.ResponseWriter, r *http.Request) {
	s.gatherAct(w, r, func(actor boards.Actor, id int64) error {
		person, _ := strconv.ParseInt(r.FormValue("person"), 10, 64)
		return s.boards.Assign(r.Context(), actor, id, person)
	})
}

func (s *server) gatherAct(w http.ResponseWriter, r *http.Request, fn func(boards.Actor, int64) error) {
	actor, ok := s.boardUser(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	if err := fn(actor, id); err != nil {
		s.showBoardErr(w, r, err)
		return
	}
	http.Redirect(w, r, boards.BasePath(boardOf(r))+"?id="+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *server) boardUser(w http.ResponseWriter, r *http.Request) (boards.Actor, bool) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return boards.Actor{}, false
	}
	if s.boards == nil {
		s.notFound(w, r)
		return boards.Actor{}, false
	}
	return boards.Actor{
		ID: user.Session.User.ID, Name: user.Session.Name, Role: user.Session.User.Role, Active: user.Session.User.Active, IP: user.IP,
	}, true
}

func boardOf(r *http.Request) string {
	if strings.HasPrefix(r.URL.Path, "/competitions") {
		return boards.Competition
	}
	return boards.Activity
}

func readBoardDraft(r *http.Request) (boards.Draft, error) {
	starts, err := optionalClock(r.FormValue("start_date"), r.FormValue("start_clock"))
	if err != nil {
		return boards.Draft{}, boards.ErrBadInput
	}
	ends, err := optionalClock(r.FormValue("end_date"), r.FormValue("end_clock"))
	if err != nil {
		return boards.Draft{}, boards.ErrBadInput
	}
	return boards.Draft{
		Title: r.FormValue("title"), Summary: r.FormValue("summary"), Contact: r.FormValue("contact"),
		Kind: r.FormValue("kind"), Place: r.FormValue("place"), Starts: starts, Ends: ends,
	}, nil
}

func optionalClock(date, clock string) (*time.Time, error) {
	if date == "" {
		return nil, nil
	}
	if clock == "" {
		clock = "00:00"
	}
	parsed, err := time.ParseInLocation("2006-01-02 15:04", date+" "+clock, time.FixedZone("CST", 8*3600))
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func (v gatherView) Keep() string {
	q := url.Values{}
	if v.Query != "" {
		q.Set("q", v.Query)
	}
	if v.Kind != "" {
		q.Set("kind", v.Kind)
	}
	if v.Status != "" {
		q.Set("status", v.Status)
	}
	if v.ShowDone {
		q.Set("done", "1")
	}
	encoded := q.Encode()
	if encoded == "" {
		return v.Base
	}
	return v.Base + "?" + encoded
}

func (s *server) showBoardErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, boards.ErrNotFound):
		s.notFound(w, r)
	case errors.Is(err, boards.ErrForbidden):
		s.render(w, r, http.StatusForbidden, page{Title: "提示", Body: "error-body", Status: http.StatusForbidden, Message: err.Error()})
	case errors.Is(err, boards.ErrBadInput), errors.Is(err, boards.ErrState):
		s.render(w, r, http.StatusBadRequest, page{Title: "提示", Body: "error-body", Status: http.StatusBadRequest, Message: err.Error()})
	default:
		s.internal(w, r, err)
	}
}
