//go:build integration

package accounts

import (
	"context"
	"dicho/internal/kernel"
	"dicho/internal/modules/inventory"
	"dicho/internal/platform"
	"github.com/jackc/pgx/v5"
	"testing"
)

func TestSpaceCopyResolvesMergedFoodBeforeRemapWorkerRuns(t *testing.T) {
	f := setupAuth(t)
	ctx := context.Background()
	u := f.activate(t, "copy-merged-alias@example.com")
	source := u["spaceId"].(string)
	user := u["userId"].(string)
	var food platform.FoodUnit
	if err := f.db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		food, err = f.s.CreatePrivateFood(ctx, tx, source, "Carrot alias", "00000000-0000-7000-8000-000000000001", "00000000-0000-7000-8000-000000000011")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	inv := inventory.New(f.db, f.s)
	amount, _ := kernel.ParseQuantity("2")
	if _, err := inv.CreateLot(ctx, source, user, inventory.LotInput{FoodID: food.FoodID, UnitID: food.UnitID, Quantity: amount, StorageLocation: "chill", ExpiresOn: "2026-11-12"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool.Exec(ctx, `UPDATE catalog.foods SET status='inactive',merged_into='00000000-0000-7000-8000-000000000021' WHERE id=$1`, food.FoodID); err != nil {
		t.Fatal(err)
	}
	status, body := f.call(t, "POST", "/it4788/v1/spaces", map[string]any{"name": "Merged copy family"}, u["accessToken"].(string))
	if status != 201 {
		t.Fatal(body)
	}
	target := body["data"].(map[string]any)["id"].(string)
	if err := f.db.Tx(ctx, func(tx pgx.Tx) error {
		mapping, err := f.s.ClonePrivateFoods(ctx, tx, source, target)
		if err != nil {
			return err
		}
		return inv.CopySpace(ctx, tx, source, target, user, false, true, mapping)
	}); err != nil {
		t.Fatal(err)
	}
	var copied string
	if err := f.db.Pool.QueryRow(ctx, `SELECT food_id::text FROM pantry.food_lots WHERE space_id=$1`, target).Scan(&copied); err != nil || copied != "00000000-0000-7000-8000-000000000021" {
		t.Fatalf("copy reintroduced merged alias %s %v", copied, err)
	}
}
