package docs

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/people"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/audit"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/files"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/migrate"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/testkit"
)

func TestDocsFlow(t *testing.T) {
	pool := testkit.Open(t)
	ctx := context.Background()
	if err := migrate.Up(ctx, pool, []migrate.Module{
		{Name: "audit", FS: audit.SQL},
		{Name: "people", FS: people.SQL},
		{Name: "files", FS: files.SQL},
		{Name: "docs", FS: SQL},
	}); err != nil {
		t.Fatal(err)
	}
	peopleSvc, err := people.New(pool, people.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if err := peopleSvc.CreateFirst(ctx, "leader", "leader-pass"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	leader, err := peopleSvc.Login(ctx, "leader", "leader-pass", "127.0.0.1", now)
	if err != nil {
		t.Fatal(err)
	}
	leaderActor := Actor{ID: leader.User.ID, Name: leader.Name, Role: leader.User.Role, Active: true}
	if _, err := peopleSvc.Create(ctx, people.Actor{ID: leader.User.ID, Name: leader.Name, Role: leader.User.Role, Active: true}, people.Input{
		Name: "组员甲", Login: "member1", Role: "组员", Password: "member-pass",
	}); err != nil {
		t.Fatal(err)
	}
	member, err := peopleSvc.Login(ctx, "member1", "member-pass", "127.0.0.1", now)
	if err != nil {
		t.Fatal(err)
	}
	memberActor := Actor{ID: member.User.ID, Name: member.Name, Role: member.User.Role, Active: true}

	store := files.New(pool, t.TempDir(), 8<<20)
	note, err := store.Save(ctx, leader.User.ID, "docs", "note.txt", strings.NewReader("示波器探头"))
	if err != nil {
		t.Fatal(err)
	}
	svc := New(pool, store)
	folderID, err := svc.CreateFolder(ctx, leaderActor, "仪器", 0)
	if err != nil {
		t.Fatal(err)
	}
	created, err := svc.Save(ctx, leaderActor, Draft{
		FolderID: folderID, Title: "示波器", Body: "校准探头. 附图 /files/" + strconv.FormatInt(note.ID, 10),
		Visibility: "all", Tags: "仪器",
	})
	if err != nil || created.Conflict != nil {
		t.Fatalf("%v %+v", err, created.Conflict)
	}
	hidden, err := svc.Save(ctx, leaderActor, Draft{Title: "内部约定", Body: "仅管理层", Visibility: "management"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(ctx, memberActor, hidden.Page.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("组员不应看到管理层页面: %v", err)
	}
	hits, err := svc.Search(ctx, memberActor, "示波器", 0)
	if err != nil || len(hits) != 1 || hits[0].ID != created.Page.ID {
		t.Fatalf("%+v %v", hits, err)
	}
	one, err := svc.Search(ctx, leaderActor, "焊", 0)
	if err != nil {
		t.Fatal(err)
	}
	_ = one
	secretHits, err := svc.Search(ctx, memberActor, "内部约定", 0)
	if err != nil || len(secretHits) != 0 {
		t.Fatalf("搜索泄露: %+v %v", secretHits, err)
	}
	ok, err := svc.CanDownload(ctx, memberActor, note.ID)
	if err != nil || !ok {
		t.Fatalf("附件应可下载 %v %v", ok, err)
	}
	updated, err := svc.Save(ctx, leaderActor, Draft{
		ID: created.Page.ID, Version: created.Page.Version, Title: "示波器", Body: "对方已改", Visibility: "all",
	})
	if err != nil || updated.Conflict != nil {
		t.Fatalf("%v %+v", err, updated.Conflict)
	}
	conflict, err := svc.Save(ctx, memberActor, Draft{
		ID: created.Page.ID, Version: created.Page.Version, Title: "示波器", Body: "我的修改", Visibility: "all",
	})
	if err != nil || conflict.Conflict == nil {
		t.Fatalf("应提示冲突: %v %+v", err, conflict.Conflict)
	}
	merged, err := svc.Save(ctx, memberActor, Draft{
		ID: created.Page.ID, Version: conflict.Conflict.CurrentVersion, Title: "示波器",
		Body: conflict.Conflict.Merge, Visibility: "all",
	})
	if err != nil || merged.Conflict != nil || merged.Page.Version <= updated.Page.Version {
		t.Fatalf("%v %+v", err, merged)
	}
	history, err := svc.History(ctx, leaderActor, created.Page.ID)
	if err != nil || len(history) < 2 {
		t.Fatalf("%+v %v", history, err)
	}
	memberHome, err := svc.HomeRecentHTML(ctx, memberActor)
	if err != nil || !strings.Contains(string(memberHome), "示波器") || strings.Contains(string(memberHome), "内部约定") {
		t.Fatalf("组员文档 %v %s", err, memberHome)
	}
	leaderHome, err := svc.HomeRecentHTML(ctx, leaderActor)
	if err != nil || !strings.Contains(string(leaderHome), "内部约定") || !strings.Contains(string(leaderHome), "示波器") {
		t.Fatalf("组长文档 %v %s", err, leaderHome)
	}
	if err := svc.Void(ctx, leaderActor, hidden.Page.ID, "过期"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(ctx, memberActor, hidden.Page.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if err := svc.Unvoid(ctx, leaderActor, hidden.Page.ID); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := svc.Export(ctx, memberActor, &buf); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	foundPage := false
	foundSecret := false
	for _, file := range reader.File {
		if strings.Contains(file.Name, "示波器") {
			foundPage = true
		}
		if strings.Contains(file.Name, "内部约定") {
			foundSecret = true
		}
	}
	if !foundPage || foundSecret {
		t.Fatalf("导出内容不对 page=%v secret=%v", foundPage, foundSecret)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM docs.pages`); err == nil || !strings.Contains(err.Error(), "文档不能物理删除") {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE docs.page_versions SET title = 'x'`); err == nil {
		t.Fatal("历史版本不应允许修改")
	}
}
