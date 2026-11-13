//go:build integration

package planning

import (
	"context"
	"dicho/internal/platform"
	"testing"
)

func TestTemplateAdministrationAndSearchVisibility(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	in := TemplateInput{RecipeInput: RecipeInput{Name: "Canh cà rốt mẫu", Servings: 1, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("0.2")}}}, Status: "draft"}
	if _, err := s.SaveTemplate(ctx, user, "", 0, in); err == nil {
		t.Fatal("user administers templates")
	}
	admin := platform.ID()
	_, err := db.Pool.Exec(ctx, "INSERT INTO identity.users(id,email,full_name,password_hash,status,system_role) VALUES($1,'admin@example.com','Admin User','hash','active','admin')", admin)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := s.SaveTemplate(ctx, admin, "", 0, in)
	if err != nil {
		t.Fatal(err)
	}
	search, err := s.Search(ctx, user, sid, "ca rot", 10, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range search.Items {
		if v["id"] == draft.ID {
			t.Fatal("draft leaked")
		}
	}
	in.Status = "published"
	published, err := s.SaveTemplate(ctx, admin, draft.ID, draft.Version, in)
	if err != nil {
		t.Fatal(err)
	}
	copy, err := s.CopyRecipe(ctx, user, sid, published.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	var origin string
	if err = db.Pool.QueryRow(ctx, "SELECT origin_recipe_id::text FROM recipe.recipes WHERE id=$1", copy.ID).Scan(&origin); err != nil || origin != published.ID {
		t.Fatalf("origin %s %v", origin, err)
	}
	search, err = s.Search(ctx, user, sid, "ca rot", 10, false)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, v := range search.Items {
		if id, ok := v["id"].(string); ok {
			seen[id] = true
		}
	}
	if !seen[published.ID] || !seen[copy.ID] {
		t.Fatalf("%+v", search)
	}
	if _, err = s.Search(ctx, platform.ID(), sid, "ca rot", 10, false); err == nil {
		t.Fatal("outsider search")
	}
}
