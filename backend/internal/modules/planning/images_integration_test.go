//go:build integration

package planning

import (
	"context"
	"dicho/internal/modules/media"
	"dicho/internal/platform"
	"github.com/jackc/pgx/v5"
	"net/http"
	"testing"
)

func recipeImage(t *testing.T, db *platform.DB, user, target, purpose, status string) string {
	t.Helper()
	id := platform.ID()
	ctx := context.Background()
	var key any
	var completed any
	if status == "ready" {
		key = "immutable/" + id
		completed = db.Clock.Now()
	}
	if _, err := db.Pool.Exec(ctx, "INSERT INTO media.objects(id,uploaded_by,purpose,target_id,content_type,size,staging_key,object_key,status,expires_at,completed_at) VALUES($1,$2,$3,$4,'image/png',128,$5,$6,$7,now()+interval '5 minutes',$8)", id, user, purpose, target, "staging/"+id, key, status, completed); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestRecipePhotoAttachesOnlyReadyOwnedMatchingMediaAndCanBeCleared(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	s.ValidateMedia = media.New(db, nil).Validate
	in := RecipeInput{Name: "Canh có ảnh", Servings: 1, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("0.2")}}}
	r, err := s.CreateRecipe(ctx, user, sid, in)
	if err != nil {
		t.Fatal(err)
	}
	ready := recipeImage(t, db, user, r.ID, "recipe", "ready")
	in.ImageObjectID = &ready
	attached, err := s.UpdateRecipe(ctx, user, sid, r.ID, 1, in)
	if err != nil || attached.ImageObjectID == nil || *attached.ImageObjectID != ready {
		t.Fatalf("photo %+v %v", attached, err)
	}
	mux := http.NewServeMux()
	s.Register(mux)
	status, body := planningRequest(t, mux, platform.Principal{UserID: user}, "GET", "/it4788/v1/spaces/"+sid+"/recipes/"+r.ID, nil, "")
	if status != 200 || body["data"].(map[string]any)["imageObjectId"] != ready {
		t.Fatalf("photo API %d %+v", status, body)
	}
	other := addPlanningUser(t, db, sid, "")
	for name, object := range map[string]string{"pending": recipeImage(t, db, user, r.ID, "recipe", "pending"), "owner": recipeImage(t, db, other, r.ID, "recipe", "ready"), "target": recipeImage(t, db, user, platform.ID(), "recipe", "ready"), "purpose": recipeImage(t, db, user, user, "avatar", "ready")} {
		t.Run(name, func(t *testing.T) {
			bad := in
			bad.ImageObjectID = &object
			bad.Name = "Không nên lưu"
			if _, err := s.UpdateRecipe(ctx, user, sid, r.ID, 2, bad); err == nil {
				t.Fatal("invalid photo attached")
			}
		})
	}
	still, err := s.GetRecipe(ctx, user, sid, r.ID, 0)
	if err != nil || still.ImageObjectID == nil || *still.ImageObjectID != ready || still.Version != 2 || still.Name != "Canh có ảnh" {
		t.Fatalf("photo mutation was not atomic %+v %v", still, err)
	}
	if err = db.Tx(ctx, func(tx pgx.Tx) error {
		candidates, err := s.suggestionCandidates(ctx, tx, sid, user, "")
		if err != nil {
			return err
		}
		if len(candidates) != 1 || candidates[0].Recipe.ImageObjectID == nil || *candidates[0].Recipe.ImageObjectID != ready {
			t.Fatalf("suggestion photo %+v", candidates)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	empty := ""
	in.ImageObjectID = &empty
	cleared, err := s.UpdateRecipe(ctx, user, sid, r.ID, 2, in)
	if err != nil || cleared.ImageObjectID != nil {
		t.Fatalf("clear %+v %v", cleared, err)
	}
}

func TestRecipePhotoValidationFailureRollsBackCreateAndTemplate(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	id := platform.ID()
	in := RecipeInput{Name: "Canh có ảnh", Servings: 1, ImageObjectID: &id, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("0.2")}}}
	if _, err := s.CreateRecipe(ctx, user, sid, in); err == nil {
		t.Fatal("photo accepted without validator")
	}
	var n int
	if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM recipe.recipes").Scan(&n); err != nil || n != 0 {
		t.Fatalf("partial recipe %d %v", n, err)
	}
	s.ValidateMedia = media.New(db, nil).Validate
	if _, err := s.CreateRecipe(ctx, user, sid, in); err == nil {
		t.Fatal("unknown media accepted")
	}
	admin := addPlanningUser(t, db, sid, "")
	if _, err := db.Pool.Exec(ctx, "UPDATE identity.users SET system_role='admin' WHERE id=$1", admin); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveTemplate(ctx, admin, "", 0, TemplateInput{RecipeInput: in, Status: "published"}); err == nil {
		t.Fatal("template accepts unknown media")
	}
	if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM recipe.recipes").Scan(&n); err != nil || n != 0 {
		t.Fatalf("partial template %d %v", n, err)
	}
}

func TestTrustedRecipeCopiesPreserveImmutablePhotoReference(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	r, err := s.CreateRecipe(ctx, user, sid, RecipeInput{Name: "Canh gốc", Servings: 1, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("0.2")}}})
	if err != nil {
		t.Fatal(err)
	}
	image := recipeImage(t, db, user, r.ID, "recipe", "ready")
	if _, err = db.Pool.Exec(ctx, "UPDATE recipe.recipes SET image_object_id=$2 WHERE id=$1", r.ID, image); err != nil {
		t.Fatal(err)
	}
	target := platform.ID()
	if _, err = db.Pool.Exec(ctx, "INSERT INTO household.spaces(id,kind,name,created_by) VALUES($1,'family','Copy',$2)", target, user); err != nil {
		t.Fatal(err)
	}
	if err = db.Tx(ctx, func(tx pgx.Tx) error { return s.CopySpace(ctx, tx, sid, target, user, nil) }); err != nil {
		t.Fatal(err)
	}
	var copiedImage string
	if err = db.Pool.QueryRow(ctx, "SELECT image_object_id::text FROM recipe.recipes WHERE space_id=$1", target).Scan(&copiedImage); err != nil || copiedImage != image {
		t.Fatalf("space copy photo %s %v", copiedImage, err)
	}
	admin := addPlanningUser(t, db, sid, "")
	if _, err = db.Pool.Exec(ctx, "UPDATE identity.users SET system_role='admin' WHERE id=$1", admin); err != nil {
		t.Fatal(err)
	}
	template, err := s.SaveTemplate(ctx, admin, "", 0, TemplateInput{RecipeInput: RecipeInput{Name: "Mẫu có ảnh", Servings: 1, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("0.2")}}}, Status: "published"})
	if err != nil {
		t.Fatal(err)
	}
	templateImage := recipeImage(t, db, admin, template.ID, "recipe", "ready")
	if _, err = db.Pool.Exec(ctx, "UPDATE recipe.recipes SET image_object_id=$2 WHERE id=$1", template.ID, templateImage); err != nil {
		t.Fatal(err)
	}
	copy, err := s.CopyRecipe(ctx, user, sid, template.ID, "")
	if err != nil || copy.ImageObjectID == nil || *copy.ImageObjectID != templateImage {
		t.Fatalf("template copy photo %+v %v", copy, err)
	}
}
