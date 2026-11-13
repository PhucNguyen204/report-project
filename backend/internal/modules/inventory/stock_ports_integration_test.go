//go:build integration

package inventory

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"dicho/internal/modules/accounts"
	"dicho/internal/platform"
	"github.com/jackc/pgx/v5"
	"testing"
)

func TestAvailableReturnsBaseQuantityAndValidLotsOnly(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if _, err := f.s.CreateLot(ctx, f.sid, f.uid, LotInput{FoodID: f.food, UnitID: "00000000-0000-7000-8000-000000000012", Quantity: qty("1"), StorageLocation: "chill", PurchasedOn: "2026-11-09", ExpiresOn: "2026-11-12"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.CreateLot(ctx, f.sid, f.uid, LotInput{FoodID: f.food, UnitID: f.unit, Quantity: qty("1"), StorageLocation: "chill", PurchasedOn: "2026-11-01", ExpiresOn: "2026-11-08", ConfirmExpired: true}); err != nil {
		t.Fatal(err)
	}
	err := f.db.Tx(ctx, func(tx pgx.Tx) error {
		out, err := f.s.Available(ctx, tx, f.sid, day("2026-11-09"))
		if err != nil {
			return err
		}
		if len(out) != 1 || out[0].Quantity != qty("1000") || out[0].Dimension != "mass" {
			t.Fatalf("available %+v", out)
		}
		empty, err := f.s.Available(ctx, tx, f.sid, day("2026-11-13"))
		if err != nil {
			return err
		}
		if len(empty) != 0 {
			t.Fatalf("expired %+v", empty)
		}
		foreign, err := f.s.Available(ctx, tx, "00000000-0000-7000-8000-999999999999", day("2026-11-09"))
		if err != nil {
			return err
		}
		if len(foreign) != 0 {
			t.Fatalf("cross-space %+v", foreign)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestManualStockCreatesPrivateFoodAtomicallyAndDefaultsExpiry(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	f.s = New(f.db, accounts.New(f.db, key))
	c := LotInput{UnitID: f.unit, Quantity: qty("1"), StorageLocation: "chill", ExpiresOn: "2026-11-12"}
	c.NewFood = &struct {
		Name       string `json:"name"`
		CategoryID string `json:"categoryId"`
	}{"Private spinach", "00000000-0000-7000-8000-000000000001"}
	lot, err := f.s.CreateLot(ctx, f.sid, f.uid, c)
	if err != nil {
		t.Fatal(err)
	}
	var scope, status, owner string
	if err = f.db.Pool.QueryRow(ctx, "SELECT scope,status,owner_space_id::text FROM catalog.foods WHERE id=$1", lot.FoodID).Scan(&scope, &status, &owner); err != nil || scope != "space" || status != "pending_review" || owner != f.sid {
		t.Fatalf("%s %s %s %v", scope, status, owner, err)
	}
	c.NewFood.Name = "Failed private food"
	c.ExpiresOn = "bad"
	if _, err = f.s.CreateLot(ctx, f.sid, f.uid, c); err == nil {
		t.Fatal("invalid expiry accepted")
	}
	var count int
	if err = f.db.Pool.QueryRow(ctx, "SELECT count(*) FROM catalog.foods WHERE name='Failed private food'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("private food escaped rollback %d %v", count, err)
	}
	if _, err = f.db.Pool.Exec(ctx, "INSERT INTO catalog.food_shelf_lives(food_id,storage_location,days) VALUES($1,'chill',7)", f.food); err != nil {
		t.Fatal(err)
	}
	defaulted, err := f.s.CreateLot(ctx, f.sid, f.uid, LotInput{FoodID: f.food, UnitID: f.unit, Quantity: qty("1"), StorageLocation: "chill"})
	if err != nil || defaulted.PurchasedOn != "2026-11-09" || defaulted.ExpiresOn != "2026-11-16" {
		t.Fatalf("defaults %+v %v", defaulted, err)
	}
	if _, err = f.s.CreateLot(ctx, f.sid, f.uid, LotInput{FoodID: f.food, UnitID: f.unit, Quantity: qty("1"), StorageLocation: "freezer"}); err == nil {
		t.Fatal("unknown shelf life silently defaulted")
	}
}

func TestStockPortRejectsInvalidInputAndExpiredSelection(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	for _, in := range []platform.StockInput{{FoodID: f.food, UnitID: f.unit, Quantity: 0, Location: "chill", ExpiresOn: day("2026-11-12")}, {FoodID: f.food, UnitID: f.unit, Quantity: qty("1"), Location: "bad", ExpiresOn: day("2026-11-12")}, {FoodID: f.food, UnitID: f.unit, Quantity: qty("1"), Location: "chill", PurchasedOn: day("2026-11-10"), ExpiresOn: day("2026-11-12")}, {FoodID: f.food, UnitID: "00000000-0000-7000-8000-999999999999", Quantity: qty("1"), Location: "chill", ExpiresOn: day("2026-11-12")}} {
		if err := f.db.Tx(ctx, func(tx pgx.Tx) error {
			_, err := f.s.AddLots(ctx, tx, f.sid, f.uid, []platform.StockInput{in})
			return err
		}); err == nil {
			t.Fatalf("accepted %+v", in)
		}
	}
	expired, err := f.s.CreateLot(ctx, f.sid, f.uid, LotInput{FoodID: f.food, UnitID: f.unit, Quantity: qty("1"), StorageLocation: "chill", PurchasedOn: "2026-11-01", ExpiresOn: "2026-11-08", ConfirmExpired: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.db.Tx(ctx, func(tx pgx.Tx) error {
		return f.s.Consume(ctx, tx, f.sid, "", []platform.Consumption{{FoodID: f.food, LotID: expired.ID, Quantity: qty("0.25")}})
	}); err == nil {
		t.Fatal("expired lot consumed without explicit consent")
	}
	if err = f.db.Tx(ctx, func(tx pgx.Tx) error {
		return f.s.Consume(ctx, tx, f.sid, "", []platform.Consumption{{FoodID: f.food, LotID: expired.ID, Quantity: qty("0.25"), AllowExpired: true}})
	}); err != nil {
		t.Fatal(err)
	}
	if err = f.db.Tx(ctx, func(tx pgx.Tx) error {
		return f.s.Consume(ctx, tx, f.sid, "", []platform.Consumption{{FoodID: f.food, Quantity: 0}})
	}); err == nil {
		t.Fatal("zero consumption accepted")
	}
}
