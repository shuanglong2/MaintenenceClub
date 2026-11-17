package web

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/people"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/audit"
)

type auditRow struct {
	ID      int64
	At      string
	Actor   string
	ActorID int64
	IP      string
	Action  string
	Summary string
}

type auditFilter struct {
	Actor  string
	Action string
	Kind   string
	From   string
	To     string
}

func (s *server) actor(user visitor) people.Actor {
	return people.Actor{
		ID: user.Session.User.ID, Name: user.Session.Name, Role: user.Session.User.Role,
		Finance: user.Session.User.Finance, Active: true, IP: user.IP,
	}
}

func (s *server) peopleList(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	rows, err := s.people.List(r.Context(), user.Session.User)
	if err != nil {
		s.showErr(w, r, err)
		return
	}
	role := r.URL.Query().Get("role")
	dept := r.URL.Query().Get("dept")
	manage := people.Can(user.Session.User, people.ActPeopleManage, people.Resource{})
	filtered := make([]people.PersonView, 0, len(rows))
	for _, row := range rows {
		if !manage && (row.Role == people.RoleGuest || row.Role == people.RoleBlacklist) {
			continue
		}
		if role != "" && row.Role != role {
			continue
		}
		if dept != "" && row.Department != dept {
			continue
		}
		filtered = append(filtered, row)
	}
	data := page{Title: "人员", Body: "people-body", People: filtered, Depts: people.Departments(), PeopleRole: role, PeopleDept: dept}
	if raw := r.URL.Query().Get("id"); raw != "" {
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil && id > 0 {
			person, err := s.people.Get(r.Context(), user.Session.User, id)
			if err == nil {
				data.Person = person
				data.HasPerson = true
			}
		}
	}
	s.render(w, r, http.StatusOK, data)
}

func (s *server) peopleNew(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if !people.Can(user.Session.User, people.ActPeopleManage, people.Resource{}) {
		s.showErr(w, r, people.ErrForbidden)
		return
	}
	s.render(w, r, http.StatusOK, page{Title: "新建人员", Body: "person-form-body"})
}

