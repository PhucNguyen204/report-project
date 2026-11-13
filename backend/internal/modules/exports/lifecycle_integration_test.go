//go:build integration

package exports

import (
	"context"
	"dicho/internal/platform"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"net/http"
	"testing"
)

// Purging a space must durably delete its export blobs and make already queued export jobs harmless.
func TestPurgeSpaceRemovesExportAndDeletesRealS3File(t *testing.T) {
	s, db, _, user, sid := setupExport(t)
	ctx := context.Background()
	out, err := s.Request(ctx, user, sid, exportInput(db, "csv"))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Process(ctx, out.ID); err != nil {
		t.Fatal(err)
	}
	ready, err := s.Get(ctx, user, sid, out.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Tx(ctx, func(tx pgx.Tx) error { return s.PurgeSpace(ctx, tx, sid) }); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(ctx, user, sid, out.ID); err == nil {
		t.Fatal("purged export still accessible")
	}
	if err = s.Process(ctx, out.ID); err != nil {
		t.Fatalf("stale job not harmless %v", err)
	}
	var b []byte
	if err = db.Pool.QueryRow(ctx, "SELECT args FROM river_job WHERE args->>'event'='export.delete'").Scan(&b); err != nil {
		t.Fatal(err)
	}
	var e platform.Event
	if err = json.Unmarshal(b, &e); err != nil {
		t.Fatal(err)
	}
	if err = s.Handle(ctx, e); err != nil {
		t.Fatal(err)
	}
	if err = s.Handle(ctx, e); err != nil {
		t.Fatalf("delete replay %v", err)
	}
	response, err := http.Get(ready.URL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 404 {
		t.Fatalf("deleted export still downloadable %d", response.StatusCode)
	}
}
