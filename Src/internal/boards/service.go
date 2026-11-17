package boards

import (
	"context"
	"errors"
	"html"
	"html/template"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/people"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/audit"
)

const (
	Activity      = "activity"
	Competition   = "competition"
	StatusClosed  = "closed"
	StatusOpen    = "open"
	StatusRunning = "running"
	StatusDone    = "done"
)

var (
	activityKinds    = []string{"校内科普", "外出科普", "义务维修", "临时任务"}
	competitionKinds = []string{"学科竞赛", "创新竞赛", "技能竞赛", "其他"}
	statuses         = []string{StatusClosed, StatusOpen, StatusRunning, StatusDone}
)

// Actor 是当前操作的人.
type Actor struct {
	ID     int64
	Name   string
	Role   string
	Active bool
	IP     string
}

func (a Actor) leader() bool {
	return a.Active && a.Role == people.RoleLeader
}

// Person 是参加的人.
type Person struct {
	ID   int64
	Name string
}

// Item 是一场活动或一次竞赛.
type Item struct {
	ID          int64
	Board       string
	Title       string
	Summary     string
	Contact     string
	Kind        string
	Status      string
	Place       string
	Starts      time.Time
	Ends        time.Time
	CreatedBy   int64
	CreatedName string
	Created     time.Time
	Names       string
	Joined      bool
	Headcount   int
}

// Draft 是创建时提交的内容.
type Draft struct {
	Title   string
	Summary string
	Contact string
	Kind    string
	Place   string
	Starts  *time.Time
	Ends    *time.Time
}

// Filter 是列表筛选.
type Filter struct {
	Kind     string
	Status   string
	Query    string
	ShowDone bool
	OpenID   int64
}

// Spot 是放进日程的一条.
type Spot struct {
	ID    int64
	Board string
	Title string
	Start time.Time
	End   time.Time
}

// Service 管理活动和竞赛.
type Service struct {
	pool *pgxpool.Pool
}

