//go:build integration

package accounts

import (
	"context"
	"dicho/internal/platform"
	"github.com/jackc/pgx/v5"
	"testing"
)

func TestFoodReviewRejectsCrossDimensionMergeWithoutSchedulingWork(t *testing.T) {
	f := setupAuth(t)
	ctx := context.Background()
	u := f.activate(t, "food-review-owner@example.com")
	admin := makeAdmin(t, f, "food-review-admin@example.com")
	var food platform.FoodUnit
	if err := f.db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		food, err = f.s.CreatePrivateFood(ctx, tx, u["spaceId"].(string), "Private mass", "00000000-0000-7000-8000-000000000001", "00000000-0000-7000-8000-000000000011")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	target := platform.ID()
	if _, err := f.db.Pool.Exec(ctx, `INSERT INTO catalog.foods(id,name,category_id,default_unit_id) VALUES($1,'Volume food','00000000-0000-7000-8000-000000000001','00000000-0000-7000-8000-000000000014')`, target); err != nil {
		t.Fatal(err)
	}
	status, body := f.call(t, "POST", "/it4788/v1/admin/foods/"+food.FoodID+"/review", map[string]any{"action": "merge", "targetFoodId": target}, admin)
	if status != 422 {
		t.Fatalf("cross-dimension review %d %+v", status, body)
	}
	var state string
	var jobs int
	if err := f.db.Pool.QueryRow(ctx, `SELECT status FROM catalog.foods WHERE id=$1`, food.FoodID).Scan(&state); err != nil || state != "pending_review" {
		t.Fatalf("review mutated source %s %v", state, err)
	}
	if err := f.db.Pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE args->>'event'='food.merged'`).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatalf("scheduled invalid remap %d %v", jobs, err)
	}
}

func TestMergedFoodValidationResolvesCanonicalFoodForOldClients(t *testing.T) {
	f := setupAuth(t)
	ctx := context.Background()
	u := f.activate(t, "food-old-client@example.com")
	var old platform.FoodUnit
	if err := f.db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		old, err = f.s.CreatePrivateFood(ctx, tx, u["spaceId"].(string), "Old carrot", "00000000-0000-7000-8000-000000000001", "00000000-0000-7000-8000-000000000011")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool.Exec(ctx, `UPDATE catalog.foods SET status='inactive',merged_into='00000000-0000-7000-8000-000000000021' WHERE id=$1`, old.FoodID); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Tx(ctx, func(tx pgx.Tx) error {
		canonical, err := f.s.ValidateFood(ctx, tx, u["spaceId"].(string), old.FoodID, old.UnitID)
		if err == nil && canonical.FoodID != "00000000-0000-7000-8000-000000000021" {
			t.Fatalf("canonical %+v", canonical)
		}
		return err
	}); err != nil {
		t.Fatalf("old client id no longer usable: %v", err)
	}
}
