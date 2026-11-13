//go:build integration

package media

import (
	"context"
	"dicho/internal/platform"
	"dicho/internal/testutil"
	"testing"
	"time"
)

func TestTemplateUploadHoldsActorAuthorityDuringResourceWait(t *testing.T) {
	s, db, _, actor, _ := setupMedia(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if _, err := db.Pool.Exec(ctx, "UPDATE identity.users SET system_role='admin' WHERE id=$1", actor); err != nil {
		t.Fatal(err)
	}
	recipe := platform.ID()
	if _, err := db.Pool.Exec(ctx, "INSERT INTO recipe.recipes(id,created_by,name,servings,kind,status) VALUES($1,$2,'Template authority',1,'template','draft')", recipe, actor); err != nil {
		t.Fatal(err)
	}
	blocker, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(context.Background())
	if _, err = blocker.Exec(ctx, "SELECT id FROM recipe.recipes WHERE id=$1 FOR UPDATE", recipe); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := s.Upload(ctx, actor, UploadInput{"recipe", recipe, "image/png", 100}); done <- err }()
	testutil.WaitForDatabaseLock(t, ctx, db, "SELECT space_id::text,created_by::text,kind", done)
	demoted := make(chan error, 1)
	go func() {
		_, err := db.Pool.Exec(ctx, "UPDATE identity.users SET system_role='user' WHERE id=$1", actor)
		demoted <- err
	}()
	testutil.WaitForDatabaseLock(t, ctx, db, "UPDATE identity.users SET system_role='user'", demoted)
	if err = blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	if err = <-demoted; err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upload(ctx, actor, UploadInput{"recipe", recipe, "image/png", 100}); err == nil {
		t.Fatal("demoted actor retained template image authority")
	}
}

func TestCompletionWaitingForActorDoesNotLockObjectBeforeProfile(t *testing.T) {
	s, db, _, actor, _ := setupMedia(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	imageID := readyImage(t, s, actor, "avatar", actor)
	profile, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer profile.Rollback(context.Background())
	if _, err := profile.Exec(ctx, "SELECT id FROM identity.users WHERE id=$1 FOR UPDATE", actor); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- s.Complete(ctx, actor, imageID)
	}()
	testutil.WaitForDatabaseLock(t, ctx, db, "SELECT system_role FROM identity.users", done)
	if _, err := profile.Exec(ctx, "SELECT id FROM media.objects WHERE id=$1 FOR SHARE NOWAIT", imageID); err != nil {
		t.Fatalf("completion held object while waiting for profile actor: %v", err)
	}
	if err := s.Validate(ctx, profile, actor, "avatar", actor, imageID); err != nil {
		t.Fatal(err)
	}
	if _, err := profile.Exec(ctx, "UPDATE identity.users SET avatar_object_id=$2 WHERE id=$1", actor, imageID); err != nil {
		t.Fatal(err)
	}
	if err := profile.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentCompletionUsesLatestReadyStateAfterObjectWait(t *testing.T) {
	s, db, store, actor, _ := setupMedia(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	data := picture(t)
	grant, err := s.Upload(ctx, actor, UploadInput{"avatar", actor, "image/png", int64(len(data))})
	if err != nil {
		t.Fatal(err)
	}
	key := "objects/" + grant.ObjectID + "/completed"
	if err = store.Put(ctx, key, "image/png", data); err != nil {
		t.Fatal(err)
	}
	finalizer, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer finalizer.Rollback(context.Background())
	if _, err := finalizer.Exec(ctx, "UPDATE media.objects SET status='ready',object_key=$2,completed_at=now() WHERE id=$1", grant.ObjectID, key); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- s.Complete(ctx, actor, grant.ObjectID) }()
	testutil.WaitForDatabaseLock(t, ctx, db, "WHERE id=$1 AND uploaded_by=$2 FOR UPDATE", done)
	if err := finalizer.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("completed image was reread from obsolete staging state: %v", err)
	}
	var current string
	if err := db.Pool.QueryRow(ctx, "SELECT object_key FROM media.objects WHERE id=$1", grant.ObjectID).Scan(&current); err != nil || current != key {
		t.Fatalf("immutable final object changed: key%q error%v", current, err)
	}
}