// New 创建服务. 网页创建只开放给组长, 以后的接口也走 Create.
func New(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// Kinds 返回这个板块可选的类型.
func Kinds(board string) []string {
	if board == Competition {
		return append([]string(nil), competitionKinds...)
	}
	return append([]string(nil), activityKinds...)
}

// Statuses 是状态顺序.
func Statuses() []string { return append([]string(nil), statuses...) }

// StatusLabel 返回状态名称.
func StatusLabel(status string) string {
	switch status {
	case StatusClosed:
		return "未开放"
	case StatusOpen:
		return "报名中"
	case StatusRunning:
		return "进行中"
	case StatusDone:
		return "已完成"
	default:
		return status
	}
}

// BasePath 是页面地址.
func BasePath(board string) string {
	if board == Competition {
		return "/competitions"
	}
	return "/activities"
}

// List 按筛选返回卡片. 已完成默认不出现.
func (s *Service) List(ctx context.Context, actor Actor, board string, filter Filter) ([]Item, error) {
	if !actor.Active || (board != Activity && board != Competition) {
		return nil, ErrForbidden
	}
	where := []string{"i.board = $1"}
	args := []any{board}
	n := 2
	if filter.Kind != "" {
		if !validKind(board, filter.Kind) {
			return nil, errInput("类型不正确")
		}
		where = append(where, "i.kind = $"+strconv.Itoa(n))
		args = append(args, filter.Kind)
		n++
	}
	if filter.Status != "" {
		if !validStatus(filter.Status) {
			return nil, errInput("状态不正确")
		}
		where = append(where, "i.status = $"+strconv.Itoa(n))
		args = append(args, filter.Status)
		n++
	} else if !filter.ShowDone {
		where = append(where, "i.status <> 'done'")
	}
	if q := strings.TrimSpace(filter.Query); q != "" {
		where = append(where, "i.title ILIKE $"+strconv.Itoa(n)+` ESCAPE '\'`)
		args = append(args, "%"+escapeLike(q)+"%")
		n++
	}
	args = append(args, actor.ID)
	rows, err := s.pool.Query(ctx, `
SELECT i.id, i.board, i.title, i.summary, i.contact, i.kind, i.status, i.place,
       i.starts_at, i.ends_at, i.created_by, i.created_name, i.created_at,
       COALESCE((SELECT string_agg(m.person_name, '、' ORDER BY m.joined_at, m.person_id) FROM boards.members m WHERE m.item_id = i.id), ''),
       EXISTS (SELECT 1 FROM boards.members m WHERE m.item_id = i.id AND m.person_id = $`+strconv.Itoa(n)+`),
       (SELECT count(*) FROM boards.members m WHERE m.item_id = i.id)
FROM boards.items i
WHERE `+strings.Join(where, " AND ")+`
ORDER BY CASE WHEN EXISTS (SELECT 1 FROM boards.members m WHERE m.item_id = i.id AND m.person_id = $`+strconv.Itoa(n)+`) THEN 0 ELSE 1 END,
         i.created_at, i.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// Open 读取一条, 供右侧详情使用.
func (s *Service) Open(ctx context.Context, actor Actor, id int64) (Item, error) {
	if !actor.Active {
		return Item{}, ErrForbidden
	}
	row := s.pool.QueryRow(ctx, `
SELECT i.id, i.board, i.title, i.summary, i.contact, i.kind, i.status, i.place,
       i.starts_at, i.ends_at, i.created_by, i.created_name, i.created_at,
       COALESCE((SELECT string_agg(m.person_name, '、' ORDER BY m.joined_at, m.person_id) FROM boards.members m WHERE m.item_id = i.id), ''),
       EXISTS (SELECT 1 FROM boards.members m WHERE m.item_id = i.id AND m.person_id = $2),
       (SELECT count(*) FROM boards.members m WHERE m.item_id = i.id)
FROM boards.items i WHERE i.id = $1`, id, actor.ID)
	item, err := scanItem(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	return item, err
}

// Create 新建. 目前只允许组长, 以后的机器人调用同一个方法.
func (s *Service) Create(ctx context.Context, actor Actor, board string, draft Draft) (Item, error) {
	if !actor.leader() || (board != Activity && board != Competition) {
		return Item{}, ErrForbidden
	}
	clean, err := clean(board, draft)
	if err != nil {
		return Item{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Item{}, err
	}
	defer tx.Rollback(ctx)
	var id int64
	err = tx.QueryRow(ctx, `
INSERT INTO boards.items (board, title, summary, contact, kind, place, starts_at, ends_at, created_by, created_name)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING id`, board, clean.Title, clean.Summary, clean.Contact, clean.Kind, clean.Place, clean.Starts, clean.Ends, actor.ID, actor.Name).Scan(&id)
	if err != nil {
		return Item{}, err
	}
	if err := audit.Write(ctx, tx, audit.Event{
		ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: "boards.create",
		Entity: "board", EntityID: &id, Summary: "新建 " + clean.Title,
	}); err != nil {
		return Item{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Item{}, err
	}
	return s.Open(ctx, actor, id)
}

// SetStatus 由组长改状态.
func (s *Service) SetStatus(ctx context.Context, actor Actor, id int64, status string) error {
	if !actor.leader() {
		return ErrForbidden
	}
	if !validStatus(status) {
		return errInput("状态不正确")
	}
	tag, err := s.pool.Exec(ctx, `UPDATE boards.items SET status = $2, updated_at = now() WHERE id = $1`, id, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Join 报名参加. 只有报名中可以自己参加.
func (s *Service) Join(ctx context.Context, actor Actor, id int64) error {
	if !actor.Active {
		return ErrForbidden
	}
	var status string
	err := s.pool.QueryRow(ctx, `SELECT status FROM boards.items WHERE id = $1`, id).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if status != StatusOpen {
		return ErrState
	}
	_, err = s.pool.Exec(ctx, `
INSERT INTO boards.members (item_id, person_id, person_name) VALUES ($1, $2, $3)
ON CONFLICT DO NOTHING`, id, actor.ID, actor.Name)
	return err
}

// Leave 退出自己的参加.
func (s *Service) Leave(ctx context.Context, actor Actor, id int64) error {
	if !actor.Active {
		return ErrForbidden
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM boards.members WHERE item_id = $1 AND person_id = $2`, id, actor.ID)
	return err
}

// Assign 由组长把某人记为参加.
func (s *Service) Assign(ctx context.Context, actor Actor, id, personID int64) error {
	if !actor.leader() {
		return ErrForbidden
	}
	var name string
	err := s.pool.QueryRow(ctx, `SELECT name FROM people.people WHERE id = $1 AND status = 'active'`, personID).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return errInput("人员不存在")
	}
	if err != nil {
		return err
	}
	var exists int
	err = s.pool.QueryRow(ctx, `SELECT 1 FROM boards.items WHERE id = $1`, id).Scan(&exists)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
INSERT INTO boards.members (item_id, person_id, person_name) VALUES ($1, $2, $3)
ON CONFLICT DO NOTHING`, id, personID, name)
	return err
}

// People 返回仍在组的人, 供指派.
func (s *Service) People(ctx context.Context, actor Actor) ([]Person, error) {
	if !actor.leader() {
		return nil, ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `SELECT id, name FROM people.people WHERE status = 'active' ORDER BY name, id`)
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

// Spots 是日程上要显示的活动和竞赛. 组员只看自己参加的.
func (s *Service) Spots(ctx context.Context, actor Actor, from, to time.Time) ([]Spot, error) {
	if !actor.Active {
		return nil, ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `
SELECT i.id, i.board, i.title, COALESCE(i.starts_at, i.created_at), COALESCE(i.ends_at, COALESCE(i.starts_at, i.created_at) + interval '1 hour')
FROM boards.items i
WHERE COALESCE(i.starts_at, i.created_at) < $2
  AND COALESCE(i.ends_at, COALESCE(i.starts_at, i.created_at) + interval '1 hour') > $1
  AND ($3::boolean OR EXISTS (SELECT 1 FROM boards.members m WHERE m.item_id = i.id AND m.person_id = $4))
ORDER BY 4, i.id
LIMIT 300`, from, to, actor.leader(), actor.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Spot
	for rows.Next() {
		var spot Spot
		if err := rows.Scan(&spot.ID, &spot.Board, &spot.Title, &spot.Start, &spot.End); err != nil {
			return nil, err
		}
		out = append(out, spot)
	}
	return out, rows.Err()
}

// HomeHTML 是进行中的活动, 最多 8 条.
func (s *Service) HomeHTML(ctx context.Context, actor Actor) (template.HTML, error) {
	if !actor.Active {
		return "", ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `
SELECT id, title, kind FROM boards.items
WHERE board = 'activity' AND status = 'running'
ORDER BY created_at, id
LIMIT 8`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var b strings.Builder
	n := 0
	for rows.Next() {
		var id int64
		var title, kind string
		if err := rows.Scan(&id, &title, &kind); err != nil {
			return "", err
		}
		if n == 0 {
			b.WriteString("<ul>")
		}
		n++
		b.WriteString(`<li><a href="/activities?id=`)
		b.WriteString(strconv.FormatInt(id, 10))
		b.WriteString(`">`)
		b.WriteString(html.EscapeString(title))
		b.WriteString("</a> ")
		b.WriteString(html.EscapeString(kind))
		b.WriteString("</li>")
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if n == 0 {
		return `<p class="empty">现在没有进行中的活动.</p>`, nil
	}
	b.WriteString("</ul>")
	return template.HTML(b.String()), nil
}

