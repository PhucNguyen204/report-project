//go:build integration

package platform_test

import (
	"context"
	"dicho/internal/platform"
	"dicho/internal/testutil"
	"github.com/jackc/pgx/v5"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestIdempotencyConcurrentRequestsAndRollback(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	_, err := db.Pool.Exec(ctx, "CREATE TABLE counter(value integer); INSERT INTO counter VALUES(0)")
	if err != nil {
		t.Fatal(err)
	}
	h := platform.Idempotency(db, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err := db.Tx(r.Context(), func(tx pgx.Tx) error { _, err := tx.Exec(r.Context(), "UPDATE counter SET value=value+1"); return err })
		if err != nil {
			platform.Fail(w, err)
			return
		}
		platform.Write(w, 201, map[string]any{"created": true})
	}))
	request := func(body, key string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/it4788/v1/test", strings.NewReader(body))
		r.Header.Set("Idempotency-Key", key)
		r = r.WithContext(platform.WithPrincipal(r.Context(), platform.Principal{UserID: "00000000-0000-7000-8000-000000000099"}))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	var wg sync.WaitGroup
	results := make(chan *httptest.ResponseRecorder, 2)
	start := make(chan struct{})
	for range 2 {
		wg.Go(func() { <-start; results <- request(`{"n":1}`, "same") })
	}
	close(start)
	wg.Wait()
	close(results)
	for w := range results {
		if w.Code != 201 {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
	}
	var count int
	if err = db.Pool.QueryRow(ctx, "SELECT value FROM counter").Scan(&count); err != nil || count != 1 {
		t.Fatalf("count %d %v", count, err)
	}
	if w := request(`{"n":2}`, "same"); w.Code != 400 {
		t.Fatalf("different body replayed: %d", w.Code)
	}
	failing := platform.Idempotency(db, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = db.Tx(r.Context(), func(tx pgx.Tx) error { _, err := tx.Exec(r.Context(), "UPDATE counter SET value=99"); return err })
		platform.Fail(w, platform.Rule("reject"))
	}))
	r := httptest.NewRequest("POST", "/it4788/v1/test", strings.NewReader(`{}`))
	r.Header.Set("Idempotency-Key", "failed")
	r = r.WithContext(platform.WithPrincipal(r.Context(), platform.Principal{UserID: "00000000-0000-7000-8000-000000000099"}))
	w := httptest.NewRecorder()
	failing.ServeHTTP(w, r)
	if w.Code != 422 {
		t.Fatal(w.Code)
	}
	_ = db.Pool.QueryRow(ctx, "SELECT value FROM counter").Scan(&count)
	if count != 1 {
		t.Fatalf("failed request committed %d", count)
	}
}

func TestQueueRollbackAndDedupe(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		if err := db.Enqueue(ctx, tx, "mail", "once", map[string]string{"to": "test@example.com"}, time.Now()); err != nil {
			return err
		}
		return platform.Rule("rollback")
	})
	if err == nil {
		t.Fatal("expected rollback")
	}
	var n int
	_ = db.Pool.QueryRow(ctx, "SELECT count(*) FROM river_job").Scan(&n)
	if n != 0 {
		t.Fatal("job escaped rollback")
	}
	for range 2 {
		if err = db.Tx(ctx, func(tx pgx.Tx) error {
			return db.Enqueue(ctx, tx, "mail", "once", map[string]string{"to": "test@example.com"}, time.Now())
		}); err != nil {
			t.Fatal(err)
		}
	}
	_ = db.Pool.QueryRow(ctx, "SELECT count(*) FROM river_job").Scan(&n)
	if n != 1 {
		t.Fatalf("dedupe count %d", n)
	}
}

func TestAssignmentDigestDedupeEndsWithCompletedBurst(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	enqueue := func() {
		t.Helper()
		if err := db.Tx(ctx, func(tx pgx.Tx) error {
			return db.Enqueue(ctx, tx, "assignment.digest", "user:list", map[string]string{"userId": "user"}, time.Now())
		}); err != nil {
			t.Fatal(err)
		}
	}
	enqueue()
	enqueue()
	var count int
	if _, err := db.Pool.Exec(ctx, "UPDATE river_job SET state='completed',finalized_at=now()"); err != nil {
		t.Fatal(err)
	}
	enqueue()
	enqueue()
	if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM river_job").Scan(&count); err != nil || count != 2 {
		t.Fatalf("bursts %d %v", count, err)
	}
}
