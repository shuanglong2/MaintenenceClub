package inventory

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/people"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/audit"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/files"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/migrate"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/testkit"
)

func TestInventoryFlow(t *testing.T) {
	pool := testkit.Open(t)
	ctx := context.Background()
	if err := migrate.Up(ctx, pool, []migrate.Module{
		{Name: "audit", FS: audit.SQL},
		{Name: "people", FS: people.SQL},
		{Name: "files", FS: files.SQL},
		{Name: "inventory", FS: SQL},
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
	svc := New(pool, nil)

	if _, err := svc.Create(ctx, memberActor, Draft{Code: "X1", Name: "不该成功", Category: "元件"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("组员不应新建物品: %v", err)
	}
	loc, err := svc.CreateLocation(ctx, leaderActor, "实验室", 0)
	if err != nil {
		t.Fatal(err)
	}
	part, err := svc.Create(ctx, leaderActor, Draft{
		Code: "R100", Name: "100%电阻", Category: "元件", Spec: "10k", Unit: "个",
		LocationID: loc, MinQty: 4, Qty: 5,
	})
	if err != nil || part.Qty != 5 || part.Location != "实验室" {
		t.Fatalf("%v %+v", err, part)
	}
	if err := svc.Move(ctx, nil, memberActor, MoveInput{ItemID: part.ID, Kind: "use", Qty: 2}); err != nil {
		t.Fatal(err)
	}
	part, err = svc.Get(ctx, memberActor, part.ID)
	if err != nil || part.Qty != 3 {
		t.Fatalf("领用后数量 %v %+v", err, part)
	}
	if err := svc.Move(ctx, nil, memberActor, MoveInput{ItemID: part.ID, Kind: "use", Qty: 4}); !errors.Is(err, ErrStock) {
		t.Fatalf("应拒绝负库存: %v", err)
	}
	part, err = svc.Get(ctx, leaderActor, part.ID)
	if err != nil || part.Qty != 3 {
		t.Fatal(part.Qty, err)
	}
	other, err := svc.Create(ctx, leaderActor, Draft{Code: "R2", Name: "普通电阻", Category: "元件", Qty: 2})
	if err != nil {
		t.Fatal(err)
	}
	found, err := svc.List(ctx, memberActor, "元件", "100%")
	if err != nil || len(found) != 1 || found[0].ID != part.ID {
		t.Fatalf("搜索百分号 %v %+v", err, found)
	}
	low, err := svc.LowStock(ctx, 8)
	if err != nil || len(low) != 1 || low[0].ID != part.ID {
		t.Fatalf("低库存 %v %+v", err, low)
	}

	tool, err := svc.Create(ctx, leaderActor, Draft{Code: "OS1", Name: "示波器", Category: "器材", Tracked: true, LocationID: loc})
	if err != nil || tool.Qty != 1 || tool.State != stateAvailable {
		t.Fatalf("%v %+v", err, tool)
	}
	if err := svc.Event(ctx, memberActor, tool.ID, "borrow", ""); err != nil {
		t.Fatal(err)
	}
	tool, err = svc.Get(ctx, memberActor, tool.ID)
	if err != nil || tool.State != stateBorrowed || tool.Qty != 1 {
		t.Fatalf("借出后 %v %+v", err, tool)
	}
	if err := svc.Event(ctx, memberActor, tool.ID, "borrow", ""); !errors.Is(err, ErrState) {
		t.Fatal(err)
	}
	if err := svc.Event(ctx, memberActor, tool.ID, "return", ""); err != nil {
		t.Fatal(err)
	}
	if err := svc.Move(ctx, nil, leaderActor, MoveInput{ItemID: tool.ID, Kind: "scrap", Qty: 1}); err != nil {
		t.Fatal(err)
	}
	tool, err = svc.Get(ctx, leaderActor, tool.ID)
	if err != nil || tool.State != stateScrapped || tool.Qty != 0 {
		t.Fatalf("报废后 %v %+v", err, tool)
	}
	if err := svc.Event(ctx, memberActor, tool.ID, "borrow", ""); !errors.Is(err, ErrState) {
		t.Fatalf("报废后不应借出: %v", err)
	}

	sheet, err := svc.CreateStocktake(ctx, leaderActor, "第一次")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SaveLines(ctx, leaderActor, sheet, map[int64]int{part.ID: 1, tool.ID: 0, other.ID: 2}); err != nil {
		t.Fatal(err)
	}
	if err := svc.ConfirmStocktake(ctx, leaderActor, sheet); err != nil {
		t.Fatal(err)
	}
	part, err = svc.Get(ctx, leaderActor, part.ID)
	if err != nil || part.Qty != 1 {
		t.Fatalf("盘点后 %v %d", err, part.Qty)
	}

	n, err := svc.Import(ctx, leaderActor, []ImportRow{{
		Code: "C1", Name: "电容", Category: "元件", Unit: "个", Location: "实验室/抽屉", MinQty: 1, Qty: 8,
	}})
	if err != nil || n != 1 {
		t.Fatal(err, n)
	}
	imported, err := svc.List(ctx, leaderActor, "", "电容")
	if err != nil || len(imported) != 1 || imported[0].Qty != 8 || imported[0].Location != "实验室/抽屉" {
		t.Fatalf("%v %+v", err, imported)
	}
	body, err := svc.Export(ctx, memberActor)
	if err != nil || !strings.Contains(string(body), "电容") || !strings.Contains(string(body), "100%电阻") {
		t.Fatal(err, string(body))
	}
	if _, err := pool.Exec(ctx, `UPDATE inventory.movements SET qty = 0 WHERE item_id = $1`, part.ID); err == nil || !strings.Contains(err.Error(), "库存流水不能修改或删除") {
		t.Fatal(err)
	}
	if err := svc.Void(ctx, leaderActor, part.ID, "用完"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(ctx, memberActor, part.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestConcurrentUse(t *testing.T) {
	pool := testkit.Open(t)
	ctx := context.Background()
	if err := migrate.Up(ctx, pool, []migrate.Module{
		{Name: "audit", FS: audit.SQL},
		{Name: "people", FS: people.SQL},
		{Name: "files", FS: files.SQL},
		{Name: "inventory", FS: SQL},
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
	leader, err := peopleSvc.Login(ctx, "leader", "leader-pass", "127.0.0.1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	actor := Actor{ID: leader.User.ID, Name: leader.Name, Role: leader.User.Role, Active: true}
	svc := New(pool, nil)
	item, err := svc.Create(ctx, actor, Draft{Code: "LAST", Name: "最后一件", Category: "消耗品", Qty: 1})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	okN, failN int
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := svc.Move(ctx, nil, actor, MoveInput{ItemID: item.ID, Kind: "use", Qty: 1})
			mu.Lock()
			if err == nil {
				okN++
			} else if errors.Is(err, ErrStock) {
				failN++
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	if okN != 1 || failN != 1 {
		t.Fatalf("并发领用 ok=%d fail=%d", okN, failN)
	}
	item, err = svc.Get(ctx, actor, item.ID)
	if err != nil || item.Qty != 0 {
		t.Fatal(err, item.Qty)
	}
}
