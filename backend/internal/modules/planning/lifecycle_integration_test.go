//go:build integration

package planning

import (
	"context"
	"dicho/internal/platform"
	"github.com/jackc/pgx/v5"
	"testing"
)

func TestCopySpaceCreatesIndependentRecipesWithMappedFoods(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	r, err := s.CreateRecipe(ctx, user, sid, RecipeInput{Name: "Món gốc", Servings: 2, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("0.2")}}})
	if err != nil {
		t.Fatal(err)
	}
	target := platform.ID()
	if _, err = db.Pool.Exec(ctx, "INSERT INTO household.spaces(id,kind,name,created_by) VALUES($1,'family','Copy family',$2)", target, user); err != nil {
		t.Fatal(err)
	}
	mapped := platform.ID()
	if _, err = db.Pool.Exec(ctx, "INSERT INTO catalog.foods(id,name,category_id,scope,owner_space_id,status,default_unit_id) SELECT $1,'Cà rốt riêng',category_id,'space',$2,'pending_review',$3 FROM catalog.foods WHERE id='00000000-0000-7000-8000-000000000021'", mapped, target, unit); err != nil {
		t.Fatal(err)
	}
	if err = db.Tx(ctx, func(tx pgx.Tx) error { return s.CopySpace(ctx, tx, sid, target, user, map[string]string{food: mapped}) }); err != nil {
		t.Fatal(err)
	}
	var id, origin, fid, quantity string
	if err = db.Pool.QueryRow(ctx, "SELECT r.id::text,r.origin_recipe_id::text,i.food_id::text,i.quantity::text FROM recipe.recipes r JOIN recipe.recipe_ingredients i ON i.recipe_id=r.id WHERE r.space_id=$1", target).Scan(&id, &origin, &fid, &quantity); err != nil {
		t.Fatal(err)
	}
	if id == r.ID || origin != r.ID || fid != mapped || quantity != "0.20" {
		t.Fatalf("copy %s %s %s %s", id, origin, fid, quantity)
	}
	if _, err = db.Pool.Exec(ctx, "UPDATE recipe.recipes SET name='Changed' WHERE id=$1", id); err != nil {
		t.Fatal(err)
	}
	original, err := s.GetRecipe(ctx, user, sid, r.ID, 0)
	if err != nil || original.Name != "Món gốc" {
		t.Fatalf("source mutated %+v %v", original, err)
	}
}

func TestPurgeSpaceRemovesOnlyOwnedPlanningData(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	r, err := s.CreateRecipe(ctx, user, sid, RecipeInput{Name: "Món riêng", Servings: 1, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("1")}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetFavorite(ctx, user, r.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateMeal(ctx, user, sid, MealInput{RecipeID: r.ID, Date: db.Today().Format("2006-01-02"), MealSlot: "lunch", Servings: 1}); err != nil {
		t.Fatal(err)
	}
	if err = db.Tx(ctx, func(tx pgx.Tx) error { return s.PurgeSpace(ctx, tx, sid) }); err != nil {
		t.Fatal(err)
	}
	if err = db.Tx(ctx, func(tx pgx.Tx) error { return s.PurgeSpace(ctx, tx, sid) }); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = db.Pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM recipe.recipes WHERE space_id=$1)+(SELECT count(*) FROM mealplan.meal_entries WHERE space_id=$1)+(SELECT count(*) FROM recipe.favorite_recipes)", sid).Scan(&n); err != nil || n != 0 {
		t.Fatalf("planning remains %d %v", n, err)
	}
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM household.spaces WHERE id=$1", sid).Scan(&n); err != nil || n != 1 {
		t.Fatalf("other module mutated %d %v", n, err)
	}
}

func TestRemapFoodCombinesExactBaseAmountsAndIsIdempotent(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	source := platform.ID()
	if _, err := db.Pool.Exec(ctx, "INSERT INTO catalog.foods(id,name,category_id,scope,owner_space_id,status,default_unit_id) SELECT $1,'Carrot private',category_id,'space',$2,'pending_review',$3 FROM catalog.foods WHERE id='00000000-0000-7000-8000-000000000021'", source, sid, unit); err != nil {
		t.Fatal(err)
	}
	r, err := s.CreateRecipe(ctx, user, sid, RecipeInput{Name: "Canh gộp", Servings: 1, Ingredients: []IngredientInput{{FoodID: source, UnitID: unit, Quantity: qty("0.2")}, {FoodID: food, UnitID: "00000000-0000-7000-8000-000000000011", Quantity: qty("50"), Optional: true}}})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = db.Tx(ctx, func(tx pgx.Tx) error { return s.RemapFood(ctx, tx, source, food) }); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	var q, uid string
	var optional bool
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM recipe.recipe_ingredients WHERE recipe_id=$1", r.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err = db.Pool.QueryRow(ctx, "SELECT quantity::text,unit_id::text,optional FROM recipe.recipe_ingredients WHERE recipe_id=$1", r.ID).Scan(&q, &uid, &optional); err != nil {
		t.Fatal(err)
	}
	if count != 1 || q != "250.00" || uid != "00000000-0000-7000-8000-000000000011" || optional {
		t.Fatalf("merged %d %s %s %v", count, q, uid, optional)
	}
}
