package events

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/people"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/audit"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/files"
)

const (
	statusPlanned   = "planned"
	statusDone      = "done"
	statusCancelled = "cancelled"
)

var kinds = []string{"科普", "竞赛", "例会", "保养日", "其他"}

// Actor 是当前操作的人.
type Actor struct {
	ID     int64
	Name   string
	Role   string
	Active bool
	IP     string
}

func (a Actor) user() people.User {
	return people.User{ID: a.ID, Role: a.Role, Active: a.Active}
}

// Person 是负责人下拉里的一行.
type Person struct {
	ID   int64
	Name string
}

// Attendee 是报名和签到记录.
type Attendee struct {
	PersonID    int64
	Name        string
	SignedUp    time.Time
	Checked     bool
	CheckedName string
	CheckedAt   time.Time
}

// Attachment 是活动上的图片或 PDF.
type Attachment struct {
	ID   int64
	Name string
	Type string
}

// Event 是一场活动.
type Event struct {
	ID            int64
	Title         string
	Kind          string
	Place         string
	Body          string
	Summary       string
	Starts        time.Time
	Ends          time.Time
	AllDay        bool
	OwnerID       int64
	OwnerName     string
	Status        string
	SignupUntil   *time.Time
	MemberVisible bool
	Href          string
	Attendees     []Attendee
	Files         []Attachment
}

// Draft 是创建或修改时提交的内容.
type Draft struct {
	Title         string
	Kind          string
	Place         string
	Body          string
	AllDay        bool
	Starts        time.Time
	Ends          time.Time
	OwnerID       int64
	Status        string
	SignupUntil   *time.Time
	MembersHidden bool
}

// Service 管理活动.
type Service struct {
	pool  *pgxpool.Pool
	files *files.Store
}

// New 创建服务.
func New(pool *pgxpool.Pool, store *files.Store) *Service {
	return &Service{pool: pool, files: store}
}

// Files 返回上传存储, 供下载使用.
func (s *Service) Files() *files.Store { return s.files }

func edit(actor Actor) bool {
	return people.Can(actor.user(), people.ActEventEdit, people.Resource{})
}

func join(actor Actor) bool {
	return people.Can(actor.user(), people.ActEventJoin, people.Resource{})
}

func canCheck(actor Actor, ownerID int64) bool {
	if actor.ID != 0 && actor.ID == ownerID {
		return true
	}
	return edit(actor)
}

