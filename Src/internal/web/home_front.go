package web

import (
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/docs"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/events"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/people"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/tasks"
)

type noticeView struct {
	ID     int64
	Body   string
	Author string
	At     time.Time
}

type homeView struct {
	Notices []noticeView
	CanPost bool
	Events  template.HTML
	Tasks   template.HTML
}

func (s *server) search(w http.ResponseWriter, r *http.Request) {
	s.front(w, r)
}

func (s *server) front(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if utf8.RuneCountInString(query) > 40 {
		s.render(w, r, http.StatusBadRequest, page{
			Title: "主页", Body: "error-body", Status: http.StatusBadRequest, Message: "搜索词太长",
		})
		return
	}
	find := findView{Query: query}
	if query != "" {
		groups, err := s.findGroups(r, user, query)
		if err != nil {
			s.internal(w, r, err)
			return
		}
		find.Groups = groups
	}
	actor := docs.Actor{
		ID: user.Session.User.ID, Name: user.Session.Name, Role: user.Session.User.Role,
		Active: user.Session.User.Active, IP: user.IP,
	}
	home := homeView{CanPost: people.Can(user.Session.User, people.ActDocsVisibility, people.Resource{})}
	if s.docs != nil {
		notices, err := s.docs.Notices(r.Context(), actor)
		if err != nil {
			slog.Error("读取公告", "err", err)
		} else {
			for _, notice := range notices {
				home.Notices = append(home.Notices, noticeView{
					ID: notice.ID, Body: notice.Body, Author: notice.Author, At: notice.At,
				})
			}
		}
	}
	if s.events != nil {
		html, err := s.events.HomeOngoingHTML(r.Context(), events.Actor{
			ID: user.Session.User.ID, Role: user.Session.User.Role, Active: user.Session.User.Active,
		})
		if err != nil {
			slog.Error("读取进行中的活动", "err", err)
		} else {
			home.Events = html
		}
	}
	if s.tasks != nil {
		html, err := s.tasks.HomeBoardHTML(r.Context(), tasks.Actor{
			ID: user.Session.User.ID, Role: user.Session.User.Role, Active: user.Session.User.Active,
		})
		if err != nil {
			slog.Error("读取未完成任务", "err", err)
		} else {
			home.Tasks = html
		}
	}
	s.render(w, r, http.StatusOK, page{Title: "主页", Body: "home-body", Find: find, Home: home})
}

func (s *server) noticeCreate(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := s.docUser(w, r)
	if !ok {
		return
	}
	if s.docs == nil {
		s.notFound(w, r)
		return
	}
	if err := s.docs.AddNotice(r.Context(), actor, r.FormValue("body")); err != nil {
		s.showDocErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *server) noticeVoid(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := s.docUser(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		s.notFound(w, r)
		return
	}
	if s.docs == nil {
		s.notFound(w, r)
		return
	}
	if err := s.docs.VoidNotice(r.Context(), actor, id); err != nil {
		s.showDocErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
