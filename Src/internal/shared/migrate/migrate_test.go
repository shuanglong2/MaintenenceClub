package migrate

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/testkit"
)

func TestUpTwice(t *testing.T) {
	pool := testkit.Open(t)
	ctx := context.Background()
	mod := []Module{{
		Name: "demo",
		FS:   fstest.MapFS{
			"001_init.sql": &fstest.MapFile{Data: []byte("CREATE TABLE demo_item (id int PRIMARY KEY);\nINSERT INTO demo_item (id) VALUES (1);\n")},
		},
	}}
	if err := Up(ctx, pool, mod); err != nil {
		t.Fatal(err)
	}
	if err := Up(ctx, pool, mod); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM public.schema_migrations WHERE module = 'demo'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("记录数 %d", n)
	}
}

func TestUpRollsBack(t *testing.T) {
	pool := testkit.Open(t)
	ctx := context.Background()
	bad := []Module{{
		Name: "boom",
		FS:   fstest.MapFS{
			"001_bad.sql": &fstest.MapFile{Data: []byte("CREATE TABLE rolled (id int);\nSELECT 1/0;\n")},
		},
	}}
	if err := Up(ctx, pool, bad); err == nil {
		t.Fatal("应失败")
	}
	var reg string
	if err := pool.QueryRow(ctx, `SELECT coalesce(to_regclass('public.rolled')::text, '')`).Scan(&reg); err != nil {
		t.Fatal(err)
	}
	if reg != "" {
		t.Fatalf("失败的迁移不应留下表 %s", reg)
	}

	good := []Module{{
		Name: "boom",
		FS:   fstest.MapFS{
			"001_bad.sql": &fstest.MapFile{Data: []byte("CREATE TABLE rolled (id int);\n")},
		},
	}}
	if err := Up(ctx, pool, good); err != nil {
		t.Fatal(err)
	}
}

func TestUpConcurrent(t *testing.T) {
	pool := testkit.Open(t)
	ctx := context.Background()
	mod := []Module{{
		Name: "once",
		FS:   fstest.MapFS{
			"001_once.sql": &fstest.MapFile{Data: []byte("CREATE TABLE once_run (id int PRIMARY KEY);\nINSERT INTO once_run (id) VALUES (1);\nSELECT pg_sleep(0.4);\n")},
		},
	}}
	errCh := make(chan error, 2)
	for range 2 {
		go func() {
			errCh <- Up(ctx, pool, mod)
		}()
	}
	for range 2 {
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM once_run`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("行数 %d", n)
	}
}

func TestEmptyUp(t *testing.T) {
	pool := testkit.Open(t)
	ctx := context.Background()
	if err := Up(ctx, pool, nil); err != nil {
		t.Fatal(err)
	}
	if err := Up(ctx, pool, nil); err != nil {
		t.Fatal(err)
	}
}
