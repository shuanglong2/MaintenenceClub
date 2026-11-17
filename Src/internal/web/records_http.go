package web

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/boards"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/tasks"
)

type archiveRow struct {
	Kind  string
	Title string
	Meta  string
	Href  string
}

func (s *server) recordsList(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	kind := r.URL.Query().Get("kind")
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	rows := make([]archiveRow, 0)
	if s.tasks != nil && (kind == "" || kind == "任务") {
		actor := tasks.Actor{ID: user.Session.User.ID, Name: user.Session.Name, Role: user.Session.User.Role, Active: user.Session.User.Active, IP: user.IP}
		for _, status := range []string{"done", "cancelled"} {
			items, err := s.tasks.Desk(r.Context(), actor, tasks.Filter{Status: status, Query: query})
			if err != nil {
				continue
			}
			for _, item := range items {
				rows = append(rows, archiveRow{
					Kind: "任务", Title: item.Title, Meta: tasks.StatusLabel(item.Status) + " · " + item.Kind,
					Href: "/tasks?id=" + strconv.FormatInt(item.ID, 10),
				})
			}
		}
	}
	if s.boards != nil && (kind == "" || kind == "活动" || kind == "竞赛") {
		actor := boards.Actor{ID: user.Session.User.ID, Name: user.Session.Name, Role: user.Session.User.Role, Active: user.Session.User.Active, IP: user.IP}
		boardsToRead := []string{boards.Activity, boards.Competition}
		if kind == "活动" {
			boardsToRead = []string{boards.Activity}
		}
		if kind == "竞赛" {
			boardsToRead = []string{boards.Competition}
		}
		for _, board := range boardsToRead {
			items, err := s.boards.List(r.Context(), actor, board, boards.Filter{Status: boards.StatusDone, Query: query})
			if err != nil {
				continue
			}
			label := "活动"
			if board == boards.Competition {
				label = "竞赛"
			}
			for _, item := range items {
				rows = append(rows, archiveRow{
					Kind: label, Title: item.Title, Meta: boards.StatusLabel(item.Status) + " · " + item.Kind,
					Href: boards.BasePath(board) + "?id=" + strconv.FormatInt(item.ID, 10),
				})
			}
		}
	}
	s.render(w, r, http.StatusOK, page{
		Title: "记录", Body: "records-body", Archive: rows, PeopleRole: kind, PeopleDept: query,
	})
}
