//go:build integration

package planning

import (
	"context"
	"dicho/internal/platform"
	"testing"
)

func TestRecipeImageSchemaAllowsOptionalImmutableObjectReference(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	r, err := s.CreateRecipe(ctx, user, sid, RecipeInput{Name: "Canh có ảnh", Servings: 1, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("0.2")}}})
	if err != nil {
		t.Fatal(err)
	}
	var object *string
	if err = db.Pool.QueryRow(ctx, "SELECT image_object_id::text FROM recipe.recipes WHERE id=$1", r.ID).Scan(&object); err != nil {
		t.Fatal(err)
	}
	if object != nil {
		t.Fatalf("unexpected default image %v", object)
	}
	id := platform.ID()
	if _, err = db.Pool.Exec(ctx, "UPDATE recipe.recipes SET image_object_id=$2 WHERE id=$1", r.ID, id); err != nil {
		t.Fatal(err)
	}
	if err = db.Pool.QueryRow(ctx, "SELECT image_object_id::text FROM recipe.recipes WHERE id=$1", r.ID).Scan(&object); err != nil || object == nil || *object != id {
		t.Fatalf("reference %v %v", object, err)
	}
}
