//go:build integration

package inventory

import (
	"context"
	"dicho/internal/modules/accounts"
	"dicho/internal/platform"
	"github.com/jackc/pgx/v5"
	"testing"
)

type inventoryLifecycle interface {
	CopySpace(context.Context, pgx.Tx, string, string, string, bool, bool, map[string]string) error
	PurgeSpace(context.Context, pgx.Tx, string) error
	RemapFood(context.Context, pgx.Tx, string, string) error
}

func TestSpaceCopyPreservesPantryValueAndResetsShoppingState(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	p, ok := any(f.s).(inventoryLifecycle)
	if !ok {
		t.Fatal("inventory lifecycle ports missing")
	}
	target := platform.ID()
	if _, err := f.db.Pool.Exec(ctx, `INSERT INTO household.spaces(id,kind,name,created_by) VALUES($1,'personal','Target',$2)`, target, f.uid); err != nil {
		t.Fatal(err)
	}
	list, err := f.s.CreateList(ctx, f.sid, f.uid, ListInput{Name: "Bought", PlannedDate: "2026-11-09"})
	if err != nil {
		t.Fatal(err)
	}
	item, err := f.s.AddItem(ctx, f.sid, f.uid, list.ID, ItemInput{FoodID: f.food, UnitID: f.unit, Quantity: qty("2")})
	if err != nil {
		t.Fatal(err)
	}
	price := int64(1500)
	if _, err = f.s.Purchase(ctx, f.sid, f.uid, []PurchaseInput{{ItemID: item.ID, UnitPrice: &price}}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.StockItems(ctx, f.sid, f.uid, []StockItemInput{{ItemID: item.ID, StorageLocation: "chill", ExpiresOn: "2026-11-12"}}); err != nil {
		t.Fatal(err)
	}
	if err = f.db.Tx(ctx, func(tx pgx.Tx) error { return p.CopySpace(ctx, tx, f.sid, target, f.uid, true, true, nil) }); err != nil {
		t.Fatal(err)
	}
	var initial, remaining, base string
	var cost int64
	var source *string
	if err = f.db.Pool.QueryRow(ctx, `SELECT initial_qty::text,remaining_qty::text,base_remaining::text,total_cost,source_item_id::text FROM pantry.food_lots WHERE space_id=$1`, target).Scan(&initial, &remaining, &base, &cost, &source); err != nil || initial != "2.00" || remaining != "2.00" || base != "2.000000" || cost != 3000 || source != nil {
		t.Fatalf("lot %s %s %s %d %v %v", initial, remaining, base, cost, source, err)
	}
	var state, listState string
	var dirty bool
	if err = f.db.Pool.QueryRow(ctx, `SELECT i.status,l.status,(i.actual_quantity IS NOT NULL OR i.assignee_id IS NOT NULL OR i.purchased_by IS NOT NULL OR i.purchased_at IS NOT NULL OR i.lot_id IS NOT NULL OR i.unit_price IS NOT NULL) FROM shopping.shopping_items i JOIN shopping.shopping_lists l ON l.id=i.list_id WHERE i.space_id=$1`, target).Scan(&state, &listState, &dirty); err != nil || state != "to_buy" || listState != "draft" || dirty {
		t.Fatalf("shopping %s %s dirty=%v %v", state, listState, dirty, err)
	}
	if err = f.db.Tx(ctx, func(tx pgx.Tx) error { return p.PurgeSpace(ctx, tx, target) }); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = f.db.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM pantry.food_lots WHERE space_id=$1)+(SELECT count(*) FROM shopping.shopping_lists WHERE space_id=$1)`, target).Scan(&n); err != nil || n != 0 {
		t.Fatalf("purged %d %v", n, err)
	}
	if err = f.db.Pool.QueryRow(ctx, `SELECT count(*) FROM pantry.food_lots WHERE space_id=$1`, f.sid).Scan(&n); err != nil || n != 1 {
		t.Fatalf("source lost %d %v", n, err)
	}
}

func TestFoodRemapPreservesDuplicateShoppingQuantitiesAndHistory(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	p, ok := any(f.s).(inventoryLifecycle)
	if !ok {
		t.Fatal("inventory lifecycle ports missing")
	}
	target := platform.ID()
	if _, err := f.db.Pool.Exec(ctx, `INSERT INTO catalog.foods(id,name,category_id,scope,default_unit_id,status) VALUES($1,'Target carrot','00000000-0000-7000-8000-000000000001','global',$2,'active')`, target, f.unit); err != nil {
		t.Fatal(err)
	}
	list, err := f.s.CreateList(ctx, f.sid, f.uid, ListInput{Name: "Merge", PlannedDate: "2026-11-09"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.AddItem(ctx, f.sid, f.uid, list.ID, ItemInput{FoodID: f.food, UnitID: "00000000-0000-7000-8000-000000000012", Quantity: qty("0.01")}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.AddItem(ctx, f.sid, f.uid, list.ID, ItemInput{FoodID: target, UnitID: f.unit, Quantity: qty("0.01")}); err != nil {
		t.Fatal(err)
	}
	f.stock(t, "2", "2026-11-12")
	for range 2 {
		if err = f.db.Tx(ctx, func(tx pgx.Tx) error { return p.RemapFood(ctx, tx, f.food, target) }); err != nil {
			t.Fatal(err)
		}
	}
	var amount string
	var oldRows int
	if err = f.db.Pool.QueryRow(ctx, `SELECT sum(quantity*unit_factor)::text FROM shopping.shopping_items WHERE food_id=$1`, target).Scan(&amount); err != nil || amount != "10.010000" {
		t.Fatalf("exact quantity %s %v", amount, err)
	}
	if err = f.db.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM pantry.food_lots WHERE food_id=$1)+(SELECT count(*) FROM pantry.stock_movements WHERE food_id=$1)+(SELECT count(*) FROM shopping.shopping_items WHERE food_id=$1)`, f.food).Scan(&oldRows); err != nil || oldRows != 0 {
		t.Fatalf("stale references %d %v", oldRows, err)
	}
}

