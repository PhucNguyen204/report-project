//go:build integration

package media

import (
	"context"
	"dicho/internal/platform"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"net/http"
	"testing"
	"time"
)

func mediaEvent(t *testing.T, db *platform.DB, kind string) platform.Event {
	t.Helper()
	var b []byte
	if err := db.Pool.QueryRow(context.Background(), "SELECT args FROM river_job WHERE args->>'event'=$1 AND scheduled_at<=now() ORDER BY id LIMIT 1", kind).Scan(&b); err != nil {
		t.Fatal(err)
	}
	var e platform.Event
	if err := json.Unmarshal(b, &e); err != nil {
		t.Fatal(err)
	}
	return e
}

// A copied family recipe must retain its immutable image when its original personal recipe or owner is purged.
func TestCopiedRecipeImageSurvivesOriginalSpaceAndAccountPurge(t *testing.T) {
	s, db, _, owner, sid := setupMedia(t)
	ctx := context.Background()
	other := secondUser(t, db)
	original, family, copyID := platform.ID(), platform.ID(), platform.ID()
	if _, err := db.Pool.Exec(ctx, "INSERT INTO recipe.recipes(id,space_id,created_by,name,servings) VALUES($1,$2,$3,'Canh rau',2)", original, sid, owner); err != nil {
		t.Fatal(err)
	}
	id := readyImage(t, s, owner, "recipe", original)
	if _, err := db.Pool.Exec(ctx, "UPDATE recipe.recipes SET image_object_id=$2 WHERE id=$1", original, id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, "INSERT INTO household.spaces(id,kind,name,created_by) VALUES($1,'family','Gia đình',$2)", family, other); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, "INSERT INTO household.space_members(space_id,user_id,role,space_kind) VALUES($1,$2,'leader','family')", family, other); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, "INSERT INTO recipe.recipes(id,space_id,created_by,name,servings,image_object_id) VALUES($1,$2,$3,'Canh rau sao chép',2,$4)", copyID, family, other, id); err != nil {
		t.Fatal(err)
	}
	if err := db.Tx(ctx, func(tx pgx.Tx) error { return s.PurgeSpace(ctx, tx, sid) }); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, "DELETE FROM recipe.recipes WHERE id=$1", original); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(ctx, other, id); err != nil {
		t.Fatalf("surviving copy unreadable %v", err)
	}
	if _, err := s.Read(ctx, owner, id); err == nil {
		t.Fatal("original owner retained access to foreign copied recipe")
	}
	if err := s.Handle(ctx, platform.Event{Event: "account.anonymized", Payload: json.RawMessage(`{"userId":"` + owner + `"}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(ctx, other, id); err != nil {
		t.Fatalf("shared image deleted with owner %v", err)
	}
	if err := db.Tx(ctx, func(tx pgx.Tx) error { return s.PurgeSpace(ctx, tx, family) }); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(ctx, other, id); err == nil {
		t.Fatal("image survived final reference purge")
	}
}

// Metadata deletion and durable blob cleanup must roll back together; unrelated avatars remain private and usable.
func TestSpacePurgeAtomicallySchedulesRealBlobDeletion(t *testing.T) {
	s, db, _, user, sid := setupMedia(t)
	ctx := context.Background()
	avatar := readyImage(t, s, user, "avatar", user)
	group := readyImage(t, s, user, "space", sid)
	download, err := s.Read(ctx, user, group)
	if err != nil {
		t.Fatal(err)
	}
	original := db.EnqueueFunc
	db.EnqueueFunc = func(ctx context.Context, tx pgx.Tx, kind, key string, payload any, at time.Time) error {
		if err := original(ctx, tx, kind, key, payload, at); err != nil {
			return err
		}
		return errors.New("queue down")
	}
	if err = db.Tx(ctx, func(tx pgx.Tx) error { return s.PurgeSpace(ctx, tx, sid) }); err == nil {
		t.Fatal("purge queue failure hidden")
	}
	var count int
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM media.objects").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatal("metadata deleted without cleanup job")
	}
	db.EnqueueFunc = original
	if err = db.Tx(ctx, func(tx pgx.Tx) error { return s.PurgeSpace(ctx, tx, sid) }); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Read(ctx, user, group); err == nil {
		t.Fatal("purged image readable")
	}
	if _, err = s.Read(ctx, user, avatar); err != nil {
		t.Fatalf("unrelated avatar removed %v", err)
	}
	e := mediaEvent(t, db, "media.delete")
	if err = s.Handle(ctx, e); err != nil {
		t.Fatal(err)
	}
	if err = s.Handle(ctx, e); err != nil {
		t.Fatalf("delete replay %v", err)
	}
	response, err := http.Get(download.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 404 {
		t.Fatalf("deleted S3 object still readable: %d", response.StatusCode)
	}
}

// Anonymizing an account must remove all its owned media through durable cleanup, including shared images.
func TestAccountAnonymizationQueuesMediaCleanup(t *testing.T) {
	s, db, _, user, sid := setupMedia(t)
	ctx := context.Background()
	readyImage(t, s, user, "avatar", user)
	readyImage(t, s, user, "space", sid)
	event := platform.Event{Event: "account.anonymized", Key: "account:" + user, Payload: json.RawMessage(`{"userId":"` + user + `"}`)}
	if err := s.Handle(ctx, event); err != nil {
		t.Fatal(err)
	}
	if err := s.Handle(ctx, event); err != nil {
		t.Fatalf("replay %v", err)
	}
	var n int
	if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM media.objects WHERE uploaded_by=$1", user).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("owned media metadata survived anonymization")
	}
	if err := s.Handle(ctx, mediaEvent(t, db, "media.delete")); err != nil {
		t.Fatal(err)
	}
}
