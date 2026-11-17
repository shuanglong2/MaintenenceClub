package web

import (
	"context"
	"errors"
	"html/template"
	"net/http"
	"strconv"
	"time"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/boards"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/events"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/people"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/tasks"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/files"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/mdown"
)

// RegisterEventWidget 把近期活动登记到首页.
func RegisterEventWidget(svc *events.Service) {
	if svc == nil {
		return
	}
	RegisterWidget(Widget{
		Title: "近期活动",
		Allow: func(user people.User) bool {
			return people.Can(user, people.ActEventJoin, people.Resource{})
		},
		Render: func(ctx context.Context, user people.User) (template.HTML, error) {
			return svc.HomeUpcomingHTML(ctx, events.Actor{ID: user.ID, Role: user.Role, Active: user.Active})
		},
	})
}

type eventView struct {
	Event       events.Event
	Days        []events.Day
	Heads       []string
	People      []events.Person
	Upcoming    []events.Event
	Past        []events.Event
	Kinds       []string
	Prev        string
	Next        string
	MonthLabel  string
	Manage      bool
	CanCheck    bool
	Joined      bool
	SelfChecked bool
	Open        bool
	BodyHTML    template.HTML
	SummaryHTML template.HTML
}

func (s *server) eventsMonth(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.eventUser(w, r)
	if !ok {
		return
	}
	now := time.Now().In(events.CST)
	year, month := now.Year(), now.Month()
	if parsed, err := time.ParseInLocation("2006-01", r.URL.Query().Get("month"), events.CST); err == nil {
		year, month = parsed.Year(), parsed.Month()
	}
	days, err := s.events.Month(r.Context(), actor, year, month, s.weekStart)
	if err != nil {
		s.showEventErr(w, r, err)
		return
	}
	days = s.overlaySchedule(r, actor, days)
	cursor := time.Date(year, month, 1, 0, 0, 0, 0, events.CST)
	s.render(w, r, http.StatusOK, page{
		Title: "日程", Body: "events-body",
		Evt: eventView{
			Days: days, Heads: events.Heads(s.weekStart),
			Prev: cursor.AddDate(0, -1, 0).Format("2006-01"), Next: cursor.AddDate(0, 1, 0).Format("2006-01"),
			MonthLabel: cursor.Format("2006年1月"), Manage: eventsEditable(actor),
		},
	})
}

func (s *server) eventsList(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.eventUser(w, r)
	if !ok {
		return
	}
	upcoming, err := s.events.List(r.Context(), actor, false)
	if err != nil {
		s.showEventErr(w, r, err)
		return
	}
	past, err := s.events.List(r.Context(), actor, true)
	if err != nil {
		s.showEventErr(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, page{
		Title: "日程列表", Body: "events-list-body",
		Evt: eventView{Upcoming: upcoming, Past: past, Manage: eventsEditable(actor)},
	})
}

func (s *server) eventsNew(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.eventEditor(w, r)
	if !ok {
		return
	}
	people, err := s.events.People(r.Context(), actor)
	if err != nil {
		s.showEventErr(w, r, err)
		return
	}
	now := time.Now().In(events.CST)
	start := time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, events.CST)
	s.render(w, r, http.StatusOK, page{
		Title: "新建日程", Body: "event-form-body",
		Evt: eventView{
			Event:  events.Event{Kind: "例会", Starts: start, Ends: start.Add(time.Hour), OwnerID: actor.ID},
			People: people, Kinds: events.Kinds(), Manage: true,
		},
	})
}

