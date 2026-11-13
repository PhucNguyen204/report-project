//go:build integration

package media

import (
	"context"
	"dicho/internal/platform"
	"errors"
	"testing"
)

func TestAdministratorDoesNotReadFamilyImagesThroughFormerMembership(t *testing.T) {
	s, db, _, owner, _ := setupMedia(t)
	ctx := context.Background()
	family := platform.ID()
	other := secondUser(t, db)
	if _, err := db.Pool.Exec(ctx, `INSERT INTO household.spaces(id,kind,name,created_by) VALUES($1,'family','Family',$2)`, family, owner); err != nil {
		t.Fatal(err)
	}
	for user, role := range map[string]string{owner: "leader", other: "member"} {
		if _, err := db.Pool.Exec(ctx, `INSERT INTO household.space_members(space_id,user_id,role,space_kind) VALUES($1,$2,$3,'family')`, family, user, role); err != nil {
			t.Fatal(err)
		}
	}
	recipe := platform.ID()
	food := platform.ID()
	if _, err := db.Pool.Exec(ctx, `INSERT INTO catalog.foods(id,name,category_id,scope,owner_space_id,default_unit_id,status) VALUES($1,'Private food','00000000-0000-7000-8000-000000000001','space',$2,'00000000-0000-7000-8000-000000000011','pending_review')`, food, family); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO recipe.recipes(id,space_id,created_by,name,servings) VALUES($1,$2,$3,'Private family recipe',1)`, recipe, family, owner); err != nil {
		t.Fatal(err)
	}
	ownAvatar := readyImage(t, s, owner, "avatar", owner)
	otherAvatar := readyImage(t, s, other, "avatar", other)
	spacePhoto := readyImage(t, s, owner, "space", family)
	recipePhoto := readyImage(t, s, owner, "recipe", recipe)
	foodPhoto := readyImage(t, s, owner, "food", food)
	for _, id := range []string{otherAvatar, spacePhoto, recipePhoto, foodPhoto} {
		if _, err := s.Read(ctx, owner, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE identity.users SET system_role='admin',token_version=token_version+1 WHERE id=$1`, owner); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{otherAvatar, spacePhoto, recipePhoto, foodPhoto} {
		_, err := s.Read(ctx, owner, id)
		var typed *platform.Error
		if !errors.As(err, &typed) || typed.Status != 404 {
			t.Fatalf("admin inherited family image access: err%v", err)
		}
	}
	if _, err := s.Read(ctx, owner, ownAvatar); err != nil {
		t.Fatalf("own avatar denied%v", err)
	}
	if _, err := s.Read(ctx, other, ownAvatar); err == nil {
		t.Fatal("administrator profile photo exposed through former membership")
	}
	if _, err := s.Upload(ctx, owner, UploadInput{"food", food, "image/png", int64(len(picture(t)))}); err == nil {
		t.Fatal("administrator inherited private food image upload")
	}
}

func TestAdministratorCannotUploadOrFinalizeFormerSpaceImages(t *testing.T) {
	s, db, _, owner, sid := setupMedia(t)
	ctx := context.Background()
	data := picture(t)
	pending, err := s.Upload(ctx, owner, UploadInput{"space", sid, "image/png", int64(len(data))})
	if err != nil {
		t.Fatal(err)
	}
	putImage(t, pending, data)
	if _, err := db.Pool.Exec(ctx, `UPDATE identity.users SET system_role='admin' WHERE id=$1`, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upload(ctx, owner, UploadInput{"space", sid, "image/png", int64(len(data))}); err == nil {
		t.Fatal("admin inherited space upload permission")
	}
	if err := s.Complete(ctx, owner, pending.ObjectID); err == nil {
		t.Fatal("admin finalized a former space grant")
	}
	var state string
	if err := db.Pool.QueryRow(ctx, `SELECT status FROM media.objects WHERE id=$1`, pending.ObjectID).Scan(&state); err != nil || state != "pending" {
		t.Fatalf("grant state%s err%v", state, err)
	}
}
func TestAdministratorManagesTemplatePhotoWithoutGrantingHiddenPhotoToUsers(t *testing.T) {
	s, db, _, admin, _ := setupMedia(t)
	ctx := context.Background()
	other := secondUser(t, db)
	template := platform.ID()
	if _, err := db.Pool.Exec(ctx, `INSERT INTO recipe.recipes(id,kind,status,created_by,name,servings) VALUES($1,'template','hidden',$2,'Hidden template',1)`, template, admin); err != nil {
		t.Fatal(err)
	}
	data := picture(t)
	if _, err := s.Upload(ctx, other, UploadInput{"recipe", template, "image/png", int64(len(data))}); err == nil {
		t.Fatal("ordinary user uploaded template image")
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE identity.users SET system_role='admin' WHERE id=$1`, admin); err != nil {
		t.Fatal(err)
	}
	id := readyImage(t, s, admin, "recipe", template)
	if _, err := s.Read(ctx, admin, id); err != nil {
		t.Fatalf("admin cannot preview hidden template: %v", err)
	}
	if _, err := s.Read(ctx, other, id); err == nil {
		t.Fatal("hidden template image leaked")
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE recipe.recipes SET status='published' WHERE id=$1`, template); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(ctx, other, id); err != nil {
		t.Fatalf("published template image unavailable: %v", err)
	}
}
