package web

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/docs"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/events"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/finance"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/inventory"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/logs"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/people"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/tasks"
)

type findView struct {
	Query  string
	Groups []findGroup
}

type findGroup struct {
	Title string
	Hits  []findHit
}

type findHit struct {
	Href  string
	Label string
}

func (s *server) findGroups(r *http.Request, user visitor, query string) ([]findGroup, error) {
	var groups []findGroup
	if s.docs != nil {
		hits, err := s.docs.Search(r.Context(), docs.Actor{
			ID: user.Session.User.ID, Name: user.Session.Name, Role: user.Session.User.Role,
			Active: user.Session.User.Active, IP: user.IP,
		}, query, 0)
		if err != nil && !errors.Is(err, docs.ErrForbidden) {
			return nil, err
		}
		if err == nil {
			group := findGroup{Title: "文档"}
			for _, hit := range hits {
				if len(group.Hits) == 8 {
					break
				}
				group.Hits = append(group.Hits, findHit{Href: "/docs/p/" + strconv.FormatInt(hit.ID, 10), Label: hit.Title})
			}
			groups = append(groups, group)
		}
	}
	if s.inv != nil {
		items, err := s.inv.List(r.Context(), inventory.Actor{
			ID: user.Session.User.ID, Name: user.Session.Name, Role: user.Session.User.Role,
			Active: user.Session.User.Active, IP: user.IP,
		}, "", query)
		if err != nil && !errors.Is(err, inventory.ErrForbidden) {
			return nil, err
		}
		if err == nil {
			group := findGroup{Title: "物品"}
			for _, item := range items {
				if len(group.Hits) == 8 {
					break
				}
				group.Hits = append(group.Hits, findHit{
					Href: "/inventory/items/" + strconv.FormatInt(item.ID, 10), Label: item.Code + " " + item.Name,
				})
			}
			groups = append(groups, group)
		}
	}
	if s.events != nil {
		found, err := s.events.Search(r.Context(), events.Actor{
			ID: user.Session.User.ID, Name: user.Session.Name, Role: user.Session.User.Role,
			Active: user.Session.User.Active, IP: user.IP,
		}, query)
		if err != nil && !errors.Is(err, events.ErrForbidden) {
			return nil, err
		}
		if err == nil {
			group := findGroup{Title: "日程"}
			for _, event := range found {
				group.Hits = append(group.Hits, findHit{Href: "/events/" + strconv.FormatInt(event.ID, 10), Label: event.Title})
			}
			groups = append(groups, group)
		}
	}
	if s.tasks != nil {
		found, _, err := s.tasks.List(r.Context(), tasks.Actor{
			ID: user.Session.User.ID, Name: user.Session.Name, Role: user.Session.User.Role,
			Active: user.Session.User.Active, IP: user.IP,
		}, tasks.Filter{Query: query, Limit: 8})
		if err != nil && !errors.Is(err, tasks.ErrForbidden) {
			return nil, err
		}
		if err == nil {
			group := findGroup{Title: "任务"}
			for _, task := range found {
				group.Hits = append(group.Hits, findHit{Href: "/tasks/" + strconv.FormatInt(task.ID, 10), Label: task.Title})
			}
			groups = append(groups, group)
		}
	}
	if s.logs != nil {
		found, _, err := s.logs.List(r.Context(), logs.Actor{
			ID: user.Session.User.ID, Name: user.Session.Name, Role: user.Session.User.Role,
			Active: user.Session.User.Active, IP: user.IP,
		}, logs.Filter{Query: query, Limit: 8})
		if err != nil && !errors.Is(err, logs.ErrForbidden) {
			return nil, err
		}
		if err == nil {
			group := findGroup{Title: "日志"}
			for _, entry := range found {
				group.Hits = append(group.Hits, findHit{Href: "/logs/" + strconv.FormatInt(entry.ID, 10), Label: entry.Snippet()})
			}
			groups = append(groups, group)
		}
	}
	if s.finance != nil {
		found, _, err := s.finance.List(r.Context(), finance.Actor{
			ID: user.Session.User.ID, Name: user.Session.Name, Role: user.Session.User.Role,
			Finance: user.Session.User.Finance, Active: user.Session.User.Active, IP: user.IP,
		}, finance.Filter{Query: query, Limit: 8})
		if err != nil && !errors.Is(err, finance.ErrForbidden) {
			return nil, err
		}
		if err == nil {
			group := findGroup{Title: "财务"}
			for _, entry := range found {
				label := strings.TrimSpace(entry.Note)
				if label == "" {
					label = finance.KindLabel(entry.Kind)
				}
				group.Hits = append(group.Hits, findHit{Href: "/finance/" + strconv.FormatInt(entry.ID, 10), Label: label})
			}
			groups = append(groups, group)
		}
	}
	if s.people != nil {
		rows, err := s.people.List(r.Context(), user.Session.User)
		if err != nil && !errors.Is(err, people.ErrForbidden) {
			return nil, err
		}
		if err == nil {
			group := findGroup{Title: "人员"}
			needle := strings.ToLower(query)
			for _, row := range rows {
				if len(group.Hits) == 8 {
					break
				}
				if strings.Contains(strings.ToLower(row.Name), needle) || strings.Contains(strings.ToLower(row.Login), needle) {
					group.Hits = append(group.Hits, findHit{Href: "/people/" + strconv.FormatInt(row.ID, 10), Label: row.Name})
				}
			}
			groups = append(groups, group)
		}
	}
	return groups, nil
}
