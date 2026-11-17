package files

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxUpload = 20 << 20

// Store 把上传文件放在磁盘上, 数据库只记元数据.
type Store struct {
	pool  *pgxpool.Pool
	root  string
	quota int64
}

// Meta 是一条已保存的文件.
type Meta struct {
	ID       int64
	Module   string
	Path     string
	Name     string
	Size     int64
	Type     string
	Uploader int64
	Created  time.Time
}

// New 使用上传根目录和容量上限 (字节).
func New(pool *pgxpool.Pool, root string, quota int64) *Store {
	return &Store{pool: pool, root: root, quota: quota}
}

// Root 是上传文件所在的目录.
func (s *Store) Root() string { return s.root }

// Quota 是上传容量上限, 单位是字节. 0 表示不限制.
func (s *Store) Quota() int64 { return s.quota }

// Save 校验类型和大小, 图片会先缩小, 然后写入磁盘和数据库.
func (s *Store) Save(ctx context.Context, uploader int64, module, original string, r io.Reader) (Meta, error) {
	if !moduleName.MatchString(module) {
		return Meta{}, ErrBadFile
	}
	body, err := readAllLimit(r, maxUpload)
	if err != nil {
		return Meta{}, err
	}
	if len(body) == 0 {
		return Meta{}, ErrBadFile
	}
	kind := http.DetectContentType(body)
	ext, ok := allowedExt(original, kind)
	if !ok {
		return Meta{}, ErrBadFile
	}
	prepared, err := prepareUpload(body, kind, ext)
	if err != nil {
		return Meta{}, err
	}
	var used int64
	if err := s.pool.QueryRow(ctx, `SELECT coalesce(sum(size_bytes), 0) FROM files.files`).Scan(&used); err != nil {
		return Meta{}, err
	}
	if s.quota > 0 && used+int64(len(prepared.body)) > s.quota {
		return Meta{}, ErrQuota
	}
	if err := os.MkdirAll(s.root, 0o750); err != nil {
		return Meta{}, err
	}
	now := time.Now().UTC()
	name, err := randomName(prepared.ext)
	if err != nil {
		return Meta{}, err
	}
	rel := path.Join(module, now.Format("2006"), now.Format("01"), name)
	full, err := SafeJoin(s.root, rel)
	if err != nil {
		return Meta{}, err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		return Meta{}, err
	}
	tmp, err := os.CreateTemp(s.root, ".upload-*")
	if err != nil {
		return Meta{}, err
	}
	tmpName := tmp.Name()
	_, writeErr := tmp.Write(prepared.body)
	closeErr := tmp.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(tmpName)
		if writeErr != nil {
			return Meta{}, writeErr
		}
		return Meta{}, closeErr
	}
	if err := os.Rename(tmpName, full); err != nil {
		_ = os.Remove(tmpName)
		return Meta{}, err
	}
	sum := sha256.Sum256(prepared.body)
	var meta Meta
	err = s.pool.QueryRow(ctx, `
INSERT INTO files.files (module, rel_path, original_name, size_bytes, content_type, sha256, uploaded_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, module, rel_path, original_name, size_bytes, content_type, uploaded_by, created_at`,
		module, rel, cleanName(original), len(prepared.body), prepared.kind, sum[:], uploader,
	).Scan(&meta.ID, &meta.Module, &meta.Path, &meta.Name, &meta.Size, &meta.Type, &meta.Uploader, &meta.Created)
	if err != nil {
		_ = os.Remove(full)
		return Meta{}, err
	}
	return meta, nil
}

// Open 按编号打开文件. 调用方负责关闭.
func (s *Store) Open(ctx context.Context, id int64) (Meta, *os.File, error) {
	meta, err := s.Get(ctx, id)
	if err != nil {
		return Meta{}, nil, err
	}
	full, err := SafeJoin(s.root, meta.Path)
	if err != nil {
		return Meta{}, nil, err
	}
	file, err := os.Open(full)
	if err != nil {
		return Meta{}, nil, err
	}
	return meta, file, nil
}

// Get 只读取元数据.
func (s *Store) Get(ctx context.Context, id int64) (Meta, error) {
	var meta Meta
	err := s.pool.QueryRow(ctx, `
SELECT id, module, rel_path, original_name, size_bytes, content_type, uploaded_by, created_at
FROM files.files WHERE id = $1`, id).Scan(
		&meta.ID, &meta.Module, &meta.Path, &meta.Name, &meta.Size, &meta.Type, &meta.Uploader, &meta.Created,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Meta{}, ErrNotFound
	}
	if err != nil {
		return Meta{}, err
	}
	return meta, nil
}

// Usage 返回已记录文件的总字节数.
func (s *Store) Usage(ctx context.Context) (int64, error) {
	var used int64
	err := s.pool.QueryRow(ctx, `SELECT coalesce(sum(size_bytes), 0) FROM files.files`).Scan(&used)
	return used, err
}

// CleanOrphans 删除超过时限, 且数据库里没有记录的文件.
func (s *Store) CleanOrphans(ctx context.Context, older time.Duration) (int, error) {
	rows, err := s.pool.Query(ctx, `SELECT rel_path FROM files.files`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	known := map[string]bool{}
	for rows.Next() {
		var rel string
		if err := rows.Scan(&rel); err != nil {
			return 0, err
		}
		known[rel] = true
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	removed := 0
	cutoff := time.Now().Add(-older)
	err = filepath.WalkDir(s.root, func(full string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			return err
		}
		rel, err := filepath.Rel(s.root, full)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(entry.Name(), ".upload-") || !known[rel] {
			if os.Remove(full) == nil {
				removed++
			}
		}
		return nil
	})
	return removed, err
}

// SafeJoin 把相对路径限制在根目录里面.
func SafeJoin(root, rel string) (string, error) {
	if rel == "" || strings.Contains(rel, `\`) || strings.Contains(rel, "..") {
		return "", ErrBadFile
	}
	clean := path.Clean("/" + rel)
	if clean == "/" || strings.HasPrefix(clean, "/..") {
		return "", ErrBadFile
	}
	full := filepath.Join(root, filepath.FromSlash(clean[1:]))
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	fullAbs, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	prefix := rootAbs + string(filepath.Separator)
	if fullAbs != rootAbs && !strings.HasPrefix(fullAbs, prefix) {
		return "", ErrBadFile
	}
	return full, nil
}

func randomName(ext string) (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]) + ext, nil
}

func cleanName(name string) string {
	name = path.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == '"' {
			return -1
		}
		return r
	}, name)
	if name == "" || name == "." || name == "/" {
		return "file"
	}
	if utf8.RuneCountInString(name) > 80 {
		rs := []rune(name)
		name = string(rs[len(rs)-80:])
	}
	return name
}

func allowedExt(original, kind string) (string, bool) {
	ext := strings.ToLower(path.Ext(original))
	kind = strings.ToLower(strings.TrimSpace(strings.Split(kind, ";")[0]))
	allows, ok := extTypes[ext]
	if !ok {
		return "", false
	}
	for _, item := range allows {
		if item == kind {
			return ext, true
		}
	}
	return "", false
}

var extTypes = map[string][]string{
	".jpg":  {"image/jpeg"},
	".jpeg": {"image/jpeg"},
	".png":  {"image/png"},
	".gif":  {"image/gif"},
	".webp": {"image/webp"},
	".pdf":  {"application/pdf"},
	".txt":  {"text/plain"},
	".md":   {"text/plain"},
	".csv":  {"text/plain"},
	".zip":  {"application/zip"},
}

var moduleName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
