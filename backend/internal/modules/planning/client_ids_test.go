package planning

import "testing"

func TestRecipeValidationRejectsInvalidClientID(t *testing.T) {
	for _, id := range []string{"00000000-0000-4000-8000-000000000099", "00000000-0000-7000-0000-000000000099", "bad-id"} {
		in := RecipeInput{ID: id, Name: "Valid recipe", Servings: 1, Ingredients: []IngredientInput{{FoodID: "food", UnitID: "unit", Quantity: qty("1")}}}
		if err := validateRecipe(in); err == nil {
			t.Fatalf("invalid client ID accepted %s", id)
		}
	}
}
