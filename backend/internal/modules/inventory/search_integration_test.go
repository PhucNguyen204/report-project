//go:build integration

package inventory

import (
	"context"
	"dicho/internal/platform"
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
		out, err := f.s.SearchLotsPage(ctx, tx, f.sid, f.uid, platform.LotSearchQuery{Query: "ca rot", Limit: 20})
		if err != nil {
			return err
		}
		if len(out.Items) != 1 || out.Items[0]["name"] != "Cà rốt tím" {
			t.Fatalf("results %+v", out)
		}
		if _, err := f.s.SearchLotsPage(ctx, tx, "00000000-0000-7000-8000-999999999999", f.uid, platform.LotSearchQuery{Query: "ca rot", Limit: 20}); err == nil {
			t.Fatal("foreign pantry search authorized")
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSearchLotsTreatsPercentAndUnderscoreAsLiteralCharacters(t *testing.T) {
	f := setup(t)
	f.stock(t, "1", "2026-11-12")
	for _, query := range []string{"%", "_"} {
		if err := f.db.Tx(context.Background(), func(tx pgx.Tx) error {
			out, err := f.s.SearchLotsPage(context.Background(), tx, f.sid, f.uid, platform.LotSearchQuery{Query: query, Limit: 20})
			if err != nil {
				return err
			}
			if len(out.Items) != 0 {
				t.Fatalf("literal query%q matched plain food %+v", query, out)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}
