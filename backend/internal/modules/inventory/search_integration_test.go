//go:build integration

package inventory

import (
	"context"
	"github.com/jackc/pgx/v5"
	"testing"
)

func TestSearchLotsFoldsVietnameseAndScopesResults(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	f.stock(t, "1", "2026-11-12")
	if _, err := f.db.Pool.Exec(ctx, "UPDATE catalog.foods SET name='Cà rốt tím' WHERE id=$1", f.food); err != nil {
		t.Fatal(err)
	}
	err := f.db.Tx(ctx, func(tx pgx.Tx) error {
		out, err := f.s.SearchLots(ctx, tx, f.sid, "ca rot")
		if err != nil {
			return err
		}
		if len(out) != 1 || out[0]["name"] != "Cà rốt tím" {
			t.Fatalf("results %+v", out)
		}
		other, err := f.s.SearchLots(ctx, tx, "00000000-0000-7000-8000-999999999999", "ca rot")
		if err != nil {
			return err
		}
		if len(other) != 0 {
			t.Fatalf("cross-space %+v", other)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
