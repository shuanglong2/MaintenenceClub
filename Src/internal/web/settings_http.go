package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/people"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/files"
)

type quotaView struct {
	DocsGB     string
	SharedGB   string
	DocsUsed   string
	SharedUsed string
}

func (s *server) settingsPage(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if !people.Can(user.Session.User, people.ActSystemManage, people.Resource{}) {
		s.notFound(w, r)
		return
	}
	s.render(w, r, http.StatusOK, page{Title: "设置", Body: "settings-body", Quota: s.quotaView(r.Context())})
}

func (s *server) settingsSave(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return
	}
	if !people.Can(user.Session.User, people.ActSystemManage, people.Resource{}) {
		s.notFound(w, r)
		return
	}
	docs, errDocs := gbToBytes(r.FormValue("docs_gb"))
	shared, errShared := gbToBytes(r.FormValue("shared_gb"))
	if errDocs != nil || errShared != nil {
		s.showErr(w, r, people.ErrBadInput)
		return
	}
	if err := s.writeQuota(r.Context(), docs, shared); err != nil {
		s.internal(w, r, err)
		return
	}
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

func (s *server) quotaView(ctx context.Context) quotaView {
	docs, shared := s.readQuota(ctx)
	docsUsed, sharedUsed := s.usage(ctx)
	return quotaView{
		DocsGB: strconv.FormatInt(docs/(1<<30), 10), SharedGB: strconv.FormatInt(shared/(1<<30), 10),
		DocsUsed: formatBytes(uint64(docsUsed)), SharedUsed: formatBytes(uint64(sharedUsed)),
	}
}

func (s *server) readQuota(ctx context.Context) (int64, int64) {
	docs, shared := int64(10<<30), int64(10<<30)
	q, ok := s.db.(interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	})
	if !ok {
		return docs, shared
	}
	var docsText, sharedText string
	err := q.QueryRow(ctx, `
SELECT coalesce(max(value) FILTER (WHERE key = 'docs_quota'), '10737418240'),
       coalesce(max(value) FILTER (WHERE key = 'shared_quota'), '10737418240')
FROM app.settings`).Scan(&docsText, &sharedText)
	if err != nil {
		return docs, shared
	}
	if n, err := strconv.ParseInt(docsText, 10, 64); err == nil && n > 0 {
		docs = n
	}
	if n, err := strconv.ParseInt(sharedText, 10, 64); err == nil && n > 0 {
		shared = n
	}
	return docs, shared
}

func (s *server) writeQuota(ctx context.Context, docs, shared int64) error {
	exec, ok := s.db.(interface {
		Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	})
	if !ok {
		return errors.New("无法保存设置")
	}
	_, err := exec.Exec(ctx, `
INSERT INTO app.settings (key, value) VALUES ('docs_quota', $1), ('shared_quota', $2)
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, strconv.FormatInt(docs, 10), strconv.FormatInt(shared, 10))
	return err
}

func (s *server) usage(ctx context.Context) (int64, int64) {
	q, ok := s.db.(interface {
		QueryRow(context.Context, string, ...any) pgx.Row
	})
	if !ok {
		return 0, 0
	}
	var docsFiles, bodies, avatars, other, dbSize int64
	err := q.QueryRow(ctx, `
SELECT coalesce((SELECT sum(size_bytes) FROM files.files WHERE module = 'docs'), 0),
       coalesce((SELECT sum(octet_length(body)) FROM docs.pages), 0),
       coalesce((SELECT sum(size_bytes) FROM files.files WHERE module = 'people'), 0),
       coalesce((SELECT sum(size_bytes) FROM files.files WHERE module <> 'docs' AND module <> 'people'), 0),
       pg_database_size(current_database())`).Scan(&docsFiles, &bodies, &avatars, &other, &dbSize)
	if err != nil {
		return 0, 0
	}
	return docsFiles + bodies, avatars + other + dbSize
}

func (s *server) allowDocs(ctx context.Context, extra int64) error {
	limit, _ := s.readQuota(ctx)
	used, _ := s.usage(ctx)
	if used+extra > limit {
		return files.ErrQuota
	}
	return nil
}

func gbToBytes(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 1 || n > 1024 {
		return 0, errors.New("容量不正确")
	}
	return n << 30, nil
}
