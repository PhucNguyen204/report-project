//go:build integration

package planning

import (
	"context"
	"dicho/internal/modules/inventory"
	"dicho/internal/platform"
	"github.com/jackc/pgx/v5"
	"testing"
)

func TestRecipeAndMissingConfirmationUseCanonicalMergedFood(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	old := platform.ID()
	if _, err := db.Pool.Exec(ctx, "INSERT INTO catalog.foods(id,name,category_id,scope,owner_space_id,status,default_unit_id,merged_into) SELECT $1,'Old carrot',category_id,'space',$2,'inactive',$3,id FROM catalog.foods WHERE id=$4", old, sid, unit, food); err != nil {
		t.Fatal(err)
	}
	recipe, err := s.CreateRecipe(ctx, user, sid, RecipeInput{Name: "Canh cà rốt", Servings: 1, Ingredients: []IngredientInput{{FoodID: old, UnitID: unit, Quantity: qty("0.2")}}})
	if err != nil {
		t.Fatal(err)
	}
	if recipe.Ingredients[0].FoodID != food {
		t.Fatalf("stale food persisted %+v", recipe.Ingredients)
	}
	inv := inventory.New(db, s.catalog)
	s.lists = inv
	out, err := s.MissingRecipe(ctx, user, sid, recipe.ID, RecipeMissingInput{Servings: 1, PlannedDate: db.Today().Format("2006-01-02"), Name: "Mua rau", Lines: []ConfirmLine{{FoodID: old, UnitID: unit, Quantity: qty("0.3")}}})
	if err != nil {
		t.Fatal(err)
	}
	var stored string
	if err = db.Pool.QueryRow(ctx, "SELECT food_id::text FROM shopping.shopping_items WHERE list_id=$1", out.ListID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != food {
		t.Fatalf("confirmation persisted old food %s", stored)
	}
	meal, err := s.CreateMeal(ctx, user, sid, MealInput{RecipeID: recipe.ID, Date: db.Today().Format("2006-01-02"), MealSlot: "lunch", Servings: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Tx(ctx, func(tx pgx.Tx) error {
		_, err := inv.AddLots(ctx, tx, sid, user, []platform.StockInput{{FoodID: food, UnitID: unit, Quantity: qty("1"), PurchasedOn: db.Today(), ExpiresOn: db.Today().AddDate(0, 0, 3), Location: "chill"}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	s.stock = inv
	if _, err = s.Cook(ctx, user, sid, meal.ID, CookInput{Lines: []CookLine{{FoodID: old, UnitID: unit, Quantity: qty("0.2")}}}); err != nil {
		t.Fatalf("canonical adjustment rejected %v", err)
	}
}
