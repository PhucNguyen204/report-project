//go:build integration

package media

import (
	"context"
	"dicho/internal/platform"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func secondUser(t *testing.T, db *platform.DB) string {
	t.Helper()
	id := platform.ID()
	if _, err := db.Pool.Exec(context.Background(), "INSERT INTO identity.users(id,email,full_name,password_hash,status) VALUES($1,$2,'Thành viên','unused','active')", id, id+"@test.local"); err != nil {
		t.Fatal(err)
	}
	return id
}
func readyImage(t *testing.T, s *Service, user, purpose, target string) string {
	t.Helper()
	data := picture(t)
	g, err := s.Upload(context.Background(), user, UploadInput{purpose, target, "image/png", int64(len(data))})
	if err != nil {
		t.Fatal(err)
	}
	putImage(t, g, data)
	if err = s.Complete(context.Background(), user, g.ObjectID); err != nil {
		t.Fatal(err)
	}
	return g.ObjectID
}

// Reading private avatars must allow current family members while excluding strangers and removed members.
func TestPrivateAvatarReadUsesCurrentFamilyMembership(t *testing.T) {
	s, db, _, owner, _ := setupMedia(t)
	ctx := context.Background()
	id := readyImage(t, s, owner, "avatar", owner)
	own, err := s.Read(ctx, owner, id)
	if err != nil {
		t.Fatal(err)
	}
	if own.URL == "" || own.ExpiresAt.Sub(db.Clock.Now()) > 15*time.Minute+time.Second {
		t.Fatalf("download %+v", own)
	}
	parsed, _ := url.Parse(own.URL)
	if parsed.Query().Get("X-Amz-Expires") != "900" {
		t.Fatalf("unexpected URL duration %s", own.URL)
	}
	response, err := http.Get(own.URL)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || len(data) == 0 {
		t.Fatal("private image not downloadable")
	}
	other := secondUser(t, db)
	if _, err = s.Read(ctx, other, id); err == nil {
		t.Fatal("stranger read avatar")
	}
	family := platform.ID()
	if _, err = db.Pool.Exec(ctx, "INSERT INTO household.spaces(id,kind,name,created_by) VALUES($1,'family','Gia đình',$2)", family, owner); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, "INSERT INTO household.space_members(space_id,user_id,role,space_kind) VALUES($1,$2,'leader','family'),($1,$3,'member','family')", family, owner, other); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Read(ctx, other, id); err != nil {
		t.Fatalf("family avatar %v", err)
	}
	if _, err = db.Pool.Exec(ctx, "DELETE FROM household.space_members WHERE space_id=$1 AND user_id=$2", family, other); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Read(ctx, other, id); err == nil {
		t.Fatal("removed member read avatar")
	}
}

// Write authority must stay stricter than shared reads for group, recipe, and private-food images.
func TestMediaReadScopeDiffersFromWriteScope(t *testing.T) {
	s, db, _, owner, sid := setupMedia(t)
	ctx := context.Background()
	other := secondUser(t, db)
	stranger := secondUser(t, db)
	if _, err := db.Pool.Exec(ctx, "INSERT INTO household.space_members(space_id,user_id,role,space_kind) VALUES($1,$2,'member','personal')", sid, other); err != nil {
		t.Fatal(err)
	}
	recipe, food := platform.ID(), platform.ID()
	if _, err := db.Pool.Exec(ctx, "INSERT INTO recipe.recipes(id,space_id,created_by,name,servings) VALUES($1,$2,$3,'Canh rau củ',2)", recipe, sid, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, "INSERT INTO catalog.foods(id,name,category_id,scope,owner_space_id,default_unit_id) VALUES($1,'Rau riêng','00000000-0000-7000-8000-000000000001','space',$2,'00000000-0000-7000-8000-000000000011')", food, sid); err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for purpose, target := range map[string]string{"space": sid, "recipe": recipe, "food": food} {
		id := readyImage(t, s, owner, purpose, target)
		ids[purpose] = id
		if _, err := s.Read(ctx, other, id); err != nil {
			t.Fatalf("%s member read %v", purpose, err)
		}
		if _, err := s.Read(ctx, stranger, id); err == nil {
			t.Fatalf("%s stranger read", purpose)
		}
		if purpose != "food" {
			if _, err := s.Upload(ctx, other, UploadInput{purpose, target, "image/png", 20}); err == nil {
				t.Fatalf("%s member got write authority", purpose)
			}
		}
	}
	if _, err := db.Pool.Exec(ctx, "UPDATE recipe.recipes SET kind='template',space_id=NULL WHERE id=$1", recipe); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(ctx, stranger, ids["recipe"]); err != nil {
		t.Fatalf("published template %v", err)
	}
	if _, err := db.Pool.Exec(ctx, "UPDATE recipe.recipes SET status='hidden' WHERE id=$1", recipe); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(ctx, stranger, ids["recipe"]); err == nil {
		t.Fatal("hidden template read")
	}
	if _, err := db.Pool.Exec(ctx, "UPDATE catalog.foods SET scope='global',owner_space_id=NULL WHERE id=$1", food); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(ctx, stranger, ids["food"]); err != nil {
		t.Fatalf("global food %v", err)
	}
	if _, err := s.Upload(ctx, stranger, UploadInput{"food", food, "image/png", 20}); err == nil {
		t.Fatal("global food writable")
	}
}
