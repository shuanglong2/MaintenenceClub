package docs

import (
	"archive/zip"
	"context"
	"io"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/audit"
)

// Export 把这个人能看到的文档打成 zip, 直接写到 w.
func (s *Service) Export(ctx context.Context, actor Actor, w io.Writer) error {
	if !actor.Active {
		return ErrForbidden
	}
	folders, err := s.folders(ctx)
	if err != nil {
		return err
	}
	pages, err := s.visiblePages(ctx, actor, false)
	if err != nil {
		return err
	}
	full := map[int64]Page{}
	for _, link := range pages {
		page, err := s.load(ctx, link.ID)
		if err != nil {
			return err
		}
		if canSee(actor, page) {
			full[page.ID] = page
		}
	}
	paths := folderPaths(folders)
	exts := map[string]string{}
	if s.files != nil {
		for _, page := range full {
			for _, match := range fileRef.FindAllStringSubmatch(page.Body, -1) {
				if _, ok := exts[match[1]]; ok {
					continue
				}
				id, err := strconv.ParseInt(match[1], 10, 64)
				if err != nil {
					continue
				}
				meta, err := s.files.Get(ctx, id)
				if err != nil {
					continue
				}
				exts[match[1]] = path.Ext(meta.Path)
			}
		}
	}
	zw := zip.NewWriter(w)
	used := map[string]bool{}
	for _, page := range full {
		dir := ""
		if page.FolderID != 0 {
			dir = paths[page.FolderID]
		}
		name := uniqueZip(used, path.Join(dir, strconv.FormatInt(page.ID, 10)+"-"+fileStem(page.Title)+".md"))
		body := rewriteFiles(page.Body, dir, exts)
		entry, err := zw.Create(name)
		if err != nil {
			_ = zw.Close()
			return err
		}
		if _, err := io.WriteString(entry, body); err != nil {
			_ = zw.Close()
			return err
		}
	}
	if s.files != nil {
		seen := map[int64]bool{}
		for _, page := range full {
			for _, match := range fileRef.FindAllStringSubmatch(page.Body, -1) {
				id, err := strconv.ParseInt(match[1], 10, 64)
				if err != nil || seen[id] {
					continue
				}
				seen[id] = true
				meta, file, err := s.files.Open(ctx, id)
				if err != nil {
					continue
				}
				entry, err := zw.Create(path.Join("files", strconv.FormatInt(id, 10)+exts[strconv.FormatInt(id, 10)]))
				if err != nil {
					file.Close()
					_ = zw.Close()
					return err
				}
				_, copyErr := io.Copy(entry, file)
				file.Close()
				if copyErr != nil {
					_ = zw.Close()
					return copyErr
				}
			}
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := audit.Write(ctx, tx, audit.Event{
		ActorID: actor.ID, ActorName: actor.Name, IP: actor.IP, Action: "docs.export",
		Entity: "page", Summary: "导出文档",
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func folderPaths(folders []FolderNode) map[int64]string {
	byID := map[int64]FolderNode{}
	for _, folder := range folders {
		byID[folder.ID] = folder
	}
	out := map[int64]string{}
	var walk func(id int64) string
	walk = func(id int64) string {
		if id == 0 {
			return ""
		}
		if got, ok := out[id]; ok {
			return got
		}
		folder, ok := byID[id]
		if !ok {
			return ""
		}
		name := fileStem(folder.Name)
		parent := walk(folder.ParentID)
		if parent == "" {
			out[id] = name
		} else {
			out[id] = path.Join(parent, name)
		}
		return out[id]
	}
	for id := range byID {
		walk(id)
	}
	return out
}

func rewriteFiles(body, dir string, exts map[string]string) string {
	depth := 0
	if dir != "" && dir != "." {
		depth = strings.Count(dir, "/") + 1
	}
	prefix := strings.Repeat("../", depth)
	return fileRef.ReplaceAllStringFunc(body, func(match string) string {
		parts := fileRef.FindStringSubmatch(match)
		if len(parts) != 2 {
			return match
		}
		return prefix + "files/" + parts[1] + exts[parts[1]]
	})
}

func uniqueZip(used map[string]bool, name string) string {
	if !used[name] {
		used[name] = true
		return name
	}
	ext := path.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for n := 2; ; n++ {
		next := stem + "-" + strconv.Itoa(n) + ext
		if !used[next] {
			used[next] = true
			return next
		}
	}
}

func fileStem(name string) string {
	name = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return '-'
		}
		if r < 32 {
			return -1
		}
		return r
	}, strings.TrimSpace(name))
	if name == "" || name == "." || name == ".." {
		return "page"
	}
	if utf8.RuneCountInString(name) > 40 {
		name = string([]rune(name)[:40])
	}
	return name
}
