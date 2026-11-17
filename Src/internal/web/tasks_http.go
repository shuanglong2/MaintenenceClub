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

	"github.com/shuanglong2/MaintenenceClub/Src/internal/events"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/people"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/mdown"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/tasks"
)

// RegisterTaskWidgets 把未完成和即将到期登记到首页. 每人只看自己负责, 创建或参与的.
func RegisterTaskWidgets(svc *tasks.Service) {
	if svc == nil {
		return
	}
	allow := func(user people.User) bool {
		return people.Can(user, people.ActTaskCreate, people.Resource{})
	}
	actor := func(user people.User) tasks.Actor {
		return tasks.Actor{ID: user.ID, Role: user.Role, Active: user.Active}
	}
	RegisterWidget(Widget{
		Title: "我的未完成任务",
		Allow: allow,
		Render: func(ctx context.Context, user people.User) (template.HTML, error) {
			return svc.HomeOpenHTML(ctx, actor(user))
		},
	})
	RegisterWidget(Widget{
		Title: "即将到期",
		Allow: allow,
		Render: func(ctx context.Context, user people.User) (template.HTML, error) {
			return svc.HomeDueHTML(ctx, actor(user))
		},
	})
}

type taskNote struct {
	Author string
	At     time.Time
	HTML   template.HTML
}

type taskView struct {
	Task       tasks.Task
	Tasks      []taskCard
	Columns    []taskColumn
	People     []taskChoice
	Items      []taskChoice
	Kinds      []string
	Statuses   []string
	Priorities []string
	Events     []taskChoice
	Notes      []taskNote
	BodyHTML   template.HTML
	CanEdit    bool
	Mine       bool
	Kind       string
	Status     string
	Sort       string
	Page       int
	HasNext    bool
	Total      int
}

type taskCard struct {
	tasks.Task
	CanEdit bool
}

type taskColumn struct {
	Status string
	Cards  []taskCard
}

type taskChoice struct {
	ID    int64
	Label string
	On    bool
}

func (v taskView) Prev() int {
	if v.Page < 2 {
		return 1
	}
	return v.Page - 1
}

func (v taskView) Next() int { return v.Page + 1 }

func (v taskView) ListHref(page int) string {
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
	if v.Sort != "" {
		q.Set("sort", v.Sort)
	}
	if page > 1 {
		q.Set("page", strconv.Itoa(page))
	}
	encoded := q.Encode()
	if encoded == "" {
		return "/tasks"
	}
	return "/tasks?" + encoded
}

func (s *server) tasksList(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.taskUser(w, r)
	if !ok {
		return
	}
	filter := tasks.Filter{
		Mine: r.URL.Query().Get("mine") == "1", Kind: r.URL.Query().Get("kind"),
		Status: r.URL.Query().Get("status"), Sort: r.URL.Query().Get("sort"), Limit: 30,
	}
	pageNo := 1
	if raw := r.URL.Query().Get("page"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 1 {
			pageNo = n
		}
	}
	filter.Offset = (pageNo - 1) * filter.Limit
	items, total, err := s.tasks.List(r.Context(), actor, filter)
	if err != nil {
		s.showTaskErr(w, r, err)
		return
	}
	cards := make([]taskCard, 0, len(items))
	for _, item := range items {
		cards = append(cards, taskCard{Task: item, CanEdit: tasks.CanEdit(actor, item)})
	}
	s.render(w, r, http.StatusOK, page{
		Title: "任务", Body: "tasks-body",
		Task: taskView{
			Tasks: cards, Kinds: tasks.Kinds(), Statuses: tasks.Statuses(),
			Mine: filter.Mine, Kind: filter.Kind, Status: filter.Status, Sort: filter.Sort,
			Page: pageNo, HasNext: filter.Offset+len(items) < total, Total: total,
		},
	})
}

func (s *server) tasksBoard(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.taskUser(w, r)
	if !ok {
		return
	}
	data, err := s.taskBoardPage(r, actor)
	if err != nil {
		s.showTaskErr(w, r, err)
		return
	}
	if r.Header.Get("HX-Request") != "" {
		s.renderPart(w, r, "board-body", data)
		return
	}
	s.render(w, r, http.StatusOK, data)
}

