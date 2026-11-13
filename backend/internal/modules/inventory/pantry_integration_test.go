//go:build integration

package inventory

import (
	"context"
	"dicho/internal/kernel"
	"dicho/internal/platform"
	"github.com/jackc/pgx/v5"
	"testing"
)

func TestManualStockValidatesScopeDatesAndQuantity(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	lot := f.stock(t, "1.25", "2026-11-12")
	if lot.RemainingQty != qty("1.25") || lot.ExpiryState != "expiring" {
		t.Fatalf("lot %+v", lot)
	}
	var movements int
	if err := f.db.Pool.QueryRow(ctx, "SELECT count(*) FROM pantry.stock_movements WHERE lot_id=$1 AND kind='stock_in'", lot.ID).Scan(&movements); err != nil || movements != 1 {
		t.Fatalf("movement %d %v", movements, err)
	}
	base := LotInput{FoodID: f.food, UnitID: f.unit, Quantity: qty("1"), StorageLocation: "chill", PurchasedOn: "2026-11-09", ExpiresOn: "2026-11-12"}
	if _, err := f.s.CreateLot(ctx, f.sid, f.other, base); err == nil {
		t.Fatal("nonmember created lot")
	}
	for _, change := range []func(*LotInput){func(c *LotInput) { c.Quantity = qty("0") }, func(c *LotInput) { c.Quantity = qty("1.005") }, func(c *LotInput) { c.ExpiresOn = "2026-11-08" }, func(c *LotInput) { c.PurchasedOn = "2026-11-10" }, func(c *LotInput) { c.StorageLocation = "unknown" }} {
		c := base
		change(&c)
		if _, err := f.s.CreateLot(ctx, f.sid, f.uid, c); err == nil {
			t.Fatalf("accepted invalid input %+v", c)
		}
	}
}

func TestConsumeFEPORollsBackAcrossFoods(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	first := f.stock(t, "0.3", "2026-11-10")
	second := f.stock(t, "0.5", "2026-11-12")
	err := f.db.Tx(ctx, func(tx pgx.Tx) error {
		return f.s.Consume(ctx, tx, f.sid, platform.ID(), []platform.Consumption{{FoodID: f.food, Quantity: qty("0.6")}, {FoodID: platform.ID(), Quantity: qty("1")}})
	})
	if err == nil {
		t.Fatal("insufficient second food did not fail")
	}
	for _, id := range []string{first.ID, second.ID} {
		var rem kernel.Quantity
		if err := f.db.Pool.QueryRow(ctx, "SELECT remaining_qty::text FROM pantry.food_lots WHERE id=$1", id).Scan(&rem); err != nil {
			t.Fatal(err)
		}
		if (id == first.ID && rem != qty("0.3")) || (id == second.ID && rem != qty("0.5")) {
			t.Fatalf("partial decrement %s %s", id, rem)
		}
	}
	err = f.db.Tx(ctx, func(tx pgx.Tx) error {
		return f.s.Consume(ctx, tx, f.sid, platform.ID(), []platform.Consumption{{FoodID: f.food, Quantity: qty("0.6")}})
	})
	if err != nil {
		t.Fatal(err)
	}
	var state string
	var rem kernel.Quantity
	if err := f.db.Pool.QueryRow(ctx, "SELECT lifecycle,remaining_qty::text FROM pantry.food_lots WHERE id=$1", first.ID).Scan(&state, &rem); err != nil || state != "consumed" || rem != 0 {
		t.Fatalf("first %s %s %v", state, rem, err)
	}
}

func TestConsumeConvertsLotToBaseUnitWithoutLosingGram(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	kg := "00000000-0000-7000-8000-000000000012"
	lot, err := f.s.CreateLot(ctx, f.sid, f.uid, LotInput{FoodID: f.food, UnitID: kg, Quantity: qty("1"), StorageLocation: "chill", PurchasedOn: "2026-11-09", ExpiresOn: "2026-11-12"})
	if err != nil {
		t.Fatal(err)
	}
	err = f.db.Tx(ctx, func(tx pgx.Tx) error {
		return f.s.Consume(ctx, tx, f.sid, "", []platform.Consumption{{FoodID: f.food, Quantity: qty("1")}})
	})
	if err != nil {
		t.Fatalf("lost representable gram: %v", err)
	}
	var unit, initial, remaining, base string
	if err = f.db.Pool.QueryRow(ctx, "SELECT unit_id::text,initial_qty::text,remaining_qty::text,base_remaining::text FROM pantry.food_lots WHERE id=$1", lot.ID).Scan(&unit, &initial, &remaining, &base); err != nil {
		t.Fatal(err)
	}
	if unit != f.unit || initial != "1000.00" || remaining != "999.00" || base != "999.000000" {
		t.Fatalf("%s %s %s %s", unit, initial, remaining, base)
	}
}
