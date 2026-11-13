//go:build integration

package exports

import (
	"context"
	"dicho/internal/platform"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// Expired pending/ready exports must be removed by the scheduler and their private S3 objects durably deleted.
func TestCleanupRemovesExpiredExportsAndPreservesUnexpiredRequests(t *testing.T) {
	s, db, _, user, sid := setupExport(t)
	ctx := context.Background()
	ready, err := s.Request(ctx, user, sid, exportInput(db, "csv"))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Process(ctx, ready.ID); err != nil {
		t.Fatal(err)
	}
	download, err := s.Get(ctx, user, sid, ready.ID)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := s.Request(ctx, user, sid, exportInput(db, "pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, "UPDATE report.exports SET expires_at=$1", db.Clock.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	live, err := s.Request(ctx, user, sid, exportInput(db, "csv"))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM report.exports").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expired exports survived %d", count)
	}
	if _, err = s.Get(ctx, user, sid, live.ID); err != nil {
		t.Fatalf("unexpired request removed %v", err)
	}
	if err = s.Process(ctx, pending.ID); err != nil {
		t.Fatalf("expired queued job %v", err)
	}
	var b []byte
	if err = db.Pool.QueryRow(ctx, "SELECT args FROM river_job WHERE args->>'event'='export.delete' AND scheduled_at<=now()").Scan(&b); err != nil {
		t.Fatal(err)
	}
	var e platform.Event
	if err = json.Unmarshal(b, &e); err != nil {
		t.Fatal(err)
	}
	if err = s.Handle(ctx, e); err != nil {
		t.Fatal(err)
	}
	response, err := http.Get(download.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 404 {
		t.Fatalf("expired export still stored %d", response.StatusCode)
	}
}
