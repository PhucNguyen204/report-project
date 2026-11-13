//go:build integration

package planning

import (
	"context"
	"dicho/internal/testutil"
	"testing"
	"time"
)

func TestTemplateMutationHoldsActorAuthorityDuringResourceWait(t *testing.T) {
	s, db, _, _, unit, food := setupPlanning(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	actor := planningPageAdmin(t, db)
	in := TemplateInput{RecipeInput: RecipeInput{Name: "Before authorized save", Servings: 1, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("1")}}}, Status: "published"}
	r, err := s.SaveTemplate(ctx, actor, "", 0, in)
	if err != nil {
		t.Fatal(err)
	}
	blocker, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(context.Background())
	if _, err = blocker.Exec(ctx, "SELECT id FROM recipe.recipes WHERE id=$1 FOR UPDATE", r.ID); err != nil {
		t.Fatal(err)
	}
	in.Name = "Authorized save before demotion"
	saved := make(chan error, 1)
	go func() { _, err := s.SaveTemplate(ctx, actor, r.ID, r.Version, in); saved <- err }()
	testutil.WaitForDatabaseLock(t, ctx, db, "SELECT version FROM recipe.recipes", saved)
	demoted := make(chan error, 1)
	go func() {
		_, err := db.Pool.Exec(ctx, "UPDATE identity.users SET system_role='user' WHERE id=$1", actor)
		demoted <- err
	}()
	testutil.WaitForDatabaseLock(t, ctx, db, "UPDATE identity.users SET system_role='user'", demoted)
	if err = blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-saved; err != nil {
		t.Fatal(err)
	}
	if err = <-demoted; err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveTemplate(ctx, actor, r.ID, r.Version+1, in); err == nil {
		t.Fatal("demoted actor retained template authority")
	}
}
