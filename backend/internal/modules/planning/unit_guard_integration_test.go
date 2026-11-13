//go:build integration

package planning

import (
	"context"
	"dicho/internal/platform"
	"testing"
)

func TestReferencedRecipeUnitCannotChangeDimension(t *testing.T) {
	s, db, user, sid, _, food := setupPlanning(t)
	ctx := context.Background()
	unit := platform.ID()
	if _, err := db.Pool.Exec(ctx, "INSERT INTO catalog.units(id,name,dimension,factor_to_base) VALUES($1,'Recipe-only unit','mass',2)", unit); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRecipe(ctx, user, sid, RecipeInput{Name: "Món riêng", Servings: 1, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("1")}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, "UPDATE catalog.units SET dimension='volume' WHERE id=$1", unit); err == nil {
		t.Fatal("referenced recipe unit changed dimension")
	}
}
