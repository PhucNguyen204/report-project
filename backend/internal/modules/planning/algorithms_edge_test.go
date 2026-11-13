package planning

import (
	"math"
	"testing"
)

func TestScoringRejectsInvalidInputsAndNeverUsesExpiredSupply(t *testing.T) {
	needs := []Demand{{FoodID: "a", Dimension: "mass", Quantity: qty("1")}}
	for _, w := range []Weights{{Coverage: math.NaN(), Expiry: 0, Favorite: 1}, {Coverage: -.1, Expiry: .5, Favorite: .6}, {Coverage: .2, Expiry: .2, Favorite: .2}} {
		if _, err := Score(needs, nil, day("2026-11-09"), 3, false, w); err == nil {
			t.Fatalf("invalid weights %+v", w)
		}
	}
	if _, err := Score(needs, []Supply{{Quantity: -1}}, day("2026-11-09"), 3, false, Weights{.6, .3, .1}); err == nil {
		t.Fatal("negative stock")
	}
	if _, err := Score([]Demand{{Quantity: 0}}, nil, day("2026-11-09"), 3, false, Weights{.6, .3, .1}); err == nil {
		t.Fatal("zero demand")
	}
	got, err := Score(needs, []Supply{{FoodID: "a", Dimension: "mass", Quantity: qty("2"), ExpiresOn: day("2026-11-08")}, {FoodID: "a", Dimension: "mass", Quantity: qty("2"), AvailableOn: day("2026-11-10")}}, day("2026-11-09"), 3, false, Weights{.6, .3, .1})
	if err != nil || got.R != 0 || got.E != 0 {
		t.Fatalf("%+v %v", got, err)
	}
}
func TestMissingOptionalAndInvalidSupply(t *testing.T) {
	if _, err := Missing([]Demand{{Quantity: qty("1")}}, []Supply{{Quantity: -1}}); err == nil {
		t.Fatal("negative supply")
	}
	got, err := Missing([]Demand{{FoodID: "a", Quantity: qty("1"), Optional: true, On: day("2026-11-09")}}, nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("optional became demand %+v %v", got, err)
	}
	if validateRecipe(RecipeInput{Name: "Canh", Servings: 1, MealSlot: "snack", Ingredients: []IngredientInput{{FoodID: "a", UnitID: "g", Quantity: qty("1")}}}) == nil {
		t.Fatal("invalid meal slot")
	}
}