func (s *server) peopleCreate(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	in := formInput(r)
	if strings.TrimSpace(in.Login) == "" {
		in.Login = in.StudentID
	}
	if strings.TrimSpace(in.StudentID) == "" {
		in.StudentID = in.Login
	}
	if in.Password == "" {
		password, genErr := people.InitialPassword()
		if genErr != nil {
			s.internal(w, r, genErr)
			return
		}
		in.Password = password
	}
	password, err := s.people.Create(r.Context(), s.actor(user), in)
	if err != nil {
		s.showErr(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, page{Title: "新建人员", Body: "person-form-body", Notice: "初始密码只显示这一次: " + password})
}

func (s *server) peopleDetail(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	person, err := s.people.Get(r.Context(), user.Session.User, id)
	if err != nil {
		s.showErr(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, page{Title: person.Name, Body: "person-body", Person: person, HasPerson: true})
}

func (s *server) peopleEdit(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if !people.Can(user.Session.User, people.ActPeopleManage, people.Resource{}) {
		s.showErr(w, r, people.ErrForbidden)
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	person, err := s.people.Get(r.Context(), user.Session.User, id)
	if err != nil {
		s.showErr(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, page{Title: "编辑 " + person.Name, Body: "person-form-body", Person: person, HasPerson: true, Editing: true})
}

func (s *server) peopleUpdate(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.people.Update(r.Context(), s.actor(user), id, formInput(r)); err != nil {
		s.showErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/people/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *server) peopleExit(w http.ResponseWriter, r *http.Request) {
	s.peopleStatus(w, r, true)
}

func (s *server) peopleRestore(w http.ResponseWriter, r *http.Request) {
	s.peopleStatus(w, r, false)
}

func (s *server) peopleStatus(w http.ResponseWriter, r *http.Request, exit bool) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var err error
	if exit {
		err = s.people.Exit(r.Context(), s.actor(user), id)
	} else {
		err = s.people.Restore(r.Context(), s.actor(user), id)
	}
	if err != nil {
		s.showErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/people/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *server) peopleReset(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	password, err := s.people.ResetPassword(r.Context(), s.actor(user), id)
	if err != nil {
		s.showErr(w, r, err)
		return
	}
	person, err := s.people.Get(r.Context(), user.Session.User, id)
	if err != nil {
		s.showErr(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, page{
		Title: person.Name, Body: "person-body", Person: person, HasPerson: true,
		Notice: "新密码只显示这一次: " + password,
	})
}

func (s *server) peopleDemote(w http.ResponseWriter, r *http.Request) {
	s.peopleShift(w, r, true)
}

func (s *server) peopleKick(w http.ResponseWriter, r *http.Request) {
	s.peopleShift(w, r, false)
}

func (s *server) peopleShift(w http.ResponseWriter, r *http.Request, demote bool) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var err error
	if demote {
		err = s.people.Demote(r.Context(), s.actor(user), id)
	} else {
		err = s.people.Kick(r.Context(), s.actor(user), id)
	}
	if err != nil {
		s.showErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/people?id="+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *server) peopleImportForm(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if !people.Can(user.Session.User, people.ActPeopleManage, people.Resource{}) {
		s.showErr(w, r, people.ErrForbidden)
		return
	}
	s.render(w, r, http.StatusOK, page{Title: "导入人员", Body: "import-body"})
}

func (s *server) peopleImport(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		s.showErr(w, r, people.ErrBadInput)
		return
	}
	defer file.Close()
	rows, err := people.ParseImport(file)
	if err != nil {
		s.render(w, r, http.StatusBadRequest, page{Title: "导入人员", Body: "import-body", Error: err.Error()})
		return
	}
	created, err := s.people.Import(r.Context(), s.actor(user), rows)
	if err != nil {
		s.showErr(w, r, err)
		return
	}
	body, err := people.PasswordCSV(created)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="people-passwords.csv"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (s *server) account(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	person, err := s.people.Get(r.Context(), user.Session.User, user.Session.User.ID)
	if err != nil {
		s.showErr(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, page{Title: "我的资料", Body: "account-body", Person: person, HasPerson: true})
}

func (s *server) accountPassword(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if r.FormValue("next") != r.FormValue("again") {
		s.showErr(w, r, people.ErrBadInput)
		return
	}
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		s.showErr(w, r, people.ErrNoSession)
		return
	}
	if err := s.people.ChangePassword(r.Context(), user.Session.User.ID, cookie.Value, r.FormValue("current"), r.FormValue("next")); err != nil {
		s.showErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/account", http.StatusSeeOther)
}

func (s *server) accountProfile(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if err := s.people.UpdateSelf(r.Context(), s.actor(user), formInput(r)); err != nil {
		s.showErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/account", http.StatusSeeOther)
}

func (s *server) accountAvatar(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	store := s.uploads()
	if store == nil {
		s.notFound(w, r)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		s.showErr(w, r, people.ErrBadInput)
		return
	}
	defer file.Close()
	meta, err := store.Save(r.Context(), user.Session.User.ID, "people", header.Filename, file)
	if err != nil {
		s.showDocErr(w, r, err)
		return
	}
	if err := s.people.SetAvatar(r.Context(), s.actor(user), meta.ID); err != nil {
		s.showErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/account", http.StatusSeeOther)
}

func (s *server) accountContact(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if err := s.people.UpdateContact(r.Context(), s.actor(user), r.FormValue("contact")); err != nil {
		s.showErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/account", http.StatusSeeOther)
}

func (s *server) auditList(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if !people.Can(user.Session.User, people.ActAuditRead, people.Resource{}) {
		s.showErr(w, r, people.ErrForbidden)
		return
	}
	filter := auditFilter{
		Actor: r.URL.Query().Get("actor"), Action: strings.TrimSpace(r.URL.Query().Get("action")),
		Kind: r.URL.Query().Get("kind"), From: r.URL.Query().Get("from"), To: r.URL.Query().Get("to"),
	}
	query := audit.Query{Action: filter.Action, Prefix: auditPrefix(filter.Kind), Limit: 50}
	if filter.Actor != "" {
		id, err := strconv.ParseInt(filter.Actor, 10, 64)
		if err != nil {
			s.showErr(w, r, people.ErrBadInput)
			return
		}
		query.ActorID = &id
	}
	if from, ok := parseDay(filter.From); ok {
		query.From = &from
	}
	if to, ok := parseDay(filter.To); ok {
		until := to.Add(24 * time.Hour)
		query.Until = &until
	}
	if offset, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && offset > 0 {
		query.Offset = offset
	}
	rows, err := audit.List(r.Context(), s.people.Pool(), query)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	view := make([]auditRow, 0, len(rows))
	for _, row := range rows {
		at := row.At
		if parsed, err := time.Parse(time.RFC3339, row.At); err == nil {
			at = formatTime(parsed)
		}
		view = append(view, auditRow{ID: row.ID, At: at, Actor: row.ActorName, ActorID: row.ActorID, IP: row.IP, Action: row.Action, Summary: row.Summary})
	}
	s.render(w, r, http.StatusOK, page{
		Title: "审计", Body: "audit-body", Audit: view, Filter: filter,
		Offset: query.Offset, Prev: max(0, query.Offset-query.Limit), HasNext: len(view) == query.Limit,
	})
}

func auditPrefix(kind string) string {
	switch kind {
	case "login", "docs", "events", "tasks", "activity", "competition", "inventory", "logs", "people":
		return kind + "."
	default:
		return ""
	}
}

func formInput(r *http.Request) people.Input {
	return people.Input{
		Name: strings.TrimSpace(r.FormValue("name")), Login: r.FormValue("login"), Role: r.FormValue("role"),
		Finance: r.FormValue("finance") == "1", StudentID: r.FormValue("student_id"), ClassName: r.FormValue("class_name"),
		Contact: r.FormValue("contact"), Note: r.FormValue("note"), Password: r.FormValue("password"),
		QQ: r.FormValue("qq"), Phone: r.FormValue("phone"), Bio: r.FormValue("bio"),
		Birthday: r.FormValue("birthday"), Website: r.FormValue("website"), Department: r.FormValue("department"),
	}
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		http.NotFound(w, r)
		return 0, false
	}
	return id, true
}
