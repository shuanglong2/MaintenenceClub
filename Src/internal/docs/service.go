package docs

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/people"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/audit"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/files"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/search"
)

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

// Page 是一篇文档.
type Page struct {
	ID         int64
	FolderID   int64
	Title      string
	Body       string
	Visibility string
	Version    int
	Updated    time.Time
	Voided     bool
	VoidReason string
	Tags       []string
}

// PageLink 是目录里的一篇文档.
type PageLink struct {
	ID         int64
	Title      string
	Visibility string
}

// FolderNode 是目录树的一层.
type FolderNode struct {
	ID       int64
	Name     string
	ParentID int64
	Sort     int
	Tone     string
	Mark     string
	Children []FolderNode
	Pages    []PageLink
}

// FolderChoice 是编辑页上的目录选项.
type FolderChoice struct {
	ID   int64
	Name string
}

// Version 是一次保存的快照.
type Version struct {
	Version int
	Title   string
	Body    string
	Editor  string
	Saved   time.Time
}

// Hit 是一条搜索结果.
type Hit struct {
	ID      int64
	Title   string
	Snippet string
}

// Draft 是编辑表单.
type Draft struct {
	ID         int64
	Version    int
	FolderID   int64
	Title      string
	Body       string
	Visibility string
	Tags       string
}

// Conflict 表示保存时版本已经变了.
type Conflict struct {
	CurrentVersion int
	Base           string
	Mine           string
	Theirs         string
	Merge          string
	Diff           string
}

// Outcome 是保存的结果. Conflict 非空时没有写入.
type Outcome struct {
	Page     Page
	Conflict *Conflict
}

// Service 处理文档.
type Service struct {
	pool  *pgxpool.Pool
	files *files.Store
}

// New 创建文档服务.
func New(pool *pgxpool.Pool, store *files.Store) *Service {
	return &Service{pool: pool, files: store}
}

// Files 返回文件存储, 供下载授权复用.
func (s *Service) Files() *files.Store {
	return s.files
}

func management(actor Actor) bool {
	return people.Can(actor.user(), people.ActDocsVisibility, people.Resource{})
}

// Tree 返回这个人能看到的目录和未作废页面.
func (s *Service) Tree(ctx context.Context, actor Actor) ([]FolderNode, []PageLink, error) {
	if !people.Can(actor.user(), people.ActDocsEdit, people.Resource{}) {
		return nil, nil, ErrForbidden
	}
	folders, err := s.folders(ctx)
	if err != nil {
		return nil, nil, err
	}
	pages, err := s.visiblePages(ctx, actor, false)
	if err != nil {
		return nil, nil, err
	}
	byParent := map[int64][]FolderNode{}
	nodes := map[int64]*FolderNode{}
	order := make([]int64, 0, len(folders))
	for _, folder := range folders {
		item := folder
		nodes[folder.ID] = &item
		order = append(order, folder.ID)
	}
	for _, id := range order {
		node := nodes[id]
		byParent[node.ParentID] = append(byParent[node.ParentID], *node)
	}
	pageByFolder := map[int64][]PageLink{}
	var loose []PageLink
	for _, page := range pages {
		link := PageLink{ID: page.ID, Title: page.Title, Visibility: page.Visibility}
		if page.FolderID == 0 {
			loose = append(loose, link)
			continue
		}
		pageByFolder[page.FolderID] = append(pageByFolder[page.FolderID], link)
	}
	var build func(parent int64) []FolderNode
	build = func(parent int64) []FolderNode {
		list := byParent[parent]
		for i := range list {
			list[i].Pages = pageByFolder[list[i].ID]
			list[i].Children = build(list[i].ID)
		}
		return list
	}
	return build(0), loose, nil
}

