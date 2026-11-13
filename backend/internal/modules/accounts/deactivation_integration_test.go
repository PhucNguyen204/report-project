//go:build integration

package accounts

import (
	"context"
	"dicho/internal/kernel"
	"dicho/internal/modules/inventory"
	"testing"
)

func TestFoodDeactivationPreservesInventoryHistoryAndRejectsNewWrites(t *testing.T) {
	f := setupAuth(t)
	ctx := context.Background()
	u := f.activate(t, "inactive-food-owner@example.com")
	admin := makeAdmin(t, f, "inactive-food-admin@example.com")
	inv := inventory.New(f.db, f.s)
	q, _ := kernel.ParseQuantity("2")
	lot, err := inv.CreateLot(ctx, u["spaceId"].(string), u["userId"].(string), inventory.LotInput{FoodID: "00000000-0000-7000-8000-000000000021", UnitID: "00000000-0000-7000-8000-000000000011", Quantity: q, StorageLocation: "chill", PurchasedOn: "2026-11-09", ExpiresOn: "2026-11-12"})
	if err != nil {
		t.Fatal(err)
	}
	status, body := f.call(t, "DELETE", "/it4788/v1/admin/foods/"+lot.FoodID, nil, admin)
	if status != 200 {
		t.Fatal(body)
	}
	var amount string
	var moves int
	if err = f.db.Pool.QueryRow(ctx, `SELECT remaining_qty::text FROM pantry.food_lots WHERE id=$1`, lot.ID).Scan(&amount); err != nil || amount != "2.00" {
		t.Fatalf("history quantity %s %v", amount, err)
	}
	if err = f.db.Pool.QueryRow(ctx, `SELECT count(*) FROM pantry.stock_movements WHERE lot_id=$1`, lot.ID).Scan(&moves); err != nil || moves != 1 {
		t.Fatalf("movement history %d %v", moves, err)
	}
	if _, err = inv.CreateLot(ctx, u["spaceId"].(string), u["userId"].(string), inventory.LotInput{FoodID: lot.FoodID, UnitID: lot.UnitID, Quantity: q, StorageLocation: "chill", ExpiresOn: "2026-11-12"}); err == nil {
		t.Fatal("new write used inactive food")
	}
}