func clean(board string, draft Draft) (Draft, error) {
	draft.Title = strings.TrimSpace(draft.Title)
	draft.Summary = strings.TrimSpace(draft.Summary)
	draft.Contact = strings.TrimSpace(draft.Contact)
	draft.Kind = strings.TrimSpace(draft.Kind)
	draft.Place = strings.TrimSpace(draft.Place)
	if draft.Title == "" || utf8.RuneCountInString(draft.Title) > 80 {
		return Draft{}, errInput("标题需要 1 到 80 个字")
	}
	if !validKind(board, draft.Kind) {
		return Draft{}, errInput("类型不正确")
	}
	if utf8.RuneCountInString(draft.Summary) > 4000 || utf8.RuneCountInString(draft.Contact) > 80 || utf8.RuneCountInString(draft.Place) > 80 {
		return Draft{}, errInput("说明, 联系方式或地点太长")
	}
	if draft.Starts != nil && draft.Ends != nil && !draft.Ends.After(*draft.Starts) {
		return Draft{}, errInput("结束时间必须晚于开始时间")
	}
	return draft, nil
}

func validKind(board, kind string) bool {
	for _, item := range Kinds(board) {
		if item == kind {
			return true
		}
	}
	return false
}

func validStatus(status string) bool {
	for _, item := range statuses {
		if item == status {
			return true
		}
	}
	return false
}

func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

type scanner interface {
	Scan(dest ...any) error
}

func scanItem(row scanner) (Item, error) {
	var item Item
	var starts, ends *time.Time
	err := row.Scan(
		&item.ID, &item.Board, &item.Title, &item.Summary, &item.Contact, &item.Kind, &item.Status, &item.Place,
		&starts, &ends, &item.CreatedBy, &item.CreatedName, &item.Created, &item.Names, &item.Joined, &item.Headcount,
	)
	if starts != nil {
		item.Starts = *starts
	}
	if ends != nil {
		item.Ends = *ends
	}
	return item, err
}
