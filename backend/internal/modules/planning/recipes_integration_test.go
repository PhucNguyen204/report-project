//go:build integration

package planning

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"dicho/internal/modules/accounts"
	"dicho/internal/platform"
	"dicho/internal/testutil"
	"testing"
)

func setupPlanning(t *testing.T) (*Service, *platform.DB, string, string, string, string) {
	t.Helper()
	db := testutil.NewDB(t)
	ctx := context.Background()
	user, sid, unit, food := platform.ID(), platform.ID(), "00000000-0000-7000-8000-000000000012", "00000000-0000-7000-8000-000000000021"
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO identity.users(id,email,full_name,password_hash,status) VALUES($1,'planner@example.com','Planner','hash','active')", []any{user}},
		{"INSERT INTO household.spaces(id,kind,name,created_by) VALUES($1,'personal','My pantry',$2)", []any{sid, user}},
		{"INSERT INTO household.space_members(space_id,user_id,role,space_kind) VALUES($1,$2,'leader','personal')", []any{sid, user}},
	} {
		if _, err := db.Pool.Exec(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatal(err)
		}
	}
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	catalog := accounts.New(db, key)
	return New(db, nil, nil, catalog), db, user, sid, unit, food
}

func TestRecipeCRUDScopesOwnershipAndOptimisticVersion(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	in := RecipeInput{Name: "Canh cà rốt", Servings: 2, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("0.2")}}}
	recipe, err := s.CreateRecipe(ctx, user, sid, in)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetRecipe(ctx, user, sid, recipe.ID, 4)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != in.Name || len(got.Ingredients) != 1 || got.Ingredients[0].Quantity != qty("0.4") {
		t.Fatalf("%+v", got)
	}
	if _, err = s.GetRecipe(ctx, platform.ID(), sid, recipe.ID, 2); err == nil {
		t.Fatal("outsider can read recipe")
	}
	in.Name = "Canh mới"
	if _, err = s.UpdateRecipe(ctx, user, sid, recipe.ID, 0, in); err == nil {
		t.Fatal("stale version accepted")
	}
	updated, err := s.UpdateRecipe(ctx, user, sid, recipe.ID, 1, in)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != 2 {
		t.Fatal("version not advanced")
	}
	if err = s.SetFavorite(ctx, user, recipe.ID, true); err != nil {
		t.Fatal(err)
	}
	if err = s.SetFavorite(ctx, user, recipe.ID, true); err != nil {
		t.Fatal(err)
	}
	var count int
	db.Pool.QueryRow(ctx, "SELECT count(*) FROM recipe.favorite_recipes WHERE user_id=$1 AND recipe_id=$2", user, recipe.ID).Scan(&count)
	if count != 1 {
		t.Fatal("favorite duplicated")
	}
	if err = s.DeleteRecipe(ctx, user, sid, recipe.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetRecipe(ctx, user, sid, recipe.ID, 2); err == nil {
		t.Fatal("deleted recipe visible")
	}
}

func TestInvalidRecipeIngredientRollsBackWholeRecipe(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	_, err := s.CreateRecipe(ctx, user, sid, RecipeInput{Name: "Canh", Servings: 2, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("1")}, {FoodID: platform.ID(), UnitID: unit, Quantity: qty("1")}}})
	if err == nil {
		t.Fatal("unknown food accepted")
	}
	var count int
	db.Pool.QueryRow(ctx, "SELECT count(*) FROM recipe.recipes").Scan(&count)
	if count != 0 {
		t.Fatalf("partial recipe persisted %d", count)
	}
}
