//go:build integration

package planning

import (
	"context"
	"dicho/internal/modules/inventory"
	"dicho/internal/platform"
	"github.com/jackc/pgx/v5"
	"testing"
)

func TestIngredientNormalizationPreservesRecipeUnitSnapshot(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	r, err := s.CreateRecipe(ctx, user, sid, RecipeInput{Name: "Món cân", Servings: 2, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("0.2")}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, "UPDATE catalog.units SET factor_to_base=2000 WHERE id=$1", unit); err != nil {
		t.Fatal(err)
	}
	if err = db.Tx(ctx, func(tx pgx.Tx) error {
		got, err := s.Ingredients(ctx, tx, sid, r.ID, qty("4"))
		if err != nil {
			return err
		}
		if len(got) != 1 || got[0].Quantity != qty("400") || got[0].UnitID != "00000000-0000-7000-8000-000000000011" {
			t.Fatalf("snapshot changed %+v", got)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s.stock = inventory.New(db, s.catalog)
	preview, err := s.MissingRecipe(ctx, user, sid, r.ID, RecipeMissingInput{Servings: 4, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Lines) != 1 || preview.Lines[0].ShortQty != qty("0.2") {
		t.Fatalf("missing amount ignores snapshot %+v", preview)
	}
}

func TestIngredientsRejectsZeroServings(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	r, err := s.CreateRecipe(ctx, user, sid, RecipeInput{Name: "Món cân", Servings: 2, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("0.2")}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Tx(ctx, func(tx pgx.Tx) error { _, err := s.Ingredients(ctx, tx, sid, r.ID, 0); return err }); err == nil {
		t.Fatal("zero servings silently defaulted")
	}
}

func TestCompareRejectsUnavailableStockAndQuantityOverflow(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	r, err := s.CreateRecipe(ctx, user, sid, RecipeInput{Name: "Món cân", Servings: 1, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("0.2")}}})
	if err != nil {
		t.Fatal(err)
	}
	// A missing stock dependency must be a service error, not a panic.
	func() {
		defer func() {
			if v := recover(); v != nil {
				t.Fatalf("nil stock panicked: %v", v)
			}
		}()
		if _, err = s.Compare(ctx, user, sid, r.ID, 1); err == nil {
			t.Fatal("missing stock accepted")
		}
	}()
	inv := inventory.New(db, s.catalog)
	s.stock = inv
	if err = db.Tx(ctx, func(tx pgx.Tx) error {
		_, err := inv.AddLots(ctx, tx, sid, user, []platform.StockInput{{FoodID: food, UnitID: unit, Quantity: qty("5000000000"), PurchasedOn: db.Today(), ExpiresOn: db.Today().AddDate(0, 0, 3), Location: "chill"}, {FoodID: food, UnitID: unit, Quantity: qty("5000000000"), PurchasedOn: db.Today(), ExpiresOn: db.Today().AddDate(0, 0, 3), Location: "chill"}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Compare(ctx, user, sid, r.ID, 1); err == nil {
		t.Fatal("overflow stock sum accepted")
	}
}
