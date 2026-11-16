//go:build integration

package inventory

import (
	"context"
	"dicho/internal/kernel"
	"dicho/internal/platform"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5"
)

func mixedDimensionLots(t *testing.T, f fixture, expiredMass bool) (Lot, Lot) {
	t.Helper()
	ctx := context.Background()
	expires, purchased := "2026-11-12", "2026-11-09"
	if expiredMass {
		expires, purchased = "2026-11-08", "2026-11-01"
	}
	mass, err := f.s.CreateLot(ctx, f.sid, f.uid, LotInput{FoodID: f.food, UnitID: f.unit, Quantity: qty("500"), StorageLocation: "chill", PurchasedOn: purchased, ExpiresOn: expires, ConfirmExpired: expiredMass})
	if err != nil {
		t.Fatal(err)
	}
	ml := "00000000-0000-7000-8000-000000000014"
	if _, err := f.db.Pool.Exec(ctx, "UPDATE catalog.foods SET default_unit_id=$2 WHERE id=$1", f.food, ml); err != nil {
		t.Fatal(err)
	}
	volume, err := f.s.CreateLot(ctx, f.sid, f.uid, LotInput{FoodID: f.food, UnitID: ml, Quantity: qty("100"), StorageLocation: "chill", PurchasedOn: "2026-11-09", ExpiresOn: "2026-11-10"})
	if err != nil {
		t.Fatal(err)
	}
	return mass, volume
}

func TestMealConsumptionNeverCombinesDifferentDimensions(t *testing.T) {
	for _, mode := range []string{"fefo", "selected-match", "selected-mismatch", "legacy-mismatch", "blank-mixed", "blank-selected-mixed", "invalid-dimension"} {
		t.Run(mode, func(t *testing.T) {
			f := setup(t)
			ctx := context.Background()
			mass, volume := mixedDimensionLots(t, f, false)
			line := platform.Consumption{FoodID: f.food, Quantity: qty("100"), Dimension: "mass"}
			wantStatus := 0
			switch mode {
			case "selected-match":
				line.LotIDs = []string{mass.ID}
			case "selected-mismatch":
				line.LotIDs = []string{volume.ID}
				wantStatus = 404
			case "legacy-mismatch":
				line.LotID = volume.ID
				wantStatus = 404
			case "blank-mixed":
				line.Dimension = ""
				wantStatus = 400
			case "blank-selected-mixed":
				line.Dimension = ""
				line.LotIDs = []string{mass.ID, volume.ID}
				wantStatus = 400
			case "invalid-dimension":
				line.Dimension = "weight"
				wantStatus = 400
			}
			meal := platform.ID()
			err := f.db.Tx(ctx, func(tx pgx.Tx) error { return f.s.Consume(ctx, tx, f.sid, meal, []platform.Consumption{line}) })
			if wantStatus == 0 && err != nil {
				t.Fatal(err)
			}
			if wantStatus != 0 {
				var typed *platform.Error
				if !errors.As(err, &typed) || typed.Status != wantStatus {
					t.Fatalf("dimension guard error%v want%d", err, wantStatus)
				}
			}
			var massRemaining, volumeRemaining kernel.Quantity
			if err := f.db.Pool.QueryRow(ctx, "SELECT (SELECT base_remaining::text FROM pantry.food_lots WHERE id=$1),(SELECT base_remaining::text FROM pantry.food_lots WHERE id=$2)", mass.ID, volume.ID).Scan(&massRemaining, &volumeRemaining); err != nil {
				t.Fatal(err)
			}
			wantMass := qty("500")
			if wantStatus == 0 {
				wantMass = qty("400")
			}
			if massRemaining != wantMass || volumeRemaining != qty("100") {
				t.Fatalf("mixed-dimension mutation mass%s volume%s", massRemaining, volumeRemaining)
			}
		})
	}
}

func TestPantryHTTPFEFOKeepsMatchedLotDimension(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(fmt.Sprint(expired), func(t *testing.T) {
			f := setup(t)
			mass, volume := mixedDimensionLots(t, f, expired)
			w := api(t, f, "POST", "/it4788/v1/spaces/"+f.sid+"/lots/"+mass.ID+"/consume", fmt.Sprintf(`{"quantity":100,"unitId":%q,"strategy":"fefo"}`, f.unit), f.uid, "")
			want := 200
			if expired {
				want = 422
			}
			if w.Code != want {
				t.Fatalf("FEFO dimension/expiry guard HTTP%d want%d", w.Code, want)
			}
			var massRemaining, volumeRemaining kernel.Quantity
			if err := f.db.Pool.QueryRow(context.Background(), "SELECT (SELECT base_remaining::text FROM pantry.food_lots WHERE id=$1),(SELECT base_remaining::text FROM pantry.food_lots WHERE id=$2)", mass.ID, volume.ID).Scan(&massRemaining, &volumeRemaining); err != nil {
				t.Fatal(err)
			}
			wantMass := qty("400")
			if expired {
				wantMass = qty("500")
			}
			if massRemaining != wantMass || volumeRemaining != qty("100") {
				t.Fatalf("HTTP FEFO mixeddimensions mass%s volume%s", massRemaining, volumeRemaining)
			}
		})
	}
}

func TestMealDeductionOrderDoesNotChangeAfterLotMetadataEdit(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	first := f.stock(t, "100", "2026-11-10")
	second := f.stock(t, "100", "2026-11-12")
	meal := platform.ID()
	if err := f.db.Tx(ctx, func(tx pgx.Tx) error {
		return f.s.Consume(ctx, tx, f.sid, meal, []platform.Consumption{{FoodID: f.food, Dimension: "mass", Quantity: qty("150")}})
	}); err != nil {
		t.Fatal(err)
	}
	var before, after []platform.StockDeduction
	if err := f.db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		before, err = f.s.MealDeductions(ctx, tx, f.sid, meal)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(before) != 2 {
		t.Fatalf("missing deduction fixture %+v", before)
	}
	if _, err := f.db.Pool.Exec(ctx, "UPDATE pantry.food_lots SET expires_on='2026-12-01' WHERE id=$1", first.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		after, err = f.s.MealDeductions(ctx, tx, f.sid, meal)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("metadata edit changed historical order before%+v after%+v; second%s", before, after, second.ID)
	}
}
