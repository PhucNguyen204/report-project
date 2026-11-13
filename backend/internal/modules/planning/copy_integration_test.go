//go:build integration

package planning

import (
	"context"
	"dicho/internal/platform"
	"testing"
)

func TestCopyRecipeRollsBackWhenOriginLinkFails(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	admin := platform.ID()
	_, err := db.Pool.Exec(ctx, "INSERT INTO identity.users(id,email,full_name,password_hash,status,system_role) VALUES($1,'copyadmin@example.com','Admin Copy','hash','active','admin')", admin)
	if err != nil {
		t.Fatal(err)
	}
	template, err := s.SaveTemplate(ctx, admin, "", 0, TemplateInput{RecipeInput: RecipeInput{Name: "Canh mẫu", Servings: 1, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("1")}}}, Status: "published"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Pool.Exec(ctx, `CREATE FUNCTION recipe.reject_origin() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.origin_recipe_id IS NOT NULL THEN RAISE EXCEPTION 'origin rejected'; END IF; RETURN NEW; END $$; CREATE TRIGGER fail_origin BEFORE INSERT OR UPDATE ON recipe.recipes FOR EACH ROW EXECUTE FUNCTION recipe.reject_origin()`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CopyRecipe(ctx, user, sid, template.ID, ""); err == nil {
		t.Fatal("expected origin failure")
	}
	var n int
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM recipe.recipes WHERE space_id=$1", sid).Scan(&n); err != nil || n != 0 {
		t.Fatalf("partial copy %d %v", n, err)
	}
}
