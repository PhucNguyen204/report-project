//go:build integration

package inventory

import (
	"context"
	"dicho/internal/platform"
	"errors"
	"sync"
	"testing"
)

func TestPurchaseStockIsAtomicAndIdempotentAcrossKeys(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	list, err := f.s.CreateList(ctx, f.sid, f.uid, ListInput{Name: "Market", PlannedDate: "2026-11-09"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := f.s.AddItem(ctx, f.sid, f.uid, list.ID, ItemInput{FoodID: f.food, UnitID: f.unit, Quantity: qty("2")})
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.s.AddItem(ctx, f.sid, f.uid, list.ID, ItemInput{Name: "Free food", UnitID: f.unit, Quantity: qty("1")})
	if err != nil {
		t.Fatal(err)
	}
	price := int64(12500)
	if _, err = f.s.Purchase(ctx, f.sid, f.uid, []PurchaseInput{{ItemID: a.ID, UnitPrice: &price}, {ItemID: b.ID}}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.StockItems(ctx, f.sid, f.uid, []StockItemInput{{ItemID: a.ID, StorageLocation: "chill", ExpiresOn: "2026-11-12"}, {ItemID: b.ID, StorageLocation: "chill", ExpiresOn: "2026-11-12"}}); err == nil {
		t.Fatal("free food missing foodId accepted")
	}
	var count int
	if err := f.db.Pool.QueryRow(ctx, "SELECT count(*) FROM pantry.food_lots").Scan(&count); err != nil || count != 0 {
		t.Fatalf("batch did not roll back %d %v", count, err)
	}
	cmd := []StockItemInput{{ItemID: a.ID, StorageLocation: "chill", ExpiresOn: "2026-11-12"}}
	results, err := f.s.StockItems(ctx, f.sid, f.uid, cmd)
	if err != nil {
		t.Fatal(err)
	}
	again, err := f.s.StockItems(ctx, f.sid, f.uid, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if results[0].LotID != again[0].LotID || again[0].Outcome != "already_stocked" {
		t.Fatalf("not idempotent: %+v %+v", results, again)
	}
	var cost int64
	if err := f.db.Pool.QueryRow(ctx, "SELECT total_cost FROM pantry.food_lots WHERE id=$1", results[0].LotID).Scan(&cost); err != nil || cost != 25000 {
		t.Fatalf("cost %d %v", cost, err)
	}
}

func TestConcurrentPurchaseHasOneWinner(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	list, err := f.s.CreateList(ctx, f.sid, f.uid, ListInput{Name: "Market", PlannedDate: "2026-11-09"})
	if err != nil {
		t.Fatal(err)
	}
	item, err := f.s.AddItem(ctx, f.sid, f.uid, list.ID, ItemInput{FoodID: f.food, UnitID: f.unit, Quantity: qty("1")})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := f.s.Purchase(ctx, f.sid, f.uid, []PurchaseInput{{ItemID: item.ID}})
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	good, bad := 0, 0
	for err := range results {
		if err == nil {
			good++
		} else {
			var e *platform.Error
			if !errors.As(err, &e) || e.Code != 4002 {
				t.Fatal(err)
			}
			bad++
		}
	}
	if good != 1 || bad != 1 {
		t.Fatalf("wins=%d conflicts=%d", good, bad)
	}
}

func TestMergeConvertsToBaseUnitWithoutLosingGram(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	list, err := f.s.CreateList(ctx, f.sid, f.uid, ListInput{Name: "Market", PlannedDate: "2026-11-09"})
	if err != nil {
		t.Fatal(err)
	}
	item, err := f.s.AddItem(ctx, f.sid, f.uid, list.ID, ItemInput{FoodID: f.food, UnitID: "00000000-0000-7000-8000-000000000012", Quantity: qty("1")})
	if err != nil {
		t.Fatal(err)
	}
	merged, err := f.s.AddItem(ctx, f.sid, f.uid, list.ID, ItemInput{FoodID: f.food, UnitID: f.unit, Quantity: qty("1")})
	if err != nil {
		t.Fatalf("lost representable gram: %v", err)
	}
	if merged.ID != item.ID || merged.UnitID != f.unit || merged.Quantity != qty("1001") {
		t.Fatalf("merged %+v", merged)
	}
}
