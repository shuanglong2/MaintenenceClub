package people

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/audit"
	"golang.org/x/crypto/bcrypt"
)

const (
	idleLifetime     = 12 * time.Hour
	absoluteLifetime = 14 * 24 * time.Hour
	touchEvery       = time.Minute
)

// 学号可以做登录名, 因此允许数字开头. 长度 2 到 32.
var loginPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_]{1,31}$`)

// Service 处理人员, 会话和登录.
type Service struct {
	pool  *pgxpool.Pool
	cost  int
	dummy []byte
	limit *Limiter
}

// Input 是创建或修改人员时提交的字段.
type Input struct {
	Name      string
	Login     string
	Role      string
	Finance   bool
	StudentID string
	Contact   string
	Note      string
	Password  string
}

// Actor 是当前操作的人. 命令行创建首个账号时 ID 为 0.
type Actor struct {
	ID      int64
	Name    string
	Role    string
	Finance bool
	Active  bool
	IP      string
}

func (a Actor) user() User {
	return User{ID: a.ID, Role: a.Role, Finance: a.Finance, Active: a.Active}
}

// Person 是数据库里的完整人员.
type Person struct {
	ID         int64
	Name       string
	Login      string
	Hash       string
	Role       string
	Finance    bool
	StudentID  string
	Contact    string
	Joined     time.Time
	Status     string
	Note       string
	MustChange bool
}

// PersonView 是按查看者权限过滤后的人员.
type PersonView struct {
	ID            int64
	Name          string
	Login         string
	Role          string
	RoleLabel     string
	Finance       bool
	ShowFinance   bool
	StudentID     string
	Contact       string
	Joined        time.Time
	Status        string
	StatusLabel   string
	Note          string
	MustChange    bool
	CanEdit       bool
	ShowSensitive bool
}

// Session 是一次成功的登录.
type Session struct {
	Token      string
	CSRF       string
	User       User
	Name       string
	Login      string
	MustChange bool
}

// New 创建服务. cost 用 ProdCost, 测试用 MinCost.
func New(pool *pgxpool.Pool, cost int) (*Service, error) {
	if cost < bcrypt.MinCost {
		return nil, fmt.Errorf("bcrypt 代价过低")
	}
	dummy, err := bcrypt.GenerateFromPassword([]byte("mclub-dummy-password"), cost)
	if err != nil {
		return nil, err
	}
	return &Service{pool: pool, cost: cost, dummy: dummy, limit: NewLimiter()}, nil
}

// Pool 返回这个服务使用的连接池, 供同一请求里的其他查询复用.
func (s *Service) Pool() *pgxpool.Pool {
	return s.pool
}

// CreateFirst 创建唯一的首个组长, 供命令行使用.
func (s *Service) CreateFirst(ctx context.Context, login, password string) error {
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM people.people`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return ErrExists
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	person, err := s.insert(ctx, tx, Input{
		Name:     login,
		Login:    login,
		Role:     RoleLeader,
		Password: password,
	})
	if err != nil {
		return err
	}
	if err := audit.Write(ctx, tx, audit.Event{
		ActorID: person.ID, ActorName: person.Name, Action: "people.create", Entity: "person",
		EntityID: &person.ID, Summary: "命令行创建首个组长", Detail: map[string]any{"role": RoleLeader},
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Create 由组长新建人员, 并返回只显示一次的初始密码.
func (s *Service) Create(ctx context.Context, actor Actor, in Input) (string, error) {
	if !Can(actor.user(), ActPeopleManage, Resource{}) {
		return "", ErrForbidden
	}
	password := in.Password
	if password == "" {
		var err error
		password, err = RandomPassword()
		if err != nil {
			return "", err
		}
	}
	in.Password = password
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	person, err := s.insert(ctx, tx, in)
	if err != nil {
		return "", err
	}
	if err := s.auditPerson(ctx, tx, actor, "people.create", person.ID, "新建人员 "+person.Name, person); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return password, nil
}

// Update 修改职务和资料. 登录名不变.
func (s *Service) Update(ctx context.Context, actor Actor, id int64, in Input) error {
	if !Can(actor.user(), ActPeopleManage, Resource{}) {
		return ErrForbidden
	}
	role, err := ParseRole(in.Role)
	if err != nil {
		return ErrBadInput
	}
	in.Role = role
	if err := validateText(in); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	leaders, err := s.lockLeaders(ctx, tx)
	if err != nil {
		return err
	}
	current, err := s.loadLocked(ctx, tx, id)
	if err != nil {
		return err
	}
	if err := AllowLeaderChange(leaders, current.Role == RoleLeader && current.Status == StatusActive, in.Role, current.Status); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
UPDATE people.people
SET name = $2, role = $3, finance = $4, student_id = $5, contact = $6, note = $7
WHERE id = $1`, id, in.Name, in.Role, in.Finance, in.StudentID, in.Contact, in.Note)
	if err != nil {
		return err
	}
	current.Name = in.Name
	current.Role = in.Role
	current.Finance = in.Finance
	if err := s.auditPerson(ctx, tx, actor, "people.update", id, "修改人员 "+in.Name, current); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Exit 让人员退出并立刻作废会话.
func (s *Service) Exit(ctx context.Context, actor Actor, id int64) error {
	return s.setStatus(ctx, actor, id, StatusExited, "people.exit", "人员退出")
}

// Restore 恢复已退出的人员.
func (s *Service) Restore(ctx context.Context, actor Actor, id int64) error {
	return s.setStatus(ctx, actor, id, StatusActive, "people.restore", "恢复人员")
}

func (s *Service) setStatus(ctx context.Context, actor Actor, id int64, status, action, summary string) error {
	if !Can(actor.user(), ActPeopleManage, Resource{}) {
		return ErrForbidden
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	leaders, err := s.lockLeaders(ctx, tx)
	if err != nil {
		return err
	}
	current, err := s.loadLocked(ctx, tx, id)
	if err != nil {
		return err
	}
	if err := AllowLeaderChange(leaders, current.Role == RoleLeader && current.Status == StatusActive, current.Role, status); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE people.people SET status = $2 WHERE id = $1`, id, status); err != nil {
		return err
	}
	if status == StatusExited {
		if _, err := tx.Exec(ctx, `DELETE FROM people.sessions WHERE person_id = $1`, id); err != nil {
			return err
		}
	}
	current.Status = status
	if err := s.auditPerson(ctx, tx, actor, action, id, summary+" "+current.Name, current); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ResetPassword 生成新密码, 要求下次登录修改, 并作废全部会话.
func (s *Service) ResetPassword(ctx context.Context, actor Actor, id int64) (string, error) {
	if !Can(actor.user(), ActPeopleManage, Resource{}) {
		return "", ErrForbidden
	}
	return s.reset(ctx, actor, id)
}

// AdminReset 从命令行按登录名重置密码.
func (s *Service) AdminReset(ctx context.Context, login string) (string, error) {
	login, err := normalizeLogin(login)
	if err != nil {
		return "", err
	}
	var id int64
	err = s.pool.QueryRow(ctx, `SELECT id FROM people.people WHERE login_name = $1`, login).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return s.reset(ctx, Actor{ID: id, Name: "命令行", Active: true}, id)
}

func (s *Service) reset(ctx context.Context, actor Actor, id int64) (string, error) {
	password, err := RandomPassword()
	if err != nil {
		return "", err
	}
	hash, err := HashPassword(ctx, password, s.cost)
	if err != nil {
		return "", err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
UPDATE people.people SET password_hash = $2, must_change_password = true WHERE id = $1`, id, hash)
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() == 0 {
		return "", ErrNotFound
	}
	if _, err := tx.Exec(ctx, `DELETE FROM people.sessions WHERE person_id = $1`, id); err != nil {
		return "", err
	}
	if err := audit.Write(ctx, tx, audit.Event{
		ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: "people.reset_password",
		Entity: "person", EntityID: &id, Summary: "重置密码",
	}); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return password, nil
}

// ChangePassword 校验当前密码后修改, 并保留当前会话.
func (s *Service) ChangePassword(ctx context.Context, personID int64, currentToken, current, next string) error {
	if err := ValidatePassword(next); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var hash, name string
	err = tx.QueryRow(ctx, `SELECT password_hash, name FROM people.people WHERE id = $1 FOR UPDATE`, personID).Scan(&hash, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	ok, err := CheckPassword(ctx, hash, current)
	if err != nil {
		return err
	}
	if !ok {
		return ErrBadLogin
	}
	nextHash, err := HashPassword(ctx, next, s.cost)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
UPDATE people.people SET password_hash = $2, must_change_password = false WHERE id = $1`, personID, nextHash); err != nil {
		return err
	}
	keep := hashToken(currentToken)
	if _, err := tx.Exec(ctx, `DELETE FROM people.sessions WHERE person_id = $1 AND token_hash <> $2`, personID, keep); err != nil {
		return err
	}
	if err := audit.Write(ctx, tx, audit.Event{
		ActorID: personID, ActorName: name, Action: "people.password", Entity: "person", EntityID: &personID, Summary: "修改自己的密码",
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// UpdateContact 只允许本人修改联系方式.
func (s *Service) UpdateContact(ctx context.Context, actor Actor, contact string) error {
	if !Can(actor.user(), ActPeopleSelf, Resource{OwnerID: actor.ID}) {
		return ErrForbidden
	}
	contact = strings.TrimSpace(contact)
	if utf8.RuneCountInString(contact) > 80 {
		return ErrBadInput
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE people.people SET contact = $2 WHERE id = $1`, actor.ID, contact); err != nil {
		return err
	}
	if err := audit.Write(ctx, tx, audit.Event{
		ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: "people.contact",
		Entity: "person", EntityID: &actor.ID, Summary: "修改自己的联系方式",
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// List 返回查看者有权看见的字段.
func (s *Service) List(ctx context.Context, viewer User) ([]PersonView, error) {
	if !Can(viewer, ActPeopleList, Resource{}) {
		return nil, ErrForbidden
	}
	rows, err := s.pool.Query(ctx, personSelect+` ORDER BY status, role, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PersonView
	for rows.Next() {
		person, err := scanPerson(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, Visible(viewer, person))
	}
	return out, rows.Err()
}

// Get 读取一个人.
func (s *Service) Get(ctx context.Context, viewer User, id int64) (PersonView, error) {
	if !Can(viewer, ActPeopleList, Resource{}) && viewer.ID != id {
		return PersonView{}, ErrForbidden
	}
	person, err := s.load(ctx, id)
	if err != nil {
		return PersonView{}, err
	}
	return Visible(viewer, person), nil
}

// Login 校验密码并创建会话.
func (s *Service) Login(ctx context.Context, login, password, ip string, now time.Time) (Session, error) {
	key := login + "\n" + ip
	if wait := s.limit.Rejected(key, now); wait > 0 {
		return Session{}, ErrRateLimited
	}
	loginName, err := normalizeLogin(login)
	var person Person
	if err == nil {
		person, err = s.loadByLogin(ctx, loginName)
	}
	hash := string(s.dummy)
	if err == nil {
		hash = person.Hash
	} else if !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrBadInput) {
		return Session{}, err
	}
	ok, checkErr := CheckPassword(ctx, hash, password)
	if checkErr != nil {
		return Session{}, checkErr
	}
	if !ok || person.ID == 0 || person.Status != StatusActive {
		s.limit.Fail(key, now)
		return Session{}, ErrBadLogin
	}
	s.limit.Success(key)
	raw, csrf, err := newSecrets()
	if err != nil {
		return Session{}, err
	}
	_, err = s.pool.Exec(ctx, `
INSERT INTO people.sessions (person_id, token_hash, csrf_token, expires_at, absolute_expires_at)
VALUES ($1, $2, $3, $4, $5)`,
		person.ID, hashToken(raw), csrf, now.Add(idleLifetime), now.Add(absoluteLifetime))
	if err != nil {
		return Session{}, err
	}
	return Session{
		Token: raw, CSRF: csrf, Name: person.Name, Login: person.Login, MustChange: person.MustChange,
		User: User{ID: person.ID, Role: person.Role, Finance: person.Finance, Active: true},
	}, nil
}

// Logout 删除当前会话.
func (s *Service) Logout(ctx context.Context, token string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM people.sessions WHERE token_hash = $1`, hashToken(token))
	return err
}

// Lookup 读取会话并在需要时滑动过期时间.
func (s *Service) Lookup(ctx context.Context, token string, now time.Time) (Session, error) {
	var (
		csrf     string
		expires  time.Time
		absolute time.Time
		seen     time.Time
		person   Person
	)
	err := s.pool.QueryRow(ctx, `
SELECT s.csrf_token, s.expires_at, s.absolute_expires_at, s.last_seen_at,
       p.id, p.name, p.login_name, p.role, p.finance, p.status, p.must_change_password
FROM people.sessions s
JOIN people.people p ON p.id = s.person_id
WHERE s.token_hash = $1`, hashToken(token)).Scan(
		&csrf, &expires, &absolute, &seen,
		&person.ID, &person.Name, &person.Login, &person.Role, &person.Finance, &person.Status, &person.MustChange,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrNoSession
	}
	if err != nil {
		return Session{}, err
	}
	if person.Status != StatusActive || now.After(expires) || now.After(absolute) {
		_ = s.Logout(ctx, token)
		return Session{}, ErrNoSession
	}
	if now.Sub(seen) >= touchEvery {
		next := now.Add(idleLifetime)
		if next.After(absolute) {
			next = absolute
		}
		_, _ = s.pool.Exec(ctx, `
UPDATE people.sessions SET last_seen_at = $2, expires_at = $3 WHERE token_hash = $1`, hashToken(token), now, next)
	}
	return Session{
		Token: token, CSRF: csrf, Name: person.Name, Login: person.Login, MustChange: person.MustChange,
		User: User{ID: person.ID, Role: person.Role, Finance: person.Finance, Active: true},
	}, nil
}

// Import 批量创建人员. 任一行失败则全部回滚.
func (s *Service) Import(ctx context.Context, actor Actor, rows []Imported) ([]Imported, error) {
	if !Can(actor.user(), ActPeopleManage, Resource{}) {
		return nil, ErrForbidden
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	out := make([]Imported, 0, len(rows))
	for _, row := range rows {
		password, err := RandomPassword()
		if err != nil {
			return nil, err
		}
		person, err := s.insert(ctx, tx, Input{Name: row.Name, Login: row.Login, Role: row.Role, Password: password})
		if err != nil {
			return nil, err
		}
		if err := s.auditPerson(ctx, tx, actor, "people.import", person.ID, "导入人员 "+person.Name, person); err != nil {
			return nil, err
		}
		role, _ := ParseRole(row.Role)
		out = append(out, Imported{Name: person.Name, Login: person.Login, Role: role, Password: password})
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) insert(ctx context.Context, tx pgx.Tx, in Input) (Person, error) {
	role, err := ParseRole(in.Role)
	if err != nil {
		return Person{}, ErrBadInput
	}
	login, err := normalizeLogin(in.Login)
	if err != nil {
		return Person{}, err
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		in.Name = login
	}
	if err := validateText(in); err != nil {
		return Person{}, err
	}
	hash, err := HashPassword(ctx, in.Password, s.cost)
	if err != nil {
		return Person{}, err
	}
	var person Person
	err = tx.QueryRow(ctx, `
INSERT INTO people.people (name, login_name, password_hash, role, finance, student_id, contact, note, status)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'active')
RETURNING id, name, login_name, password_hash, role, finance, student_id, contact, joined_at, status, note, must_change_password`,
		in.Name, login, hash, role, in.Finance, strings.TrimSpace(in.StudentID), strings.TrimSpace(in.Contact), strings.TrimSpace(in.Note),
	).Scan(&person.ID, &person.Name, &person.Login, &person.Hash, &person.Role, &person.Finance, &person.StudentID, &person.Contact, &person.Joined, &person.Status, &person.Note, &person.MustChange)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return Person{}, ErrLoginTaken
		}
		return Person{}, err
	}
	return person, nil
}

func (s *Service) auditPerson(ctx context.Context, tx pgx.Tx, actor Actor, action string, id int64, summary string, person Person) error {
	return audit.Write(ctx, tx, audit.Event{
		ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: action, Entity: "person", EntityID: &id,
		Summary: summary, Detail: map[string]any{"role": person.Role, "finance": person.Finance, "status": person.Status},
	})
}

func (s *Service) lockLeaders(ctx context.Context, tx pgx.Tx) (int, error) {
	rows, err := tx.Query(ctx, `SELECT id FROM people.people WHERE role = 'leader' AND status = 'active' FOR UPDATE`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		n++
	}
	return n, rows.Err()
}

func (s *Service) loadLocked(ctx context.Context, tx pgx.Tx, id int64) (Person, error) {
	row := tx.QueryRow(ctx, personSelect+` WHERE id = $1 FOR UPDATE`, id)
	person, err := scanPerson(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Person{}, ErrNotFound
	}
	return person, err
}

func (s *Service) load(ctx context.Context, id int64) (Person, error) {
	person, err := scanPerson(s.pool.QueryRow(ctx, personSelect+` WHERE id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Person{}, ErrNotFound
	}
	return person, err
}

func (s *Service) loadByLogin(ctx context.Context, login string) (Person, error) {
	person, err := scanPerson(s.pool.QueryRow(ctx, personSelect+` WHERE login_name = $1`, login))
	if errors.Is(err, pgx.ErrNoRows) {
		return Person{}, ErrNotFound
	}
	return person, err
}

const personSelect = `
SELECT id, name, login_name, password_hash, role, finance, student_id, contact, joined_at, status, note, must_change_password
FROM people.people`

type scanner interface {
	Scan(dest ...any) error
}

func scanPerson(row scanner) (Person, error) {
	var person Person
	err := row.Scan(&person.ID, &person.Name, &person.Login, &person.Hash, &person.Role, &person.Finance, &person.StudentID, &person.Contact, &person.Joined, &person.Status, &person.Note, &person.MustChange)
	return person, err
}

// Visible 按权限去掉不该看的字段.
func Visible(viewer User, person Person) PersonView {
	view := PersonView{
		ID: person.ID, Name: person.Name, Role: person.Role, RoleLabel: RoleLabel(person.Role),
		Joined: person.Joined, Status: person.Status, StatusLabel: StatusLabel(person.Status),
	}
	view.ShowSensitive = Can(viewer, ActPeopleSensitive, Resource{OwnerID: person.ID})
	view.CanEdit = Can(viewer, ActPeopleManage, Resource{})
	view.ShowFinance = (viewer.Active && (viewer.Role == RoleLeader || viewer.Role == RoleDeputy)) || viewer.ID == person.ID
	if view.ShowSensitive {
		view.StudentID = person.StudentID
		view.Contact = person.Contact
		view.Login = person.Login
		view.Note = person.Note
	}
	if view.ShowFinance {
		view.Finance = person.Finance
	}
	if viewer.ID == person.ID {
		view.MustChange = person.MustChange
		view.Login = person.Login
		view.Contact = person.Contact
		view.StudentID = person.StudentID
	}
	if !view.ShowSensitive && viewer.ID != person.ID {
		view.Note = ""
	}
	return view
}

func validateText(in Input) error {
	if utf8.RuneCountInString(in.Name) < 1 || utf8.RuneCountInString(in.Name) > 40 {
		return ErrBadInput
	}
	if utf8.RuneCountInString(in.StudentID) > 32 || utf8.RuneCountInString(in.Contact) > 80 || utf8.RuneCountInString(in.Note) > 500 {
		return ErrBadInput
	}
	return nil
}

func normalizeLogin(login string) (string, error) {
	login = strings.ToLower(strings.TrimSpace(login))
	if !loginPattern.MatchString(login) {
		return "", ErrBadInput
	}
	return login, nil
}

func newSecrets() (string, string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", "", err
	}
	var csrf [32]byte
	if _, err := rand.Read(csrf[:]); err != nil {
		return "", "", err
	}
	return hex.EncodeToString(raw[:]), hex.EncodeToString(csrf[:]), nil
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