func (s *server) eventsCreate(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.eventEditor(w, r)
	if !ok {
		return
	}
	draft, err := readEventDraft(r)
	if err != nil {
		s.showEventErr(w, r, err)
		return
	}
	item, err := s.events.Create(r.Context(), actor, draft)
	if err != nil {
		s.showEventErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/events/"+strconv.FormatInt(item.ID, 10), http.StatusSeeOther)
}

func (s *server) eventsShow(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.eventUser(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	item, err := s.events.Get(r.Context(), actor, id)
	if err != nil {
		s.showEventErr(w, r, err)
		return
	}
	joined, selfChecked := false, false
	for _, person := range item.Attendees {
		if person.PersonID == actor.ID {
			joined = true
			selfChecked = person.Checked
		}
	}
	s.render(w, r, http.StatusOK, page{
		Title: item.Title, Body: "event-body",
		Evt: eventView{
			Event: item, Manage: eventsEditable(actor), CanCheck: item.OwnerID == actor.ID || eventsEditable(actor),
			Joined: joined, SelfChecked: selfChecked, Open: item.Open(time.Now()),
			BodyHTML: mdown.Render(item.Body), SummaryHTML: mdown.Render(item.Summary),
		},
	})
}

func (s *server) eventsEdit(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.eventEditor(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	item, err := s.events.Get(r.Context(), actor, id)
	if err != nil {
		s.showEventErr(w, r, err)
		return
	}
	people, err := s.events.People(r.Context(), actor)
	if err != nil {
		s.showEventErr(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, page{
		Title: "编辑活动", Body: "event-form-body", Editing: true,
		Evt: eventView{Event: item, People: people, Kinds: events.Kinds(), Manage: true},
	})
}

func (s *server) eventsUpdate(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.eventEditor(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	draft, err := readEventDraft(r)
	if err != nil {
		s.showEventErr(w, r, err)
		return
	}
	if _, err := s.events.Update(r.Context(), actor, id, draft); err != nil {
		s.showEventErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/events/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *server) eventsSignup(w http.ResponseWriter, r *http.Request) {
	s.eventPost(w, r, func(actor events.Actor, id int64) error {
		return s.events.Signup(r.Context(), actor, id)
	})
}

func (s *server) eventsCancel(w http.ResponseWriter, r *http.Request) {
	s.eventPost(w, r, func(actor events.Actor, id int64) error {
		return s.events.Cancel(r.Context(), actor, id)
	})
}

func (s *server) eventsCheck(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.eventUser(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	personID, err := strconv.ParseInt(r.FormValue("person"), 10, 64)
	if err != nil || personID < 1 {
		s.showEventErr(w, r, events.ErrBadInput)
		return
	}
	if err := s.events.Check(r.Context(), actor, id, personID, r.FormValue("on") == "1"); err != nil {
		s.showEventErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/events/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *server) eventsSummary(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.eventEditor(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	if err := s.events.SaveSummary(r.Context(), actor, id, r.FormValue("summary")); err != nil {
		s.showEventErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/events/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *server) eventsUpload(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.eventEditor(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	store := s.uploads()
	if store == nil {
		s.showEventErr(w, r, events.ErrBadInput)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		s.showEventErr(w, r, events.ErrBadInput)
		return
	}
	defer file.Close()
	meta, err := store.Save(r.Context(), actor.ID, "events", header.Filename, file)
	if err != nil {
		s.showEventErr(w, r, err)
		return
	}
	if err := s.events.Attach(r.Context(), actor, id, meta.ID); err != nil {
		s.showEventErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/events/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *server) eventsDetach(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.eventEditor(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	fileID, err := strconv.ParseInt(r.FormValue("file"), 10, 64)
	if err != nil || fileID < 1 {
		s.showEventErr(w, r, events.ErrBadInput)
		return
	}
	if err := s.events.Detach(r.Context(), actor, id, fileID); err != nil {
		s.showEventErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/events/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *server) eventsCopy(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.eventEditor(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	item, err := s.events.CopyNextWeek(r.Context(), actor, id)
	if err != nil {
		s.showEventErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/events/"+strconv.FormatInt(item.ID, 10), http.StatusSeeOther)
}

func (s *server) eventPost(w http.ResponseWriter, r *http.Request, fn func(events.Actor, int64) error) {
	actor, ok := s.eventUser(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	if err := fn(actor, id); err != nil {
		s.showEventErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/events/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *server) eventUser(w http.ResponseWriter, r *http.Request) (events.Actor, bool) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return events.Actor{}, false
	}
	if s.events == nil {
		s.internal(w, r, errors.New("活动服务未配置"))
		return events.Actor{}, false
	}
	return events.Actor{
		ID: user.Session.User.ID, Name: user.Session.Name, Role: user.Session.User.Role,
		Active: user.Session.User.Active, IP: user.IP,
	}, true
}

func (s *server) eventEditor(w http.ResponseWriter, r *http.Request) (events.Actor, bool) {
	actor, ok := s.eventUser(w, r)
	if !ok {
		return events.Actor{}, false
	}
	if !eventsEditable(actor) {
		s.showEventErr(w, r, events.ErrForbidden)
		return events.Actor{}, false
	}
	return actor, true
}

func eventsEditable(actor events.Actor) bool {
	return actor.Active && (actor.Role == "leader" || actor.Role == "deputy")
}

func readEventDraft(r *http.Request) (events.Draft, error) {
	allDay := r.FormValue("all_day") == "1"
	start, end, err := events.ParseRange(allDay, r.FormValue("start_date"), r.FormValue("start_clock"), r.FormValue("end_date"), r.FormValue("end_clock"))
	if err != nil {
		return events.Draft{}, err
	}
	signup, err := events.ParseOptional(r.FormValue("signup_date"), r.FormValue("signup_clock"))
	if err != nil {
		return events.Draft{}, err
	}
	owner, _ := strconv.ParseInt(r.FormValue("owner"), 10, 64)
	return events.Draft{
		Title: r.FormValue("title"), Kind: r.FormValue("kind"), Place: r.FormValue("place"), Body: r.FormValue("body"),
		AllDay: allDay, Starts: start, Ends: end, OwnerID: owner, Status: r.FormValue("status"), SignupUntil: signup,
		MembersHidden: r.FormValue("members") != "1",
	}, nil
}

func (s *server) overlaySchedule(r *http.Request, actor events.Actor, days []events.Day) []events.Day {
	if len(days) == 0 {
		return days
	}
	from := days[0].Date
	to := days[len(days)-1].Date.AddDate(0, 0, 1)
	var extra []events.Event
	if s.tasks != nil {
		spots, err := s.tasks.Spots(r.Context(), tasks.Actor{ID: actor.ID, Name: actor.Name, Role: actor.Role, Active: actor.Active, IP: actor.IP}, from, to)
		if err == nil {
			for _, spot := range spots {
				extra = append(extra, events.Event{
					Title: spot.Title, Kind: "任务", Href: "/tasks?id=" + strconv.FormatInt(spot.ID, 10),
					Starts: spot.At, Ends: spot.At.Add(time.Hour),
				})
			}
		}
	}
	if s.boards != nil {
		spots, err := s.boards.Spots(r.Context(), boards.Actor{ID: actor.ID, Name: actor.Name, Role: actor.Role, Active: actor.Active, IP: actor.IP}, from, to)
		if err == nil {
			for _, spot := range spots {
				kind := "活动"
				if spot.Board == boards.Competition {
					kind = "竞赛"
				}
				extra = append(extra, events.Event{
					Title: spot.Title, Kind: kind, Href: boards.BasePath(spot.Board) + "?id=" + strconv.FormatInt(spot.ID, 10),
					Starts: spot.Start, Ends: spot.End,
				})
			}
		}
	}
	return events.Place(days, extra)
}

func (s *server) showEventErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, events.ErrNotFound), errors.Is(err, files.ErrNotFound):
		s.notFound(w, r)
	case errors.Is(err, events.ErrForbidden):
		s.render(w, r, http.StatusForbidden, page{Title: "提示", Body: "error-body", Status: http.StatusForbidden, Message: err.Error()})
	case errors.Is(err, events.ErrBadInput), errors.Is(err, events.ErrState),
		errors.Is(err, files.ErrBadFile), errors.Is(err, files.ErrTooBig), errors.Is(err, files.ErrPixels), errors.Is(err, files.ErrQuota):
		s.render(w, r, http.StatusBadRequest, page{Title: "提示", Body: "error-body", Status: http.StatusBadRequest, Message: err.Error()})
	default:
		s.internal(w, r, err)
	}
}
