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
	"testing"
)

func TestIdempotentHandlerReadsItsUncommittedWrites(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	_, err := db.Pool.Exec(ctx, "CREATE TABLE visible(value int); INSERT INTO visible VALUES(0)")
	if err != nil {
		t.Fatal(err)
	}
	h := platform.Idempotency(db, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := db.Tx(r.Context(), func(tx pgx.Tx) error { _, err := tx.Exec(r.Context(), "UPDATE visible SET value=1"); return err }); err != nil {
			platform.Fail(w, err)
			return
		}
		var value int
		if err := db.Pool.QueryRow(r.Context(), "SELECT value FROM visible").Scan(&value); err != nil || value != 1 {
			platform.Fail(w, platform.Err(500, 5000, "read-your-writes failed"))
			return
		}
		platform.Write(w, 201, value)
	}))
	r := httptest.NewRequest("POST", "/it4788/v1/test", strings.NewReader(`{}`))
	r.Header.Set("Idempotency-Key", platform.ID())
	r = r.WithContext(platform.WithPrincipal(r.Context(), platform.Principal{UserID: "00000000-0000-7000-8000-000000000099"}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 201 {
		t.Fatalf("got%d %s", w.Code, w.Body)
	}
}

func TestTransactionPanicReleasesConnectionAndRollsBack(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	_, err := db.Pool.Exec(ctx, "CREATE TABLE panic_rows(value int)")
	if err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() { _ = recover() }()
		_ = db.Tx(ctx, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, "INSERT INTO panic_rows VALUES(1)"); err != nil {
				t.Fatal(err)
			}
			panic("handler panic")
		})
	}()
	if acquired := db.Pool.Stat().AcquiredConns(); acquired != 0 {
		t.Fatalf("leaked %d connections", acquired)
	}
	var count int
	if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM panic_rows").Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial transaction %d %v", count, err)
	}
}
