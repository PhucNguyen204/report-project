//go:build integration

package planning

import (
	"context"
	"dicho/internal/modules/inventory"
	"dicho/internal/platform"
	"github.com/jackc/pgx/v5"
	"testing"
)

func TestGenerateMissingUsesDatedSupplyAndKeepsConfirmationEdits(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	inv := inventory.New(db, s.catalog)
	s.stock = inv
	s.lists = inv
	d := db.Today().AddDate(0, 0, 1)
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		_, err := inv.AddLots(ctx, tx, sid, user, []platform.StockInput{
			{FoodID: food, UnitID: unit, Quantity: qty("0.5"), PurchasedOn: db.Today(), ExpiresOn: d.AddDate(0, 0, 1), Location: "chill"},
			{FoodID: food, UnitID: unit, Quantity: qty("0.2"), PurchasedOn: db.Today(), ExpiresOn: d.AddDate(0, 0, 3), Location: "chill"},
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	r1, err := s.CreateRecipe(ctx, user, sid, RecipeInput{Name: "Bữa đầu", Servings: 1, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("0.4")}}})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := s.CreateRecipe(ctx, user, sid, RecipeInput{Name: "Bữa sau", Servings: 1, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("0.6")}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct{ rid, date string }{{r1.ID, d.Format("2006-01-02")}, {r2.ID, d.AddDate(0, 0, 2).Format("2006-01-02")}} {
		if _, err = s.CreateMeal(ctx, user, sid, MealInput{RecipeID: v.rid, Date: v.date, MealSlot: "lunch", Servings: 1}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.CreateMeal(ctx, user, sid, MealInput{Name: "Món tự do", Date: d.Format("2006-01-02"), MealSlot: "dinner", Servings: 1}); err != nil {
		t.Fatal(err)
	}
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		_, err := inv.Compose(ctx, tx, sid, user, "", []platform.MissingLine{{FoodID: food, UnitID: unit, Quantity: qty("0.1")}})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	in := GenerateInput{From: d.Format("2006-01-02"), To: d.AddDate(0, 0, 2).Format("2006-01-02"), DryRun: true}
	preview, err := s.Generate(ctx, user, sid, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Lines) != 1 || preview.Lines[0].ShortQty != qty("0.3") || preview.Lines[0].UnitID != unit || len(preview.SkippedMeals) != 1 {
		t.Fatalf("%+v", preview)
	}
	var count int
	_ = db.Pool.QueryRow(ctx, "SELECT count(*) FROM shopping.shopping_lists").Scan(&count)
	if count != 1 {
		t.Fatal("preview wrote list")
	}
	in.DryRun = false
	in.PlannedDate = db.Today().Format("2006-01-02")
	in.Lines = []ConfirmLine{{FoodID: food, UnitID: unit, Quantity: qty("0.5")}}
	confirmed, err := s.Generate(ctx, user, sid, in)
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.ListID == "" {
		t.Fatal("no list")
	}
	var quantity string
	if err = db.Pool.QueryRow(ctx, "SELECT quantity::text FROM shopping.shopping_items WHERE list_id=$1", confirmed.ListID).Scan(&quantity); err != nil || quantity != "0.50" {
		t.Fatalf("confirmed %s %v", quantity, err)
	}
}

func TestMissingRecipeRejectsInvalidScopeAndTargetChoice(t *testing.T) {
	s, _, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	inv := inventory.New(s.db, s.catalog)
	s.stock = inv
	s.lists = inv
	r, err := s.CreateRecipe(ctx, user, sid, RecipeInput{Name: "Canh", Servings: 1, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("1")}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.MissingRecipe(ctx, platform.ID(), sid, r.ID, RecipeMissingInput{Servings: 1, DryRun: true}); err == nil {
		t.Fatal("outsider preview")
	}
	if _, err = s.MissingRecipe(ctx, user, sid, r.ID, RecipeMissingInput{Servings: 1, PlannedDate: s.db.Today().Format("2006-01-02"), TargetListID: platform.ID(), Lines: []ConfirmLine{{FoodID: food, UnitID: unit, Quantity: qty("1")}}}); err == nil {
		t.Fatal("both targets accepted")
	}
}