// People 返回仍在组的人员, 供选择负责人.
func (s *Service) People(ctx context.Context, actor Actor) ([]Person, error) {
	if !edit(actor) {
		return nil, ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `
SELECT id, name FROM people.people WHERE status = 'active' ORDER BY name, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Person
	for rows.Next() {
		var person Person
		if err := rows.Scan(&person.ID, &person.Name); err != nil {
			return nil, err
		}
		out = append(out, person)
	}
	return out, rows.Err()
}

// Create 新建活动. 只有管理层可以.
func (s *Service) Create(ctx context.Context, actor Actor, draft Draft) (Event, error) {
	if !edit(actor) {
		return Event{}, ErrForbidden
	}
	clean, err := cleanDraft(draft, true)
	if err != nil {
		return Event{}, err
	}
	if clean.OwnerID == 0 {
		clean.OwnerID = actor.ID
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Event{}, err
	}
	defer tx.Rollback(ctx)
	name, err := personName(ctx, tx, clean.OwnerID)
	if err != nil {
		return Event{}, err
	}
	var id int64
	err = tx.QueryRow(ctx, `
INSERT INTO events.events (
    title, kind, starts_at, ends_at, all_day, place, body, owner_id, owner_name, status, signup_until, created_by, member_visible
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'planned', $10, $11, $12)
RETURNING id`,
		clean.Title, clean.Kind, clean.Starts, clean.Ends, clean.AllDay, clean.Place, clean.Body,
		clean.OwnerID, name, clean.SignupUntil, actor.ID, !clean.MembersHidden,
	).Scan(&id)
	if err != nil {
		return Event{}, err
	}
	if err := audit.Write(ctx, tx, audit.Event{
		ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: "events.create",
		Entity: "event", EntityID: &id, Summary: "创建活动 " + clean.Title,
	}); err != nil {
		return Event{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Event{}, err
	}
	return s.Get(ctx, actor, id)
}

// Update 修改活动资料. 总结走 SaveSummary.
func (s *Service) Update(ctx context.Context, actor Actor, id int64, draft Draft) (Event, error) {
	if !edit(actor) {
		return Event{}, ErrForbidden
	}
	clean, err := cleanDraft(draft, false)
	if err != nil {
		return Event{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Event{}, err
	}
	defer tx.Rollback(ctx)
	if _, err := loadTx(ctx, tx, id); err != nil {
		return Event{}, err
	}
	name, err := personName(ctx, tx, clean.OwnerID)
	if err != nil {
		return Event{}, err
	}
	tag, err := tx.Exec(ctx, `
UPDATE events.events
SET title = $2, kind = $3, starts_at = $4, ends_at = $5, all_day = $6, place = $7, body = $8,
    owner_id = $9, owner_name = $10, status = $11, signup_until = $12, member_visible = $13, updated_at = now()
WHERE id = $1`,
		id, clean.Title, clean.Kind, clean.Starts, clean.Ends, clean.AllDay, clean.Place, clean.Body,
		clean.OwnerID, name, clean.Status, clean.SignupUntil, !clean.MembersHidden,
	)
	if err != nil {
		return Event{}, err
	}
	if tag.RowsAffected() == 0 {
		return Event{}, ErrNotFound
	}
	if err := audit.Write(ctx, tx, audit.Event{
		ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: "events.update",
		Entity: "event", EntityID: &id, Summary: "修改活动 " + clean.Title,
	}); err != nil {
		return Event{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Event{}, err
	}
	return s.Get(ctx, actor, id)
}

// CopyNextWeek 复制一场活动到 7 天之后, 不复制报名和附件.
func (s *Service) CopyNextWeek(ctx context.Context, actor Actor, id int64) (Event, error) {
	if !edit(actor) {
		return Event{}, ErrForbidden
	}
	current, err := s.Get(ctx, actor, id)
	if err != nil {
		return Event{}, err
	}
	draft := Draft{
		Title: current.Title, Kind: current.Kind, Place: current.Place, Body: current.Body,
		AllDay: current.AllDay, Starts: current.Starts.AddDate(0, 0, 7), Ends: current.Ends.AddDate(0, 0, 7),
		OwnerID: current.OwnerID, MembersHidden: !current.MemberVisible,
	}
	if current.SignupUntil != nil {
		next := current.SignupUntil.AddDate(0, 0, 7)
		draft.SignupUntil = &next
	}
	return s.Create(ctx, actor, draft)
}

// Get 读取活动, 含报名和附件.
func (s *Service) Get(ctx context.Context, actor Actor, id int64) (Event, error) {
	if !join(actor) {
		return Event{}, ErrForbidden
	}
	event, err := load(ctx, s.pool, id)
	if err != nil {
		return Event{}, err
	}
	event.Attendees, err = s.attendees(ctx, id)
	if err != nil {
		return Event{}, err
	}
	event.Files, err = s.attachments(ctx, id)
	if err != nil {
		return Event{}, err
	}
	if !edit(actor) && !event.MemberVisible && event.OwnerID != actor.ID {
		seen := false
		for _, person := range event.Attendees {
			if person.PersonID == actor.ID {
				seen = true
			}
		}
		if !seen {
			return Event{}, ErrNotFound
		}
	}
	return event, nil
}

// List 按即将开始或已经结束列出活动.
func (s *Service) List(ctx context.Context, actor Actor, past bool) ([]Event, error) {
	if !join(actor) {
		return nil, ErrForbidden
	}
	seeAll := edit(actor)
	query := `
SELECT id, title, kind, place, body, summary, starts_at, ends_at, all_day, owner_id, owner_name, status, signup_until, member_visible
FROM events.events
WHERE ends_at >= $1 AND status <> 'cancelled'
  AND ($2::boolean OR member_visible OR owner_id = $3 OR EXISTS (SELECT 1 FROM events.attendees a WHERE a.event_id = events.events.id AND a.person_id = $3))
ORDER BY starts_at, id
LIMIT 100`
	if past {
		query = `
SELECT id, title, kind, place, body, summary, starts_at, ends_at, all_day, owner_id, owner_name, status, signup_until, member_visible
FROM events.events
WHERE ends_at < $1
  AND ($2::boolean OR member_visible OR owner_id = $3 OR EXISTS (SELECT 1 FROM events.attendees a WHERE a.event_id = events.events.id AND a.person_id = $3))
ORDER BY starts_at DESC, id DESC
LIMIT 100`
	}
	rows, err := s.pool.Query(ctx, query, time.Now(), seeAll, actor.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

// Month 返回某月的月历, 含跨到邻月格子上的活动.
func (s *Service) Month(ctx context.Context, actor Actor, year int, month time.Month, weekStart time.Weekday) ([]Day, error) {
	if !join(actor) {
		return nil, ErrForbidden
	}
	days := MonthDays(year, month, weekStart)
	if len(days) == 0 {
		return days, nil
	}
	from := days[0].Date
	to := days[len(days)-1].Date.AddDate(0, 0, 1)
	rows, err := s.pool.Query(ctx, `
SELECT id, title, kind, place, body, summary, starts_at, ends_at, all_day, owner_id, owner_name, status, signup_until, member_visible
FROM events.events
WHERE starts_at < $2 AND ends_at > $1
  AND ($3::boolean OR member_visible OR owner_id = $4 OR EXISTS (SELECT 1 FROM events.attendees a WHERE a.event_id = events.events.id AND a.person_id = $4))
ORDER BY starts_at, id
LIMIT 500`, from, to, edit(actor), actor.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Event
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, event)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	placeOnDays(days, items)
	return days, nil
}

// CanSeeFile 判断这个人能否下载挂在活动上的文件.
func (s *Service) CanSeeFile(ctx context.Context, actor Actor, fileID int64) (bool, error) {
	if !join(actor) {
		return false, nil
	}
	var ok bool
	err := s.pool.QueryRow(ctx, `
SELECT EXISTS (SELECT 1 FROM events.event_files WHERE file_id = $1)`, fileID).Scan(&ok)
	return ok, err
}

func cleanDraft(draft Draft, creating bool) (Draft, error) {
	draft.Title = strings.TrimSpace(draft.Title)
	draft.Place = strings.TrimSpace(draft.Place)
	draft.Body = strings.TrimSpace(draft.Body)
	draft.Kind = strings.TrimSpace(draft.Kind)
	if draft.Title == "" || utf8.RuneCountInString(draft.Title) > 80 {
		return Draft{}, errInput("标题需要 1 到 80 个字")
	}
	if !validKind(draft.Kind) {
		return Draft{}, errInput("类型不正确")
	}
	if utf8.RuneCountInString(draft.Place) > 80 || utf8.RuneCountInString(draft.Body) > 8000 {
		return Draft{}, errInput("地点或说明太长")
	}
	if !draft.Ends.After(draft.Starts) {
		return Draft{}, errInput("结束时间必须晚于开始时间")
	}
	if draft.SignupUntil != nil && draft.SignupUntil.After(draft.Starts) {
		return Draft{}, errInput("报名截止不能晚于开始时间")
	}
	if creating || draft.Status == "" {
		draft.Status = statusPlanned
	}
	switch draft.Status {
	case statusPlanned, statusDone, statusCancelled:
	default:
		return Draft{}, errInput("状态不正确")
	}
	return draft, nil
}

// Kinds 是活动类型.
func Kinds() []string {
	return append([]string(nil), kinds...)
}

func validKind(kind string) bool {
	for _, item := range kinds {
		if item == kind {
			return true
		}
	}
	return false
}

func personName(ctx context.Context, tx pgx.Tx, id int64) (string, error) {
	var name string
	err := tx.QueryRow(ctx, `SELECT name FROM people.people WHERE id = $1 AND status = 'active'`, id).Scan(&name)
	if err != nil {
		return "", errInput("负责人不存在")
	}
	return name, nil
}

type queryer interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func load(ctx context.Context, q queryer, id int64) (Event, error) {
	row := q.QueryRow(ctx, `
SELECT id, title, kind, place, body, summary, starts_at, ends_at, all_day, owner_id, owner_name, status, signup_until, member_visible
FROM events.events WHERE id = $1`, id)
	event, err := scanEvent(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return Event{}, ErrNotFound
		}
		return Event{}, err
	}
	return event, nil
}

func loadTx(ctx context.Context, tx pgx.Tx, id int64) (Event, error) {
	return load(ctx, tx, id)
}

type scanner interface {
	Scan(dest ...any) error
}

// Search 按标题, 地点和正文查找活动.
func (s *Service) Search(ctx context.Context, actor Actor, query string) ([]Event, error) {
	if !join(actor) {
		return nil, ErrForbidden
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	pattern := "%" + escapeLike(query) + "%"
	rows, err := s.pool.Query(ctx, `
SELECT id, title, kind, place, body, summary, starts_at, ends_at, all_day, owner_id, owner_name, status, signup_until, member_visible
FROM events.events
WHERE (title ILIKE $1 ESCAPE '\' OR place ILIKE $1 ESCAPE '\' OR body ILIKE $1 ESCAPE '\')
  AND ($2::boolean OR member_visible OR owner_id = $3 OR EXISTS (SELECT 1 FROM events.attendees a WHERE a.event_id = events.events.id AND a.person_id = $3))
ORDER BY starts_at DESC, id DESC
LIMIT 8`, pattern, edit(actor), actor.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		event, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func scanEvent(row scanner) (Event, error) {
	var event Event
	err := row.Scan(
		&event.ID, &event.Title, &event.Kind, &event.Place, &event.Body, &event.Summary,
		&event.Starts, &event.Ends, &event.AllDay, &event.OwnerID, &event.OwnerName, &event.Status, &event.SignupUntil, &event.MemberVisible,
	)
	return event, err
}

func (e Event) StartDate() string { return e.Starts.In(CST).Format("2006-01-02") }

func (e Event) StartClock() string {
	if e.AllDay || e.Starts.IsZero() {
		return ""
	}
	return e.Starts.In(CST).Format("15:04")
}

func (e Event) EndDate() string {
	if e.Ends.IsZero() {
		return ""
	}
	end := e.Ends.In(CST)
	if e.AllDay {
		end = end.Add(-time.Second)
	}
	return end.Format("2006-01-02")
}

func (e Event) EndClock() string {
	if e.AllDay || e.Ends.IsZero() {
		return ""
	}
	return e.Ends.In(CST).Format("15:04")
}

func (e Event) SignupDate() string {
	if e.SignupUntil == nil {
		return ""
	}
	return e.SignupUntil.In(CST).Format("2006-01-02")
}

func (e Event) SignupClock() string {
	if e.SignupUntil == nil {
		return ""
	}
	return e.SignupUntil.In(CST).Format("15:04")
}

func (e Event) SignupText() string {
	if e.SignupUntil == nil {
		return ""
	}
	return e.SignupUntil.In(CST).Format("2006-01-02 15:04")
}

func (e Event) Open(now time.Time) bool {
	if e.Status != statusPlanned {
		return false
	}
	deadline := e.Starts
	if e.SignupUntil != nil {
		deadline = *e.SignupUntil
	}
	return now.Before(deadline)
}
