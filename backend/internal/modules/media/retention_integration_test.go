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

// Cleanup must remove abandoned uploads and 30-day unbound images while preserving bound images and retryable deletion jobs.
func TestCleanupRetainsBoundImagesAndDurablyDeletesExpiredUploads(t *testing.T) {
	s, db, store, user, _ := setupMedia(t)
	ctx := context.Background()
	bound := readyImage(t, s, user, "avatar", user)
	orphan := readyImage(t, s, user, "avatar", user)
	orphanURL, err := s.Read(ctx, user, orphan)
	if err != nil {
		t.Fatal(err)
	}
	data := picture(t)
	pending, err := s.Upload(ctx, user, UploadInput{"avatar", user, "image/png", int64(len(data))})
	if err != nil {
		t.Fatal(err)
	}
	putImage(t, pending, data)
	if _, err = db.Pool.Exec(ctx, "UPDATE identity.users SET avatar_object_id=$2 WHERE id=$1", user, bound); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, "UPDATE media.objects SET expires_at=$1,completed_at=CASE WHEN status='ready' THEN $2::timestamptz ELSE NULL END", db.Clock.Now().Add(-time.Minute), db.Clock.Now().Add(-31*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var stage string
	if err = db.Pool.QueryRow(ctx, "SELECT staging_key FROM media.objects WHERE id=$1", bound).Scan(&stage); err != nil {
		t.Fatal(err)
	}
	original := db.EnqueueFunc
	db.EnqueueFunc = func(ctx context.Context, tx pgx.Tx, kind, key string, payload any, at time.Time) error {
		if err := original(ctx, tx, kind, key, payload, at); err != nil {
			return err
		}
		return errors.New("queue unavailable")
	}
	if err = s.Cleanup(ctx); err == nil {
		t.Fatal("cleanup queue failure hidden")
	}
	db.EnqueueFunc = original
	var count int
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM media.objects").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatal("failed cleanup removed metadata")
	}
	if err = s.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM media.objects").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("orphan uploads survived cleanup: %d", count)
	}
	rows, err := db.Pool.Query(ctx, "SELECT args FROM river_job WHERE args->>'event'='media.delete' AND scheduled_at<=now() ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	var events []platform.Event
	for rows.Next() {
		var b []byte
		if err = rows.Scan(&b); err != nil {
			t.Fatal(err)
		}
		var e platform.Event
		if err = json.Unmarshal(b, &e); err != nil {
			t.Fatal(err)
		}
		events = append(events, e)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if err = s.Handle(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	for _, rawURL := range []string{orphanURL.URL, func() string {
		u, e := store.DownloadURL(ctx, stage, "", time.Minute)
		if e != nil {
			t.Fatal(e)
		}
		return u
	}()} {
		response, err := http.Get(rawURL)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 404 {
			t.Fatalf("expired object remained %d", response.StatusCode)
		}
	}
	if _, err = s.Read(ctx, user, bound); err != nil {
		t.Fatalf("bound image removed %v", err)
	}
	var before, after int
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM river_job WHERE args->>'event'='media.delete'").Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err = s.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM river_job WHERE args->>'event'='media.delete'").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatal("cleanup replay duplicated blob jobs")
	}
}