func TestCanonicalFoodIsPersistedWhenOldClientUsesMergedID(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	old := platform.ID()
	if _, err := f.db.Pool.Exec(ctx, `INSERT INTO catalog.foods(id,name,category_id,scope,owner_space_id,default_unit_id,status,merged_into) VALUES($1,'Merged carrot','00000000-0000-7000-8000-000000000001','space',$2,$3,'inactive',$4)`, old, f.sid, f.unit, f.food); err != nil {
		t.Fatal(err)
	}
	f.s = New(f.db, accounts.New(f.db, nil))
	list, err := f.s.CreateList(ctx, f.sid, f.uid, ListInput{Name: "Old client", PlannedDate: "2026-11-09"})
	if err != nil {
		t.Fatal(err)
	}
	item, err := f.s.AddItem(ctx, f.sid, f.uid, list.ID, ItemInput{FoodID: old, UnitID: f.unit, Quantity: qty("1")})
	if err != nil {
		t.Fatal(err)
	}
	if item.FoodID == nil || *item.FoodID != f.food {
		t.Fatalf("stale food persisted in item %+v", item)
	}
	lot, err := f.s.CreateLot(ctx, f.sid, f.uid, LotInput{FoodID: old, UnitID: f.unit, Quantity: qty("1"), StorageLocation: "chill", PurchasedOn: "2026-11-09", ExpiresOn: "2026-11-12"})
	if err != nil {
		t.Fatal(err)
	}
	if lot.FoodID != f.food {
		t.Fatalf("stale food persisted in lot %+v", lot)
	}
	free, err := f.s.AddItem(ctx, f.sid, f.uid, list.ID, ItemInput{Name: "Free carrot", UnitID: f.unit, Quantity: qty("1")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Purchase(ctx, f.sid, f.uid, []PurchaseInput{{ItemID: free.ID}}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.StockItems(ctx, f.sid, f.uid, []StockItemInput{{ItemID: free.ID, FoodID: old, StorageLocation: "chill", ExpiresOn: "2026-11-12"}}); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err = f.db.Pool.QueryRow(ctx, `SELECT food_id::text FROM shopping.shopping_items WHERE id=$1`, free.ID).Scan(&stored); err != nil || stored != f.food {
		t.Fatalf("stale stock item %q %v", stored, err)
	}
}

func TestRemapRejectsDimensionMismatchAndUnrepresentableQuantityAtomically(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	p := any(f.s).(inventoryLifecycle)
	target := platform.ID()
	if _, err := f.db.Pool.Exec(ctx, `INSERT INTO catalog.foods(id,name,category_id,default_unit_id) VALUES($1,'Volume target','00000000-0000-7000-8000-000000000001','00000000-0000-7000-8000-000000000014')`, target); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Tx(ctx, func(tx pgx.Tx) error { return p.RemapFood(ctx, tx, f.food, target) }); err == nil {
		t.Fatal("dimension mismatch accepted")
	}
	tiny := platform.ID()
	if _, err := f.db.Pool.Exec(ctx, `INSERT INTO catalog.units(id,name,dimension,factor_to_base) VALUES($1,'tiny mass','mass',0.0001)`, tiny); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool.Exec(ctx, `UPDATE catalog.foods SET default_unit_id=$2 WHERE id=$1`, target, f.unit); err != nil {
		t.Fatal(err)
	}
	list, err := f.s.CreateList(ctx, f.sid, f.uid, ListInput{Name: "Precision", PlannedDate: "2026-11-09"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.AddItem(ctx, f.sid, f.uid, list.ID, ItemInput{FoodID: f.food, UnitID: tiny, Quantity: qty("0.01")}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.AddItem(ctx, f.sid, f.uid, list.ID, ItemInput{FoodID: target, UnitID: f.unit, Quantity: qty("0.01")}); err != nil {
		t.Fatal(err)
	}
	if err = f.db.Tx(ctx, func(tx pgx.Tx) error { return p.RemapFood(ctx, tx, f.food, target) }); err == nil {
		t.Fatal("inexact base merge accepted")
	}
	var n int
	if err = f.db.Pool.QueryRow(ctx, `SELECT count(*) FROM shopping.shopping_items WHERE food_id=$1`, f.food).Scan(&n); err != nil || n != 1 {
		t.Fatalf("failed merge lost source %d %v", n, err)
	}
	if err = f.db.Tx(ctx, func(tx pgx.Tx) error { return p.CopySpace(ctx, tx, f.sid, f.sid, f.uid, true, true, nil) }); err == nil {
		t.Fatal("copy into source accepted")
	}
}
