package web

import (
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/docs"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/events"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/inventory"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/people"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/files"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/mdown"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/search"
)

// RegisterDocsWidget 把最近文档修改登记到首页. 仅管理层的页面不会出现在组员首页.
func RegisterDocsWidget(svc *docs.Service) {
	if svc == nil {
		return
	}
	RegisterWidget(Widget{
		Title: "最近文档修改",
		Allow: func(user people.User) bool {
			return people.Can(user, people.ActDocsEdit, people.Resource{})
		},
		Render: func(ctx context.Context, user people.User) (template.HTML, error) {
			return svc.HomeRecentHTML(ctx, docs.Actor{ID: user.ID, Role: user.Role, Active: user.Active})
		},
	})
}

type docView struct {
	Tree       []docs.FolderNode
	Loose      []docs.PageLink
	Voided     []docs.PageLink
	Page       docs.Page
	Folders    []docs.FolderChoice
	Hits       []docHit
	Query      string
	Versions   []docs.Version
	Version    docs.Version
	Conflict   *docs.Conflict
	Draft      docs.Draft
	Tags       string
	HTML       template.HTML
	Management bool
	HasNext    bool
	Offset     int
}

type docHit struct {
	ID    int64
	Title string
	HTML  template.HTML
}

func (s *server) docsIndex(w http.ResponseWriter, r *http.Request) {
	user, actor, ok := s.docUser(w, r)
	if !ok {
		return
	}
	tree, loose, err := s.docs.Tree(r.Context(), actor)
	if err != nil {
		s.showDocErr(w, r, err)
		return
	}
	voided, err := s.docs.Voided(r.Context(), actor)
	if err != nil {
		s.showDocErr(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, page{
		Title: "文档", Body: "docs-body",
		Doc: docView{Tree: tree, Loose: loose, Voided: voided, Management: user.Session.User.Role != "member"},
	})
}

func (s *server) docsNew(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := s.docUser(w, r)
	if !ok {
		return
	}
	folders, err := s.docs.Folders(r.Context(), actor)
	if err != nil {
		s.showDocErr(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, page{
		Title: "新建文档", Body: "doc-form-body", Editing: false,
		Doc: docView{Folders: folders, Draft: docs.Draft{Visibility: "all"}, Management: actor.Role != "member"},
	})
}

func (s *server) docsCreate(w http.ResponseWriter, r *http.Request) {
	s.saveDoc(w, r, 0)
}

func (s *server) docsEdit(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := s.docUser(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	pageRow, err := s.docs.Get(r.Context(), actor, id)
	if err != nil {
		s.showDocErr(w, r, err)
		return
	}
	folders, err := s.docs.Folders(r.Context(), actor)
	if err != nil {
		s.showDocErr(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, page{
		Title: "编辑 " + pageRow.Title, Body: "doc-form-body", Editing: true,
		Doc: docView{
			Page: pageRow, Folders: folders, Management: actor.Role != "member",
			Draft: draftFrom(pageRow), Tags: strings.Join(pageRow.Tags, ", "),
		},
	})
}

func (s *server) docsSave(w http.ResponseWriter, r *http.Request) {
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	s.saveDoc(w, r, id)
}

func (s *server) saveDoc(w http.ResponseWriter, r *http.Request, id int64) {
	_, actor, ok := s.docUser(w, r)
	if !ok {
		return
	}
	draft := readDraft(r, id)
	out, err := s.docs.Save(r.Context(), actor, draft)
	if err != nil {
		s.showDocErr(w, r, err)
		return
	}
	if out.Conflict != nil {
		folders, err := s.docs.Folders(r.Context(), actor)
		if err != nil {
			s.showDocErr(w, r, err)
			return
		}
		s.render(w, r, http.StatusConflict, page{
			Title: "保存冲突", Body: "doc-conflict-body",
			Doc: docView{Conflict: out.Conflict, Draft: draft, Folders: folders, Page: docs.Page{ID: id}},
		})
		return
	}
	http.Redirect(w, r, "/docs/p/"+strconv.FormatInt(out.Page.ID, 10), http.StatusSeeOther)
}

func (s *server) docsConflict(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := s.docUser(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	if r.FormValue("choice") == "discard" {
		http.Redirect(w, r, "/docs/p/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
		return
	}
	draft := readDraft(r, id)
	version, _ := strconv.Atoi(r.FormValue("current_version"))
	draft.Version = version
	if r.FormValue("choice") == "merge" {
		draft.Body = r.FormValue("merge")
	} else {
		draft.Body = r.FormValue("mine")
	}
	out, err := s.docs.Save(r.Context(), actor, draft)
	if err != nil {
		s.showDocErr(w, r, err)
		return
	}
	if out.Conflict != nil {
		s.render(w, r, http.StatusConflict, page{
			Title: "保存冲突", Body: "doc-conflict-body",
			Doc: docView{Conflict: out.Conflict, Draft: draft, Page: docs.Page{ID: id}},
		})
		return
	}
	http.Redirect(w, r, "/docs/p/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *server) docsView(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.PathValue("id"), ".md") && r.PathValue("slug") == "" {
		s.docsRaw(w, r)
		return
	}
	_, actor, ok := s.docUser(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	row, err := s.docs.Get(r.Context(), actor, id)
	if err != nil {
		s.showDocErr(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, page{
		Title: row.Title, Body: "doc-body",
		Doc: docView{Page: row, HTML: mdown.Render(row.Body), Management: actor.Role != "member"},
	})
}

func (s *server) docsRaw(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := s.docUser(w, r)
	if !ok {
		return
	}
	rawID := strings.TrimSuffix(r.PathValue("id"), ".md")
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || id < 1 {
		s.notFound(w, r)
		return
	}
	row, err := s.docs.Get(r.Context(), actor, id)
	if err != nil {
		s.showDocErr(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", "inline")
	_, _ = io.WriteString(w, row.Body)
}

func (s *server) docsHistory(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := s.docUser(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	row, err := s.docs.Get(r.Context(), actor, id)
	if err != nil {
		s.showDocErr(w, r, err)
		return
	}
	versions, err := s.docs.History(r.Context(), actor, id)
	if err != nil {
		s.showDocErr(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, page{
		Title: "历史 " + row.Title, Body: "doc-history-body",
		Doc: docView{Page: row, Versions: versions},
	})
}

func (s *server) docsVersion(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := s.docUser(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	version, err := strconv.Atoi(r.PathValue("version"))
	if err != nil || version < 1 {
		s.notFound(w, r)
		return
	}
	row, err := s.docs.Get(r.Context(), actor, id)
	if err != nil {
		s.showDocErr(w, r, err)
		return
	}
	item, err := s.docs.Version(r.Context(), actor, id, version)
	if err != nil {
		s.showDocErr(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, page{
		Title: "版本 " + strconv.Itoa(version), Body: "doc-version-body",
		Doc: docView{Page: row, Version: item, HTML: mdown.Render(item.Body)},
	})
}

func (s *server) docsRestore(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := s.docUser(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	version, err := strconv.Atoi(r.PathValue("version"))
	if err != nil || version < 1 {
		s.notFound(w, r)
		return
	}
	row, err := s.docs.Get(r.Context(), actor, id)
	if err != nil {
		s.showDocErr(w, r, err)
		return
	}
	item, err := s.docs.Version(r.Context(), actor, id, version)
	if err != nil {
		s.showDocErr(w, r, err)
		return
	}
	out, err := s.docs.Save(r.Context(), actor, docs.Draft{
		ID: id, Version: row.Version, FolderID: row.FolderID, Title: item.Title,
		Body: item.Body, Visibility: row.Visibility, Tags: strings.Join(row.Tags, ", "),
	})
	if err != nil {
		s.showDocErr(w, r, err)
		return
	}
	if out.Conflict != nil {
		s.render(w, r, http.StatusConflict, page{
			Title: "保存冲突", Body: "doc-conflict-body",
			Doc: docView{Conflict: out.Conflict, Page: row, Draft: draftFrom(row)},
		})
		return
	}
	http.Redirect(w, r, "/docs/p/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *server) docsVoid(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := s.docUser(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	if err := s.docs.Void(r.Context(), actor, id, r.FormValue("reason")); err != nil {
		s.showDocErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/docs", http.StatusSeeOther)
}

func (s *server) docsUnvoid(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := s.docUser(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	if err := s.docs.Unvoid(r.Context(), actor, id); err != nil {
		s.showDocErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/docs/p/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *server) docsSearch(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := s.docUser(w, r)
	if !ok {
		return
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	hits, err := s.docs.Search(r.Context(), actor, query, offset)
	if err != nil {
		s.showDocErr(w, r, err)
		return
	}
	view := docView{Query: query, Offset: offset}
	if len(hits) > 20 {
		view.HasNext = true
		hits = hits[:20]
	}
	for _, hit := range hits {
		view.Hits = append(view.Hits, docHit{ID: hit.ID, Title: hit.Title, HTML: template.HTML(search.Highlight(hit.Snippet, query))})
	}
	prev := offset - 20
	if prev < 0 {
		prev = 0
	}
	s.render(w, r, http.StatusOK, page{
		Title: "搜索文档", Body: "doc-search-body", Doc: view,
		Offset: offset, Prev: prev, HasNext: view.HasNext,
	})
}

func (s *server) docsExport(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := s.docUser(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="docs.zip"`)
	if err := s.docs.Export(r.Context(), actor, w); err != nil {
		s.showDocErr(w, r, err)
	}
}

func (s *server) docsFolderCreate(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := s.docUser(w, r)
	if !ok {
		return
	}
	parent, _ := strconv.ParseInt(r.FormValue("parent"), 10, 64)
	if _, err := s.docs.CreateFolder(r.Context(), actor, r.FormValue("name"), parent); err != nil {
		s.showDocErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/docs", http.StatusSeeOther)
}

func (s *server) docsFolderUpdate(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := s.docUser(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	parent, _ := strconv.ParseInt(r.FormValue("parent"), 10, 64)
	if err := s.docs.UpdateFolder(r.Context(), actor, id, r.FormValue("name"), parent); err != nil {
		s.showDocErr(w, r, err)
		return
	}
	http.Redirect(w, r, "/docs", http.StatusSeeOther)
}

func (s *server) docsUpload(w http.ResponseWriter, r *http.Request) {
	s.receiveFile(w, r, false)
}

func (s *server) docsAttach(w http.ResponseWriter, r *http.Request) {
	s.receiveFile(w, r, true)
}

func (s *server) receiveFile(w http.ResponseWriter, r *http.Request, backToEdit bool) {
	_, actor, ok := s.docUser(w, r)
	if !ok {
		return
	}
	store := s.docs.Files()
	if store == nil {
		s.internal(w, r, errors.New("文件存储未配置"))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		s.showDocErr(w, r, docs.ErrBadInput)
		return
	}
	defer file.Close()
	meta, err := store.Save(r.Context(), actor.ID, "docs", header.Filename, file)
	if err != nil {
		s.showDocErr(w, r, err)
		return
	}
	line := uploadMarkdown(meta)
	if wantsJSON(r) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": meta.ID, "markdown": line})
		return
	}
	if !backToEdit {
		http.Redirect(w, r, "/docs", http.StatusSeeOther)
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	row, err := s.docs.Get(r.Context(), actor, id)
	if err != nil {
		s.showDocErr(w, r, err)
		return
	}
	row.Body = strings.TrimRight(row.Body, "\n") + "\n" + line + "\n"
	folders, err := s.docs.Folders(r.Context(), actor)
	if err != nil {
		s.showDocErr(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, page{
		Title: "编辑 " + row.Title, Body: "doc-form-body", Editing: true, Notice: "已插入引用, 记得保存",
		Doc: docView{
			Page: row, Folders: folders, Management: actor.Role != "member",
			Draft: draftFrom(row), Tags: strings.Join(row.Tags, ", "),
		},
	})
}

func (s *server) mdPreview(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := s.docUser(w, r); !ok {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(mdown.Render(r.FormValue("body"))))
}

func (s *server) fileDownload(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := s.docUser(w, r)
	if !ok {
		return
	}
	id, ok := s.docID(w, r)
	if !ok {
		return
	}
	allowed := false
	var err error
	if s.docs != nil {
		allowed, err = s.docs.CanDownload(r.Context(), actor, id)
		if err != nil {
			s.showDocErr(w, r, err)
			return
		}
	}
	if !allowed && s.inv != nil {
		allowed, err = s.inv.CanSeeFile(r.Context(), inventory.Actor{
			ID: actor.ID, Name: actor.Name, Role: actor.Role, Active: actor.Active, IP: actor.IP,
		}, id)
		if err != nil {
			s.showInvErr(w, r, err)
			return
		}
	}
	if !allowed && s.events != nil {
		allowed, err = s.events.CanSeeFile(r.Context(), events.Actor{
			ID: actor.ID, Name: actor.Name, Role: actor.Role, Active: actor.Active, IP: actor.IP,
		}, id)
		if err != nil {
			s.showEventErr(w, r, err)
			return
		}
	}
	if !allowed {
		s.notFound(w, r)
		return
	}
	store := s.uploads()
	if store == nil {
		s.notFound(w, r)
		return
	}
	meta, file, err := store.Open(r.Context(), id)
	if err != nil {
		s.showDocErr(w, r, err)
		return
	}
	defer file.Close()
	disp := "attachment"
	if strings.HasPrefix(meta.Type, "image/") {
		disp = "inline"
	}
	w.Header().Set("Content-Type", meta.Type)
	w.Header().Set("Content-Disposition", disp+`; filename="`+strings.NewReplacer(`"`, "", "\r", "", "\n", "").Replace(meta.Name)+`"`)
	http.ServeContent(w, r, meta.Name, meta.Created, file)
}

func (s *server) docUser(w http.ResponseWriter, r *http.Request) (visitor, docs.Actor, bool) {
	user, ok := s.requireUser(w, r)
	if !ok {
		return visitor{}, docs.Actor{}, false
	}
	if s.docs == nil {
		s.internal(w, r, errors.New("文档服务未配置"))
		return visitor{}, docs.Actor{}, false
	}
	return user, docs.Actor{
		ID: user.Session.User.ID, Name: user.Session.Name, Role: user.Session.User.Role,
		Active: user.Session.User.Active, IP: user.IP,
	}, true
}

func (s *server) docID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		s.notFound(w, r)
		return 0, false
	}
	return id, true
}

func (s *server) showDocErr(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, docs.ErrNotFound), errors.Is(err, files.ErrNotFound):
		s.notFound(w, r)
	case errors.Is(err, docs.ErrForbidden):
		s.render(w, r, http.StatusForbidden, page{Title: "提示", Body: "error-body", Status: http.StatusForbidden, Message: err.Error()})
	case errors.Is(err, docs.ErrBadInput), errors.Is(err, files.ErrBadFile), errors.Is(err, files.ErrTooBig), errors.Is(err, files.ErrPixels), errors.Is(err, files.ErrQuota):
		s.render(w, r, http.StatusBadRequest, page{Title: "提示", Body: "error-body", Status: http.StatusBadRequest, Message: err.Error()})
	default:
		s.internal(w, r, err)
	}
}

func readDraft(r *http.Request, id int64) docs.Draft {
	folder, _ := strconv.ParseInt(r.FormValue("folder"), 10, 64)
	version, _ := strconv.Atoi(r.FormValue("version"))
	return docs.Draft{
		ID: id, Version: version, FolderID: folder, Title: r.FormValue("title"),
		Body: r.FormValue("body"), Visibility: r.FormValue("visibility"), Tags: r.FormValue("tags"),
	}
}

func draftFrom(page docs.Page) docs.Draft {
	return docs.Draft{
		ID: page.ID, Version: page.Version, FolderID: page.FolderID, Title: page.Title,
		Body: page.Body, Visibility: page.Visibility, Tags: strings.Join(page.Tags, ", "),
	}
}

func uploadMarkdown(meta files.Meta) string {
	ref := "/files/" + strconv.FormatInt(meta.ID, 10)
	if strings.HasPrefix(meta.Type, "image/") {
		return "![" + meta.Name + "](" + ref + ")"
	}
	return "[" + meta.Name + "](" + ref + ")"
}

func wantsJSON(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "application/json")
}
