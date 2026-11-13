package planning

import (
	"dicho/internal/kernel"
	"testing"
	"time"
)

func qty(s string) kernel.Quantity {
	q, err := kernel.ParseQuantity(s)
	if err != nil {
		panic(err)
	}
	return q
}
func day(s string) time.Time {
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return d
}

func TestMissingAllocatesSupplyOnceAtEachMealDate(t *testing.T) {
	demands := []Demand{{FoodID: "carrot", Dimension: "mass", Quantity: qty("0.4"), On: day("2026-11-09")}, {FoodID: "carrot", Dimension: "mass", Quantity: qty("0.6"), On: day("2026-11-11")}}
	supply := []Supply{{FoodID: "carrot", Dimension: "mass", Quantity: qty("0.5"), ExpiresOn: day("2026-11-10")}, {FoodID: "carrot", Dimension: "mass", Quantity: qty("0.2"), ExpiresOn: day("2026-11-12")}, {FoodID: "carrot", Dimension: "mass", Quantity: qty("0.1"), AvailableOn: day("2026-11-09")}}
	got, err := Missing(demands, supply)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Quantity != qty("0.3") || !got[0].On.Equal(day("2026-11-11")) {
		t.Fatalf("got %+v", got)
	}
	if supply[0].Quantity != qty("0.5") {
		t.Fatal("caller supply mutated")
	}
}

func TestMissingRejectsWrongDimensionAndLatePending(t *testing.T) {
	got, err := Missing([]Demand{{FoodID: "milk", Dimension: "volume", Quantity: qty("1"), On: day("2026-11-09")}}, []Supply{{FoodID: "milk", Dimension: "mass", Quantity: qty("10")}, {FoodID: "milk", Dimension: "volume", Quantity: qty("1"), AvailableOn: day("2026-11-10")}})
	if err != nil || len(got) != 1 || got[0].Quantity != qty("1") {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestMissingRejectsNegativeAndOverflow(t *testing.T) {
	if _, err := Missing([]Demand{{Quantity: -1}}, nil); err == nil {
		t.Fatal("negative accepted")
	}
}

func TestSuggestionPrefersExpiringFoodAsSpecified(t *testing.T) {
	stock := []Supply{{FoodID: "fish", Dimension: "mass", Quantity: qty("1"), ExpiresOn: day("2026-11-10")}, {FoodID: "pineapple", Dimension: "mass", Quantity: qty("1"), ExpiresOn: day("2026-11-11")}, {FoodID: "tomato", Dimension: "mass", Quantity: qty("1"), ExpiresOn: day("2026-11-14")}, {FoodID: "egg", Dimension: "count", Quantity: qty("1"), ExpiresOn: day("2026-11-19")}}
	first, err := Score([]Demand{{FoodID: "fish", Dimension: "mass", Quantity: qty("1")}, {FoodID: "pineapple", Dimension: "mass", Quantity: qty("1")}, {FoodID: "tomato", Dimension: "mass", Quantity: qty("1")}, {FoodID: "tamarind", Dimension: "mass", Quantity: qty("1")}}, stock, day("2026-11-09"), 3, false, Weights{.6, .3, .1})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Score([]Demand{{FoodID: "egg", Dimension: "count", Quantity: qty("1")}, {FoodID: "tomato", Dimension: "mass", Quantity: qty("1")}}, stock, day("2026-11-09"), 3, false, Weights{.6, .3, .1})
	if err != nil {
		t.Fatal(err)
	}
	if first.R != .75 || first.E != 1 || first.S != .75 || second.S != .6 {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
}

func TestSuggestionRequiresFullQuantityAndIgnoresOptional(t *testing.T) {
	got, err := Score([]Demand{{FoodID: "a", Dimension: "mass", Quantity: qty("2")}, {FoodID: "b", Dimension: "mass", Quantity: qty("1"), Optional: true}}, []Supply{{FoodID: "a", Dimension: "mass", Quantity: qty("1"), ExpiresOn: day("2026-11-10")}}, day("2026-11-09"), 3, true, Weights{.6, .3, .1})
	if err != nil {
		t.Fatal(err)
	}
	if got.R != 0 || got.F != 1 || got.Missing != 1 {
		t.Fatalf("%+v", got)
	}
}

func TestValidateRecipeAndMealBoundaries(t *testing.T) {
	if err := validateRecipe(RecipeInput{Name: "Canh", Servings: 2, Ingredients: []IngredientInput{{FoodID: "food", UnitID: "unit", Quantity: qty("1")}}}); err != nil {
		t.Fatal(err)
	}
	for _, v := range []RecipeInput{{Name: "a", Servings: 2}, {Name: "Canh", Servings: 0}, {Name: "Canh", Servings: 21}, {Name: "Canh", Servings: 2, Ingredients: []IngredientInput{{Quantity: 0}}}} {
		if validateRecipe(v) == nil {
			t.Fatalf("accepted %+v", v)
		}
	}
	if validateMealDate(day("2026-11-09"), day("2026-12-07"), 28) != nil {
		t.Fatal("boundary day rejected")
	}
	if validateMealDate(day("2026-11-09"), day("2026-12-08"), 28) == nil {
		t.Fatal("horizon exceeded")
	}
}