// Folders 返回全部目录, 供下拉框使用.
func (s *Service) Folders(ctx context.Context, actor Actor) ([]FolderChoice, error) {
	if !people.Can(actor.user(), people.ActDocsEdit, people.Resource{}) {
		return nil, ErrForbidden
	}
	nodes, err := s.folders(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]FolderNode, len(nodes))
	for _, node := range nodes {
		byID[node.ID] = node
	}
	var path func(int64, int) string
	path = func(id int64, guard int) string {
		node, ok := byID[id]
		if !ok || guard > 12 {
			return ""
		}
		if node.ParentID == 0 {
			return node.Name
		}
		parent := path(node.ParentID, guard+1)
		if parent == "" {
			return node.Name
		}
		return parent + " / " + node.Name
	}
	out := make([]FolderChoice, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, FolderChoice{ID: node.ID, Name: path(node.ID, 0)})
	}
	slices.SortFunc(out, func(a, b FolderChoice) int {
		return strings.Compare(a.Name, b.Name)
	})
	return out, nil
}

// Voided 返回已作废页面, 只给管理层.
func (s *Service) Voided(ctx context.Context, actor Actor) ([]PageLink, error) {
	if !management(actor) {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
SELECT id, title, visibility FROM docs.pages
WHERE voided_at IS NOT NULL ORDER BY voided_at DESC LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PageLink
	for rows.Next() {
		var item PageLink
		if err := rows.Scan(&item.ID, &item.Title, &item.Visibility); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// Get 读取一篇这个人能看到的文档.
func (s *Service) Get(ctx context.Context, actor Actor, id int64) (Page, error) {
	page, err := s.load(ctx, id)
	if err != nil {
		return Page{}, err
	}
	if !canSee(actor, page) {
		return Page{}, ErrNotFound
	}
	tags, err := s.tagsOf(ctx, id)
	if err != nil {
		return Page{}, err
	}
	page.Tags = tags
	return page, nil
}

// Save 新建或更新. 版本不一致时返回冲突, 不写入.
func (s *Service) Save(ctx context.Context, actor Actor, draft Draft) (Outcome, error) {
	if !people.Can(actor.user(), people.ActDocsEdit, people.Resource{}) {
		return Outcome{}, ErrForbidden
	}
	title := strings.TrimSpace(draft.Title)
	if title == "" || utf8.RuneCountInString(title) > 80 || len(draft.Body) > 200000 {
		return Outcome{}, ErrBadInput
	}
	visibility := draft.Visibility
	if visibility == "" {
		visibility = "all"
	}
	if visibility != "all" && visibility != "management" {
		return Outcome{}, ErrBadInput
	}
	if visibility == "management" && !management(actor) {
		return Outcome{}, ErrForbidden
	}
	tags, err := parseTags(draft.Tags)
	if err != nil {
		return Outcome{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Outcome{}, err
	}
	defer tx.Rollback(ctx)
	if draft.FolderID != 0 {
		var exists int
		if err := tx.QueryRow(ctx, `SELECT 1 FROM docs.folders WHERE id = $1`, draft.FolderID).Scan(&exists); err != nil {
			return Outcome{}, ErrBadInput
		}
	}
	if draft.ID == 0 {
		page, err := s.insert(ctx, tx, actor, draft, title, visibility)
		if err != nil {
			return Outcome{}, err
		}
		if err := s.replaceTags(ctx, tx, page.ID, tags); err != nil {
			return Outcome{}, err
		}
		if err := s.replaceFiles(ctx, tx, page.ID, draft.Body); err != nil {
			return Outcome{}, err
		}
		if err := audit.Write(ctx, tx, audit.Event{
			ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: "docs.create",
			Entity: "page", EntityID: &page.ID, Summary: "新建文档 " + page.Title,
		}); err != nil {
			return Outcome{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return Outcome{}, err
		}
		page.Tags = tags
		return Outcome{Page: page}, nil
	}
	current, err := loadTx(ctx, tx, draft.ID)
	if err != nil {
		return Outcome{}, err
	}
	if !canSee(actor, current) {
		return Outcome{}, ErrNotFound
	}
	if current.Voided {
		return Outcome{}, ErrBadInput
	}
	if !management(actor) {
		visibility = current.Visibility
	}
	if draft.Version != current.Version {
		base, err := versionBody(ctx, tx, current.ID, draft.Version)
		if err != nil {
			base = ""
		}
		return Outcome{Conflict: &Conflict{
			CurrentVersion: current.Version,
			Base:           base,
			Mine:           draft.Body,
			Theirs:         current.Body,
			Merge:          Merge(base, draft.Body, current.Body),
			Diff:           DiffText(base, draft.Body) + "\n\n" + DiffText(base, current.Body),
		}}, nil
	}
	tokens := search.Join(title + "\n" + draft.Body)
	var folder any
	if draft.FolderID != 0 {
		folder = draft.FolderID
	}
	var page Page
	err = tx.QueryRow(ctx, `
UPDATE docs.pages
SET title = $1, body = $2, search_tokens = $3, folder_id = $4, visibility = $5,
    version = version + 1, updated_at = now(), updated_by = $6
WHERE id = $7 AND version = $8 AND voided_at IS NULL
RETURNING id, coalesce(folder_id, 0), title, body, visibility, version, updated_at, voided_at IS NOT NULL, void_reason`,
		title, draft.Body, tokens, folder, visibility, actor.ID, draft.ID, draft.Version,
	).Scan(&page.ID, &page.FolderID, &page.Title, &page.Body, &page.Visibility, &page.Version, &page.Updated, &page.Voided, &page.VoidReason)
	if errors.Is(err, pgx.ErrNoRows) {
		return Outcome{}, ErrNotFound
	}
	if err != nil {
		return Outcome{}, err
	}
	if err := insertVersion(ctx, tx, page, actor); err != nil {
		return Outcome{}, err
	}
	if err := s.replaceTags(ctx, tx, page.ID, tags); err != nil {
		return Outcome{}, err
	}
	if err := s.replaceFiles(ctx, tx, page.ID, page.Body); err != nil {
		return Outcome{}, err
	}
	if err := audit.Write(ctx, tx, audit.Event{
		ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: "docs.update",
		Entity: "page", EntityID: &page.ID, Summary: "修改文档 " + page.Title,
	}); err != nil {
		return Outcome{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Outcome{}, err
	}
	page.Tags = tags
	return Outcome{Page: page}, nil
}

// Void 作废一篇文档.
func (s *Service) Void(ctx context.Context, actor Actor, id int64, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" || utf8.RuneCountInString(reason) > 200 {
		return ErrBadInput
	}
	return s.setVoid(ctx, actor, id, reason, true)
}

// Unvoid 恢复一篇作废文档, 仅管理层.
func (s *Service) Unvoid(ctx context.Context, actor Actor, id int64) error {
	if !management(actor) {
		return ErrForbidden
	}
	return s.setVoid(ctx, actor, id, "", false)
}

// History 返回全部快照, 新的在前.
func (s *Service) History(ctx context.Context, actor Actor, id int64) ([]Version, error) {
	if _, err := s.Get(ctx, actor, id); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
SELECT version, title, body, editor_name, saved_at
FROM docs.page_versions WHERE page_id = $1 ORDER BY version DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Version
	for rows.Next() {
		var item Version
		if err := rows.Scan(&item.Version, &item.Title, &item.Body, &item.Editor, &item.Saved); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// Version 读取一个快照.
func (s *Service) Version(ctx context.Context, actor Actor, id int64, version int) (Version, error) {
	if _, err := s.Get(ctx, actor, id); err != nil {
		return Version{}, err
	}
	var item Version
	err := s.pool.QueryRow(ctx, `
SELECT version, title, body, editor_name, saved_at
FROM docs.page_versions WHERE page_id = $1 AND version = $2`, id, version).Scan(
		&item.Version, &item.Title, &item.Body, &item.Editor, &item.Saved)
	if errors.Is(err, pgx.ErrNoRows) {
		return Version{}, ErrNotFound
	}
	return item, err
}

// Search 按标题和正文查找.
func (s *Service) Search(ctx context.Context, actor Actor, query string, offset int) ([]Hit, error) {
	if !people.Can(actor.user(), people.ActDocsEdit, people.Resource{}) {
		return nil, ErrForbidden
	}
	if offset < 0 {
		offset = 0
	}
	like, tsquery := search.Query(query)
	mgmt := management(actor)
	var (
		rows pgx.Rows
		err  error
	)
	if strings.TrimSpace(query) == "" {
		rows, err = s.pool.Query(ctx, `
SELECT id, title, body FROM docs.pages
WHERE voided_at IS NULL AND ($1 OR visibility = 'all')
ORDER BY updated_at DESC LIMIT 21 OFFSET $2`, mgmt, offset)
	} else if like != "" {
		pattern := "%" + escapeLike(like) + "%"
		rows, err = s.pool.Query(ctx, `
SELECT id, title, body FROM docs.pages
WHERE voided_at IS NULL AND ($1 OR visibility = 'all')
  AND (title ILIKE $2 ESCAPE '\' OR body ILIKE $2 ESCAPE '\')
ORDER BY updated_at DESC LIMIT 21 OFFSET $3`, mgmt, pattern, offset)
	} else if tsquery != "" {
		rows, err = s.pool.Query(ctx, `
SELECT id, title, body FROM docs.pages
WHERE voided_at IS NULL AND ($1 OR visibility = 'all')
  AND tsv @@ to_tsquery('simple', $2)
ORDER BY ts_rank(tsv, to_tsquery('simple', $2)) DESC, id DESC
LIMIT 21 OFFSET $3`, mgmt, tsquery, offset)
	} else {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Hit
	for rows.Next() {
		var item Hit
		var body string
		if err := rows.Scan(&item.ID, &item.Title, &body); err != nil {
			return nil, err
		}
		item.Snippet = search.Snippet(item.Title+"\n"+body, query)
		out = append(out, item)
	}
	return out, rows.Err()
}

// CanDownload 判断这个人能否下载该文件.
func (s *Service) CanDownload(ctx context.Context, actor Actor, fileID int64) (bool, error) {
	if !actor.Active {
		return false, nil
	}
	var linked bool
	err := s.pool.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1 FROM docs.page_files pf
    JOIN docs.pages p ON p.id = pf.page_id
    WHERE pf.file_id = $1 AND p.voided_at IS NULL AND ($2 OR p.visibility = 'all')
)`, fileID, management(actor)).Scan(&linked)
	if err != nil {
		return false, err
	}
	if linked {
		return true, nil
	}
	var uploader int64
	var anyLink bool
	err = s.pool.QueryRow(ctx, `
SELECT f.uploaded_by, EXISTS (SELECT 1 FROM docs.page_files pf WHERE pf.file_id = f.id)
FROM files.files f WHERE f.id = $1`, fileID).Scan(&uploader, &anyLink)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return !anyLink && uploader == actor.ID, nil
}

// CreateFolder 新建目录.
func folderTone(value string) string {
	switch value {
	case "copper", "blue", "red", "gold", "plum":
		return value
	default:
		return "ink"
	}
}

func folderMark(value string) string {
	switch value {
	case "book", "wrench", "flask", "chip", "note":
		return value
	default:
		return "folder"
	}
}

func (s *Service) CreateFolder(ctx context.Context, actor Actor, name string, parent int64, tone, mark string) (int64, error) {
	if !people.Can(actor.user(), people.ActDocsEdit, people.Resource{}) {
		return 0, ErrForbidden
	}
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 40 {
		return 0, ErrBadInput
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if parent != 0 {
		if err := s.folderExists(ctx, tx, parent); err != nil {
			return 0, err
		}
	}
	var id int64
	var parentArg any
	if parent != 0 {
		parentArg = parent
	}
	if err := tx.QueryRow(ctx, `
INSERT INTO docs.folders (parent_id, name, tone, mark) VALUES ($1, $2, $3, $4) RETURNING id`,
		parentArg, name, folderTone(tone), folderMark(mark)).Scan(&id); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return id, nil
}

// UpdateFolder 改名或移动目录.
func (s *Service) UpdateFolder(ctx context.Context, actor Actor, id int64, name string, parent int64, tone, mark string) error {
	if !people.Can(actor.user(), people.ActDocsEdit, people.Resource{}) {
		return ErrForbidden
	}
	name = strings.TrimSpace(name)
	if id == 0 || name == "" || utf8.RuneCountInString(name) > 40 || parent == id {
		return ErrBadInput
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := s.folderExists(ctx, tx, id); err != nil {
		return err
	}
	if parent != 0 {
		if err := assertNoCycle(ctx, tx, id, parent); err != nil {
			return err
		}
	}
	var parentArg any
	if parent != 0 {
		parentArg = parent
	}
	tag, err := tx.Exec(ctx, `
UPDATE docs.folders SET name = $2, parent_id = $3, tone = $4, mark = $5 WHERE id = $1`,
		id, name, parentArg, folderTone(tone), folderMark(mark))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return tx.Commit(ctx)
}

func (s *Service) setVoid(ctx context.Context, actor Actor, id int64, reason string, void bool) error {
	if !people.Can(actor.user(), people.ActDocsEdit, people.Resource{}) {
		return ErrForbidden
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	page, err := loadTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if void && !canSee(actor, page) {
		return ErrNotFound
	}
	if !void && !management(actor) {
		return ErrForbidden
	}
	action := "docs.restore"
	summary := "恢复文档 " + page.Title
	if void {
		action = "docs.void"
		summary = "作废文档 " + page.Title
		if _, err := tx.Exec(ctx, `
UPDATE docs.pages SET voided_at = now(), void_reason = $2, voided_by = $3 WHERE id = $1`, id, reason, actor.ID); err != nil {
			return err
		}
	} else if _, err := tx.Exec(ctx, `
UPDATE docs.pages SET voided_at = NULL, void_reason = '', voided_by = NULL WHERE id = $1`, id); err != nil {
		return err
	}
	if err := audit.Write(ctx, tx, audit.Event{
		ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: action,
		Entity: "page", EntityID: &id, Summary: summary,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) insert(ctx context.Context, tx pgx.Tx, actor Actor, draft Draft, title, visibility string) (Page, error) {
	tokens := search.Join(title + "\n" + draft.Body)
	var folder any
	if draft.FolderID != 0 {
		folder = draft.FolderID
	}
	var page Page
	err := tx.QueryRow(ctx, `
INSERT INTO docs.pages (folder_id, title, body, search_tokens, visibility, version, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, 1, $6, $6)
RETURNING id, coalesce(folder_id, 0), title, body, visibility, version, updated_at, false::boolean, ''::text`,
		folder, title, draft.Body, tokens, visibility, actor.ID,
	).Scan(&page.ID, &page.FolderID, &page.Title, &page.Body, &page.Visibility, &page.Version, &page.Updated, &page.Voided, &page.VoidReason)
	if err != nil {
		return Page{}, err
	}
	if err := insertVersion(ctx, tx, page, actor); err != nil {
		return Page{}, err
	}
	return page, nil
}

func insertVersion(ctx context.Context, tx pgx.Tx, page Page, actor Actor) error {
	_, err := tx.Exec(ctx, `
INSERT INTO docs.page_versions (page_id, version, title, body, editor_id, editor_name)
VALUES ($1, $2, $3, $4, $5, $6)`, page.ID, page.Version, page.Title, page.Body, actor.ID, actor.Name)
	return err
}

func (s *Service) replaceTags(ctx context.Context, tx pgx.Tx, pageID int64, tags []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM docs.page_tags WHERE page_id = $1`, pageID); err != nil {
		return err
	}
	for _, tag := range tags {
		var id int64
		if err := tx.QueryRow(ctx, `
INSERT INTO docs.tags (name) VALUES ($1)
ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
RETURNING id`, tag).Scan(&id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO docs.page_tags (page_id, tag_id) VALUES ($1, $2)`, pageID, id); err != nil {
			return err
		}
	}
	return nil
}

var fileRef = regexp.MustCompile(`/files/([0-9]+)`)

func (s *Service) replaceFiles(ctx context.Context, tx pgx.Tx, pageID int64, body string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM docs.page_files WHERE page_id = $1`, pageID); err != nil {
		return err
	}
	seen := map[int64]bool{}
	for _, match := range fileRef.FindAllStringSubmatch(body, -1) {
		id, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil || seen[id] {
			continue
		}
		seen[id] = true
		if _, err := tx.Exec(ctx, `
INSERT INTO docs.page_files (page_id, file_id)
SELECT $1, id FROM files.files WHERE id = $2`, pageID, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) load(ctx context.Context, id int64) (Page, error) {
	return loadRow(s.pool.QueryRow(ctx, pageSelect+` WHERE id = $1`, id))
}

func loadTx(ctx context.Context, tx pgx.Tx, id int64) (Page, error) {
	return loadRow(tx.QueryRow(ctx, pageSelect+` WHERE id = $1`, id))
}

const pageSelect = `
SELECT id, coalesce(folder_id, 0), title, body, visibility, version, updated_at, voided_at IS NOT NULL, void_reason
FROM docs.pages`

func loadRow(row pgx.Row) (Page, error) {
	var page Page
	err := row.Scan(&page.ID, &page.FolderID, &page.Title, &page.Body, &page.Visibility, &page.Version, &page.Updated, &page.Voided, &page.VoidReason)
	if errors.Is(err, pgx.ErrNoRows) {
		return Page{}, ErrNotFound
	}
	if err != nil {
		return Page{}, err
	}
	return page, nil
}

func versionBody(ctx context.Context, tx pgx.Tx, pageID int64, version int) (string, error) {
	var body string
	err := tx.QueryRow(ctx, `SELECT body FROM docs.page_versions WHERE page_id = $1 AND version = $2`, pageID, version).Scan(&body)
	return body, err
}

func canSee(actor Actor, page Page) bool {
	if !actor.Active || !people.Can(actor.user(), people.ActDocsEdit, people.Resource{}) {
		return false
	}
	if page.Voided || page.Visibility == "management" {
		return management(actor)
	}
	return true
}

func (s *Service) folders(ctx context.Context) ([]FolderNode, error) {
	rows, err := s.pool.Query(ctx, `
SELECT id, coalesce(parent_id, 0), name, sort_order, tone, mark
FROM docs.folders ORDER BY sort_order, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FolderNode
	for rows.Next() {
		var item FolderNode
		if err := rows.Scan(&item.ID, &item.ParentID, &item.Name, &item.Sort, &item.Tone, &item.Mark); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) visiblePages(ctx context.Context, actor Actor, voided bool) ([]Page, error) {
	rows, err := s.pool.Query(ctx, `
SELECT id, coalesce(folder_id, 0), title, ''::text, visibility, version, updated_at, voided_at IS NOT NULL, void_reason
FROM docs.pages
WHERE voided_at IS NULL AND ($1 OR visibility = 'all')
ORDER BY title, id`, management(actor))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Page
	for rows.Next() {
		var page Page
		if err := rows.Scan(&page.ID, &page.FolderID, &page.Title, &page.Body, &page.Visibility, &page.Version, &page.Updated, &page.Voided, &page.VoidReason); err != nil {
			return nil, err
		}
		out = append(out, page)
	}
	_ = voided
	return out, rows.Err()
}

func (s *Service) folderExists(ctx context.Context, tx pgx.Tx, id int64) error {
	var one int
	err := tx.QueryRow(ctx, `SELECT 1 FROM docs.folders WHERE id = $1`, id).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrBadInput
	}
	return err
}

func assertNoCycle(ctx context.Context, tx pgx.Tx, id, parent int64) error {
	seen := map[int64]bool{id: true}
	cur := parent
	for cur != 0 {
		if seen[cur] {
			return ErrBadInput
		}
		seen[cur] = true
		var next *int64
		err := tx.QueryRow(ctx, `SELECT parent_id FROM docs.folders WHERE id = $1`, cur).Scan(&next)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrBadInput
		}
		if err != nil {
			return err
		}
		if next == nil {
			return nil
		}
		cur = *next
	}
	return nil
}

func parseTags(raw string) ([]string, error) {
	raw = strings.ReplaceAll(raw, "，", ",")
	parts := strings.Split(raw, ",")
	var out []string
	seen := map[string]bool{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if utf8.RuneCountInString(part) > 20 || len(out) == 8 {
			return nil, ErrBadInput
		}
		if seen[part] {
			continue
		}
		seen[part] = true
		out = append(out, part)
	}
	return out, nil
}

func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func (s *Service) tagsOf(ctx context.Context, pageID int64) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
SELECT t.name FROM docs.tags t
JOIN docs.page_tags pt ON pt.tag_id = t.id
WHERE pt.page_id = $1 ORDER BY t.name`, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}