func (s *server) taskBoardPage(r *http.Request, actor tasks.Actor) (page, error) {
	columns, err := s.tasks.Board(r.Context(), actor)
	if err != nil {
		return page{}, err
	}
	view := taskView{}
	for _, column := range columns {
		cards := make([]taskCard, 0, len(column.Tasks))
		for _, item := range column.Tasks {
			cards = append(cards, taskCard{Task: item, CanEdit: tasks.CanEdit(actor, item)})
		}
		view.Columns = append(view.Columns, taskColumn{Status: column.Status, Cards: cards})
	}
	return page{Title: "看板", Body: "tasks-board-body", Task: view}, nil
}

func (s *server) tasksNew(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.taskUser(w, r)
	if !ok {
		return
	}
	item := tasks.Task{Kind: "其他", Status: "todo", Priority: "normal", OwnerID: actor.ID}
	if raw := r.URL.Query().Get("event"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id < 1 || s.events == nil {
			s.taskInput(w, r, "活动不存在")
			return
		}
		event, err := s.events.Get(r.Context(), events.Actor{
			ID: actor.ID, Name: actor.Name, Role: actor.Role, Active: actor.Active, IP: actor.IP,
		}, id)
		if err != nil {
			s.taskInput(w, r, "活动不存在")
			return
		}
		item.Title = event.Title
		item.EventID = event.ID
		item.EventTitle = event.Title
	}
	people, items, err := s.taskChoices(r, actor, nil, nil)
	if err != nil {
		s.showTaskErr(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, page{
		Title: "新建任务", Body: "task-form-body",
		Task: taskView{
			Task: item, People: people, Items: items, Events: s.taskEvents(r, actor, item),
			Kinds: tasks.Kinds(), Statuses: tasks.Statuses(), Priorities: tasks.Priorities(),
		},
	})
}

func (s *server) tasksCreate(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.taskUser(w, r)
	if !ok {
		return
	}
	draft, err := readTaskDraft(r)
	if err != nil {
		s.showTaskErr(w, r, err)
		return
	}
	item, err := s.tasks.Create(r.Context(), actor, draft)
	if err != nil {
		s.showTaskErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/tasks/"+strconv.FormatInt(item.ID, 10), http.StatusSeeOther)
}

func (s *server) tasksShow(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.taskUser(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	item, err := s.tasks.Get(r.Context(), actor, id)
	if err != nil {
		s.showTaskErr(w, r, err)
		return
	}
	notes := make([]taskNote, 0, len(item.Comments))
	for _, comment := range item.Comments {
		notes = append(notes, taskNote{Author: comment.Author, At: comment.At, HTML: mdown.Render(comment.Body)})
	}
	s.render(w, r, http.StatusOK, page{
		Title: item.Title, Body: "task-body",
		Task: taskView{Task: item, Notes: notes, BodyHTML: mdown.Render(item.Body), CanEdit: tasks.CanEdit(actor, item)},
	})
}

func (s *server) tasksEdit(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.taskUser(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	item, err := s.tasks.Get(r.Context(), actor, id)
	if err != nil {
		s.showTaskErr(w, r, err)
		return
	}
	if !tasks.CanEdit(actor, item) {
		s.showTaskErr(w, r, tasks.ErrForbidden)
		return
	}
	people, items, err := s.taskChoices(r, actor, item.People, item.Items)
	if err != nil {
		s.showTaskErr(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, page{
		Title: "编辑任务", Body: "task-form-body", Editing: true,
		Task: taskView{
			Task: item, People: people, Items: items, Events: s.taskEvents(r, actor, item),
			Kinds: tasks.Kinds(), Statuses: tasks.Statuses(), Priorities: tasks.Priorities(), CanEdit: true,
		},
	})
}

func (s *server) tasksUpdate(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.taskUser(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	draft, err := readTaskDraft(r)
	if err != nil {
		s.showTaskErr(w, r, err)
		return
	}
	if _, err := s.tasks.Update(r.Context(), actor, id, draft); err != nil {
		s.showTaskErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/tasks/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *server) tasksStatus(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.taskUser(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	if err := s.tasks.SetStatus(r.Context(), actor, id, r.FormValue("status")); err != nil {
		s.showTaskErr(w, r, err)
		return
	}
	if r.Header.Get("HX-Request") != "" {
		data, err := s.taskBoardPage(r, actor)
		if err != nil {
			s.showTaskErr(w, r, err)
			return
		}
		s.renderPart(w, r, "board-body", data)
		return
	}
	next := "/tasks/" + strconv.FormatInt(id, 10)
	if r.FormValue("back") == "board" {
		next = "/tasks/board"
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *server) tasksComment(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.taskUser(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	if err := s.tasks.Comment(r.Context(), actor, id, r.FormValue("body")); err != nil {
		s.showTaskErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/tasks/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *server) taskChoices(r *http.Request, actor tasks.Actor, people []tasks.Person, items []tasks.ItemRef) ([]taskChoice, []taskChoice, error) {
	allPeople, err := s.tasks.People(r.Context(), actor)
	if err != nil {
		return nil, nil, err
	}
	allItems, err := s.tasks.Items(r.Context(), actor)
	if err != nil {
		return nil, nil, err
	}
	chosenPeople := map[int64]bool{}
	for _, person := range people {
		chosenPeople[person.ID] = true
	}
	chosenItems := map[int64]bool{}
	for _, item := range items {
		chosenItems[item.ID] = true
	}
	var personChoices []taskChoice
	for _, person := range allPeople {
		personChoices = append(personChoices, taskChoice{ID: person.ID, Label: person.Name, On: chosenPeople[person.ID]})
	}
	var itemChoices []taskChoice
	for _, item := range allItems {
		itemChoices = append(itemChoices, taskChoice{ID: item.ID, Label: item.Code + " " + item.Name, On: chosenItems[item.ID]})
	}
	return personChoices, itemChoices, nil
}

func (s *server) taskEvents(r *http.Request, actor tasks.Actor, current tasks.Task) []taskChoice {
	var out []taskChoice
	seen := map[int64]bool{}
	if s.events != nil {
		found, err := s.events.List(r.Context(), events.Actor{
			ID: actor.ID, Name: actor.Name, Role: actor.Role, Active: actor.Active, IP: actor.IP,
		}, false)
		if err == nil {
			for _, event := range found {
				label := event.Title
				if label == "" {
					label = strconv.FormatInt(event.ID, 10)
				}
				out = append(out, taskChoice{ID: event.ID, Label: label})
				seen[event.ID] = true
			}
		}
	}
	if current.EventID > 0 && !seen[current.EventID] {
		label := current.EventTitle
		if label == "" {
			label = "活动 " + strconv.FormatInt(current.EventID, 10)
		}
		out = append(out, taskChoice{ID: current.EventID, Label: label})
	}
	return out
}

func (s *server) taskUser(w http.ResponseWriter, r *http.Request) (tasks.Actor, bool) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return tasks.Actor{}, false
	}
	if s.tasks == nil {
		s.internal(w, r, errors.New("任务服务未配置"))
		return tasks.Actor{}, false
	}
	return tasks.Actor{
		ID: user.Session.User.ID, Name: user.Session.Name, Role: user.Session.User.Role,
		Active: user.Session.User.Active, IP: user.IP,
	}, true
}

func readTaskDraft(r *http.Request) (tasks.Draft, error) {
	var due *time.Time
	if raw := r.FormValue("due"); raw != "" {
		parsed, err := time.Parse("2006-01-02", raw)
		if err != nil {
			return tasks.Draft{}, tasks.ErrBadInput
		}
		due = &parsed
	}
	owner, _ := strconv.ParseInt(r.FormValue("owner"), 10, 64)
	var eventID int64
	if raw := strings.TrimSpace(r.FormValue("event")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 1 {
			return tasks.Draft{}, tasks.ErrBadInput
		}
		eventID = parsed
	}
	return tasks.Draft{
		Title: r.FormValue("title"), Body: r.FormValue("body"), Kind: r.FormValue("kind"),
		Status: r.FormValue("status"), Priority: r.FormValue("priority"), OwnerID: owner, Due: due, EventID: eventID,
		People: formIDs(r, "person"), Items: formIDs(r, "item"),
	}, nil
}

func formIDs(r *http.Request, name string) []int64 {
	var out []int64
	for _, raw := range r.Form[name] {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err == nil && id > 0 {
			out = append(out, id)
		}
	}
	return out
}

func (s *server) taskInput(w http.ResponseWriter, r *http.Request, message string) {
	s.render(w, r, http.StatusBadRequest, page{Title: "提示", Body: "error-body", Status: http.StatusBadRequest, Message: message})
}

func (s *server) showTaskErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, tasks.ErrNotFound):
		s.notFound(w, r)
	case errors.Is(err, tasks.ErrForbidden):
		s.render(w, r, http.StatusForbidden, page{Title: "提示", Body: "error-body", Status: http.StatusForbidden, Message: err.Error()})
	case errors.Is(err, tasks.ErrBadInput):
		s.render(w, r, http.StatusBadRequest, page{Title: "提示", Body: "error-body", Status: http.StatusBadRequest, Message: err.Error()})
	default:
		s.internal(w, r, err)
	}
}
