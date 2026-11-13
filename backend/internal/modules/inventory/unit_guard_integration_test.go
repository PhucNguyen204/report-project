//go:build integration

package inventory

import (
	"context"
	"errors"
	"testing"

	"dicho/internal/platform"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestUnitDimensionIsImmutableWhenOnlyInventoryReferencesIt(t *testing.T) {
	for _, kind := range []string{"lot", "item"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t)
			ctx := context.Background()
			unit := platform.ID()
			if _, err := f.db.Pool.Exec(ctx, "INSERT INTO catalog.units(id,name,dimension,factor_to_base) VALUES($1,'special mass','mass',2)", unit); err != nil {
				t.Fatal(err)
			}
			if kind == "lot" {
				if _, err := f.s.CreateLot(ctx, f.sid, f.uid, LotInput{FoodID: f.food, UnitID: unit, Quantity: qty("1"), StorageLocation: "chill", PurchasedOn: "2026-11-09", ExpiresOn: "2026-11-12"}); err != nil {
					t.Fatal(err)
				}
			} else {
				l, err := f.s.CreateList(ctx, f.sid, f.uid, ListInput{Name: "Guard", PlannedDate: "2026-11-09"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = f.s.AddItem(ctx, f.sid, f.uid, l.ID, ItemInput{Name: "Free food", UnitID: unit, Quantity: qty("1")}); err != nil {
					t.Fatal(err)
				}
			}
			_, err := f.db.Pool.Exec(ctx, "UPDATE catalog.units SET dimension='volume' WHERE id=$1", unit)
			var pgerr *pgconn.PgError
			if !errors.As(err, &pgerr) || pgerr.Code != "23514" {
				t.Fatalf("referenced unit dimension changed: %v", err)
			}
			if _, err = f.db.Pool.Exec(ctx, "UPDATE catalog.units SET name='renamed unit' WHERE id=$1", unit); err != nil {
				t.Fatal(err)
			}
		})
	}
}
