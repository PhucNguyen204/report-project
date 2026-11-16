//go:build integration

package inventory

import (
	"context"
	"dicho/internal/kernel"
	"dicho/internal/platform"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestMealConsumptionUsesOnlySelectedLotsIncludingExpired(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	expired, err := f.s.CreateLot(ctx, f.sid, f.uid, LotInput{FoodID: f.food, UnitID: f.unit, Quantity: qty("0.4"), StorageLocation: "chill", PurchasedOn: "2026-11-01", ExpiresOn: "2026-11-08", ConfirmExpired: true})
	if err != nil {
		t.Fatal(err)
	}
	unselected := f.stock(t, "10", "2026-11-10")
	fresh := f.stock(t, "0.7", "2026-11-12")
	meal := platform.ID()
	err = f.db.Tx(ctx, func(tx pgx.Tx) error {
		return f.s.Consume(ctx, tx, f.sid, meal, []platform.Consumption{{FoodID: f.food, Quantity: qty("0.8"), LotIDs: []string{strings.ToUpper(fresh.ID), expired.ID}}})
	})
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{expired.ID: "0", fresh.ID: "0.3", unselected.ID: "10"} {
		var remaining kernel.Quantity
		if err := f.db.Pool.QueryRow(ctx, "SELECT base_remaining::text FROM pantry.food_lots WHERE id=$1", id).Scan(&remaining); err != nil {
			t.Fatal(err)
		}
		if remaining != qty(want) {
			t.Fatalf("selection changed wrong stock: remaining%s want%s", remaining, want)
		}
	}
	var movements int
	if err := f.db.Pool.QueryRow(ctx, "SELECT count(*) FROM pantry.stock_movements WHERE meal_id=$1 AND kind='consume'", meal).Scan(&movements); err != nil || movements != 2 {
		t.Fatalf("movements%d err%v", movements, err)
	}
}

func TestMealConsumptionRejectsInvalidSelectedLotsWithoutEffects(t *testing.T) {
	for _, scenario := range []string{"duplicate", "duplicate-case", "malformed", "extra-separator", "missing", "missing-with-consumed", "foreign", "wrong-food", "deleted", "too-many", "mixed-legacy"} {
		t.Run(scenario, func(t *testing.T) {
			f := setup(t)
			ctx := context.Background()
			lot := f.stock(t, "1", "2026-11-12")
			line := platform.Consumption{FoodID: f.food, Quantity: qty("0.5"), LotIDs: []string{lot.ID}}
			wantStatus := 400
			switch scenario {
			case "duplicate":
				line.LotIDs = []string{lot.ID, lot.ID}
			case "duplicate-case":
				line.LotIDs = []string{lot.ID, strings.ToUpper(lot.ID)}
			case "malformed":
				line.LotIDs = []string{"not-a-uuid"}
			case "extra-separator":
				line.LotIDs = []string{lot.ID[:34] + "--"}
			case "missing":
				line.LotIDs = []string{platform.ID()}
				wantStatus = 404
			case "missing-with-consumed":
				line.LotIDs = []string{lot.ID, platform.ID()}
				wantStatus = 404
				if _, err := f.db.Pool.Exec(ctx, "UPDATE pantry.food_lots SET remaining_qty=0,base_remaining=0,lifecycle='consumed' WHERE id=$1", lot.ID); err != nil {
					t.Fatal(err)
				}
			case "foreign":
				wantStatus = 404
				space := platform.ID()
				if _, err := f.db.Pool.Exec(ctx, "INSERT INTO household.spaces(id,kind,name,created_by) VALUES($1,'family','Other family',$2)", space, f.other); err != nil {
					t.Fatal(err)
				}
				if _, err := f.db.Pool.Exec(ctx, "UPDATE pantry.food_lots SET space_id=$2 WHERE id=$1", lot.ID, space); err != nil {
					t.Fatal(err)
				}
			case "wrong-food":
				wantStatus = 404
				food := platform.ID()
				if _, err := f.db.Pool.Exec(ctx, "INSERT INTO catalog.foods(id,name,category_id,default_unit_id) VALUES($1,'Other food','00000000-0000-7000-8000-000000000001',$2)", food, f.unit); err != nil {
					t.Fatal(err)
				}
				if _, err := f.db.Pool.Exec(ctx, "UPDATE pantry.food_lots SET food_id=$2 WHERE id=$1", lot.ID, food); err != nil {
					t.Fatal(err)
				}
			case "deleted":
				wantStatus = 404
				if _, err := f.db.Pool.Exec(ctx, "UPDATE pantry.food_lots SET deleted_at=now() WHERE id=$1", lot.ID); err != nil {
					t.Fatal(err)
				}
			case "too-many":
				line.LotIDs = make([]string, 101)
				for i := range line.LotIDs {
					line.LotIDs[i] = platform.ID()
				}
			case "mixed-legacy":
				line.LotID = lot.ID
			}
			var before kernel.Quantity
			if err := f.db.Pool.QueryRow(ctx, "SELECT base_remaining::text FROM pantry.food_lots WHERE id=$1", lot.ID).Scan(&before); err != nil {
				t.Fatal(err)
			}
			meal := platform.ID()
			err := f.db.Tx(ctx, func(tx pgx.Tx) error { return f.s.Consume(ctx, tx, f.sid, meal, []platform.Consumption{line}) })
			var typed *platform.Error
			if !errors.As(err, &typed) || typed.Status != wantStatus {
				t.Fatalf("invalid selection returned%v", err)
			}
			var after kernel.Quantity
			var moves int
			if err := f.db.Pool.QueryRow(ctx, "SELECT base_remaining::text,(SELECT count(*) FROM pantry.stock_movements WHERE meal_id=$2) FROM pantry.food_lots WHERE id=$1", lot.ID, meal).Scan(&after, &moves); err != nil {
				t.Fatal(err)
			}
			if before != after || moves != 0 {
				t.Fatal("invalid selection changed stock or movements")
			}
		})
	}
}

func TestMealConsumptionTerminalSelectionsReportShortage(t *testing.T) {
	for _, selector := range []string{"lotIds", "lotId"} {
		for _, state := range []string{"consumed", "discarded", "zero-available"} {
			for _, expired := range []bool{false, true} {
				name := selector + "/" + state + "/fresh"
				if expired {
					name = selector + "/" + state + "/expired"
				}
				t.Run(name, func(t *testing.T) {
					f := setup(t)
					ctx := context.Background()
					expiry := "2026-11-12"
					if expired {
						expiry = "2026-11-08"
					}
					lot, err := f.s.CreateLot(ctx, f.sid, f.uid, LotInput{FoodID: f.food, UnitID: f.unit, Quantity: qty("1"), StorageLocation: "chill", PurchasedOn: "2026-11-01", ExpiresOn: expiry, ConfirmExpired: expired})
					if err != nil {
						t.Fatal(err)
					}
					other := f.stock(t, "10", "2026-11-13")
					base := "/it4788/v1/spaces/" + f.sid + "/lots/" + lot.ID
					switch state {
					case "consumed":
						w := api(t, f, "POST", base+"/consume", `{"quantity":1,"unitId":"`+f.unit+`","strategy":"lot"}`, f.uid, "")
						if w.Code != 200 {
							t.Fatalf("deplete %d %s", w.Code, w.Body.String())
						}
					case "discarded":
						w := api(t, f, "POST", base+"/discard", `{"quantity":1,"reason":"spoiled"}`, f.uid, "")
						if w.Code != 200 {
							t.Fatalf("discard %d %s", w.Code, w.Body.String())
						}
					case "zero-available":
						if _, err := f.db.Pool.Exec(ctx, "UPDATE pantry.food_lots SET base_remaining=0 WHERE id=$1", lot.ID); err != nil {
							t.Fatal(err)
						}
					}
					var beforeMoves, beforeVersion int
					if err := f.db.Pool.QueryRow(ctx, "SELECT version,(SELECT count(*) FROM pantry.stock_movements WHERE space_id=$2) FROM pantry.food_lots WHERE id=$1", lot.ID, f.sid).Scan(&beforeVersion, &beforeMoves); err != nil {
						t.Fatal(err)
					}
					line := platform.Consumption{FoodID: f.food, Dimension: "mass", Quantity: qty("0.5")}
					if selector == "lotIds" {
						line.LotIDs = []string{lot.ID, other.ID}
					} else {
						line.LotID = lot.ID
					}
					err = f.db.Tx(ctx, func(tx pgx.Tx) error { return f.s.Consume(ctx, tx, f.sid, platform.ID(), []platform.Consumption{line}) })
					var typed *platform.Error
					if !errors.As(err, &typed) || typed.Status != 422 || typed.Code != 2003 {
						t.Fatalf("known unavailable selection must report shortage, got %v", err)
					}
					var remaining, otherRemaining kernel.Quantity
					var moves, version, otherVersion int
					if err := f.db.Pool.QueryRow(ctx, "SELECT base_remaining::text,version,(SELECT count(*) FROM pantry.stock_movements WHERE space_id=$2),(SELECT base_remaining::text FROM pantry.food_lots WHERE id=$3),(SELECT version FROM pantry.food_lots WHERE id=$3) FROM pantry.food_lots WHERE id=$1", lot.ID, f.sid, other.ID).Scan(&remaining, &version, &moves, &otherRemaining, &otherVersion); err != nil {
						t.Fatal(err)
					}
					if remaining != 0 || version != beforeVersion || otherRemaining != qty("10") || otherVersion != 1 || moves != beforeMoves {
						t.Fatal("unavailable selection changed stock, versions or movements")
					}
				})
			}
		}
	}
}

func TestMealConsumptionDoesNotRevealExpiredUnmatchedSelections(t *testing.T) {
	for _, selector := range []string{"lotIds", "lotId"} {
		for _, mismatch := range []string{"missing", "space", "food", "dimension", "deleted"} {
			t.Run(selector+"/"+mismatch, func(t *testing.T) {
				f := setup(t)
				ctx := context.Background()
				lot, err := f.s.CreateLot(ctx, f.sid, f.uid, LotInput{FoodID: f.food, UnitID: f.unit, Quantity: qty("1"), StorageLocation: "chill", PurchasedOn: "2026-11-01", ExpiresOn: "2026-11-08", ConfirmExpired: true})
				if err != nil {
					t.Fatal(err)
				}
				line := platform.Consumption{FoodID: f.food, Dimension: "mass", Quantity: qty("0.5")}
				selected := lot.ID
				switch mismatch {
				case "missing":
					selected = platform.ID()
				case "space":
					line.FoodID = f.food
					space := platform.ID()
					if _, err := f.db.Pool.Exec(ctx, "INSERT INTO household.spaces(id,kind,name,created_by) VALUES($1,'family','Foreign selected lot',$2)", space, f.other); err != nil {
						t.Fatal(err)
					}
					if _, err := f.db.Pool.Exec(ctx, "UPDATE pantry.food_lots SET space_id=$2 WHERE id=$1", lot.ID, space); err != nil {
						t.Fatal(err)
					}
				case "food":
					line.FoodID = platform.ID()
				case "dimension":
					line.Dimension = "volume"
				case "deleted":
					if _, err := f.db.Pool.Exec(ctx, "UPDATE pantry.food_lots SET deleted_at=now() WHERE id=$1", lot.ID); err != nil {
						t.Fatal(err)
					}
				}
				if selector == "lotIds" {
					line.LotIDs = []string{selected}
				} else {
					line.LotID = selected
				}
				err = f.db.Tx(ctx, func(tx pgx.Tx) error { return f.s.Consume(ctx, tx, f.sid, platform.ID(), []platform.Consumption{line}) })
				var typed *platform.Error
				if !errors.As(err, &typed) || typed.Status != 404 {
					t.Fatalf("unmatched selection must remain not found, got %v", err)
				}
				var remaining kernel.Quantity
				var moves int
				if err := f.db.Pool.QueryRow(ctx, "SELECT base_remaining::text,(SELECT count(*) FROM pantry.stock_movements WHERE lot_id=$1 AND kind='consume') FROM pantry.food_lots WHERE id=$1", lot.ID).Scan(&remaining, &moves); err != nil {
					t.Fatal(err)
				}
				if remaining != qty("1") || moves != 0 {
					t.Fatal("unmatched selection changed stock")
				}
			})
		}
	}
}

func TestSelectedMealShortageRollsBackEarlierFoods(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	first := f.stock(t, "1", "2026-11-12")
	secondFood := platform.ID()
	if _, err := f.db.Pool.Exec(ctx, "INSERT INTO catalog.foods(id,name,category_id,default_unit_id) VALUES($1,'Another carrot','00000000-0000-7000-8000-000000000001',$2)", secondFood, f.unit); err != nil {
		t.Fatal(err)
	}
	second, err := f.s.CreateLot(ctx, f.sid, f.uid, LotInput{FoodID: secondFood, UnitID: f.unit, Quantity: qty("0.2"), StorageLocation: "chill", PurchasedOn: "2026-11-09", ExpiresOn: "2026-11-12"})
	if err != nil {
		t.Fatal(err)
	}
	meal := platform.ID()
	err = f.db.Tx(ctx, func(tx pgx.Tx) error {
		return f.s.Consume(ctx, tx, f.sid, meal, []platform.Consumption{{FoodID: f.food, Quantity: qty("0.5"), LotIDs: []string{first.ID}}, {FoodID: secondFood, Quantity: qty("0.5"), LotIDs: []string{second.ID}}})
	})
	var typed *platform.Error
	if !errors.As(err, &typed) || typed.Status != 422 || typed.Code != 2003 {
		t.Fatalf("shortage%v", err)
	}
	var remaining kernel.Quantity
	var moves int
	if err := f.db.Pool.QueryRow(ctx, "SELECT base_remaining::text,(SELECT count(*) FROM pantry.stock_movements WHERE meal_id=$2) FROM pantry.food_lots WHERE id=$1", first.ID, meal).Scan(&remaining, &moves); err != nil {
		t.Fatal(err)
	}
	if remaining != qty("1") || moves != 0 {
		t.Fatal("selected shortage partially committed")
	}
}

func TestMealDeductionsAreCanonicalStableAndScoped(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	kg := "00000000-0000-7000-8000-000000000012"
	lot, err := f.s.CreateLot(ctx, f.sid, f.uid, LotInput{ID: "00000000-0000-7000-8000-000000000071", FoodID: f.food, UnitID: kg, Quantity: qty("1"), StorageLocation: "chill", PurchasedOn: "2026-11-09", ExpiresOn: "2026-11-12"})
	if err != nil {
		t.Fatal(err)
	}
	earlier, err := f.s.CreateLot(ctx, f.sid, f.uid, LotInput{ID: "00000000-0000-7000-8000-000000000072", FoodID: f.food, UnitID: f.unit, Quantity: qty("100"), StorageLocation: "chill", PurchasedOn: "2026-11-09", ExpiresOn: "2026-11-10"})
	if err != nil {
		t.Fatal(err)
	}
	meal := platform.ID()
	if err = f.db.Tx(ctx, func(tx pgx.Tx) error {
		return f.s.Consume(ctx, tx, f.sid, meal, []platform.Consumption{{FoodID: f.food, Quantity: qty("300")}})
	}); err != nil {
		t.Fatal(err)
	}
	history, ok := any(f.s).(platform.StockHistoryPort)
	if !ok {
		t.Fatal("inventory does not expose authoritative meal deductions")
	}
	var first []platform.StockDeduction
	if err = f.db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		first, err = history.MealDeductions(ctx, tx, f.sid, meal)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	want := []platform.StockDeduction{{FoodID: f.food, LotID: lot.ID, Quantity: qty("200"), UnitID: f.unit, Dimension: "mass"}, {FoodID: f.food, LotID: earlier.ID, Quantity: qty("100"), UnitID: f.unit, Dimension: "mass"}}
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("history must be base quantities, got%+v", first)
	}
	foreign := platform.ID()
	if _, err := f.db.Pool.Exec(ctx, "INSERT INTO household.spaces(id,kind,name,created_by) VALUES($1,'family','Other history',$2)", foreign, f.other); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool.Exec(ctx, "INSERT INTO household.space_members(space_id,user_id,role,space_kind) VALUES($1,$2,'leader','family')", foreign, f.other); err != nil {
		t.Fatal(err)
	}
	foreignLot, err := f.s.CreateLot(ctx, foreign, f.other, LotInput{FoodID: f.food, UnitID: f.unit, Quantity: qty("10"), StorageLocation: "chill", PurchasedOn: "2026-11-09", ExpiresOn: "2026-11-12"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.db.Tx(ctx, func(tx pgx.Tx) error {
		return f.s.Consume(ctx, tx, foreign, meal, []platform.Consumption{{FoodID: f.food, Quantity: qty("1"), LotIDs: []string{foreignLot.ID}}})
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool.Exec(ctx, "UPDATE pantry.food_lots SET deleted_at=now() WHERE id=$1", lot.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.db.Tx(ctx, func(tx pgx.Tx) error {
		return f.s.Consume(ctx, tx, f.sid, meal, []platform.Consumption{{FoodID: f.food, Quantity: qty("99999")}})
	}); err != nil {
		t.Fatal(err)
	}
	if err = f.db.Tx(ctx, func(tx pgx.Tx) error {
		second, err := history.MealDeductions(ctx, tx, f.sid, meal)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatal("replay changed historical deductions")
		}
		for _, sid := range []string{platform.ID(), "00000000-0000-7000-8000-000000000099"} {
			rows, err := history.MealDeductions(ctx, tx, sid, meal)
			if err != nil {
				return err
			}
			if rows == nil || len(rows) != 0 {
				t.Fatal("history exposed another space or used null empty result")
			}
		}
		empty, err := history.MealDeductions(ctx, tx, f.sid, platform.ID())
		if err == nil && (empty == nil || len(empty) != 0) {
			t.Fatal("missing meal history must be an empty list")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyMealLotIDAcceptsUppercaseWithoutChangingExpiryConsent(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	lot := f.stock(t, "1", "2026-11-12")
	if err := f.db.Tx(ctx, func(tx pgx.Tx) error {
		return f.s.Consume(ctx, tx, f.sid, platform.ID(), []platform.Consumption{{FoodID: f.food, LotID: strings.ToUpper(lot.ID), Quantity: qty("0.5")}})
	}); err != nil {
		t.Fatalf("uppercase legacy UUID rejected: %v", err)
	}
	var remaining kernel.Quantity
	if err := f.db.Pool.QueryRow(ctx, "SELECT base_remaining::text FROM pantry.food_lots WHERE id=$1", lot.ID).Scan(&remaining); err != nil || remaining != qty("0.5") {
		t.Fatalf("legacy remaining%s err%v", remaining, err)
	}
}

func TestMealDeductionsFailClosedWhenCanonicalUnitIsMissing(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	lot := f.stock(t, "1", "2026-11-12")
	meal := platform.ID()
	if err := f.db.Tx(ctx, func(tx pgx.Tx) error {
		return f.s.Consume(ctx, tx, f.sid, meal, []platform.Consumption{{FoodID: f.food, LotID: lot.ID, Quantity: qty("0.5")}})
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool.Exec(ctx, "UPDATE catalog.units SET is_base=false WHERE id=$1", f.unit); err != nil {
		t.Fatal(err)
	}
	err := f.db.Tx(ctx, func(tx pgx.Tx) error { _, err := f.s.MealDeductions(ctx, tx, f.sid, meal); return err })
	var typed *platform.Error
	if !errors.As(err, &typed) || typed.Status != 503 {
		t.Fatalf("history hidden instead of failing closed: %v", err)
	}
	var movements int
	if err := f.db.Pool.QueryRow(ctx, "SELECT count(*) FROM pantry.stock_movements WHERE meal_id=$1", meal).Scan(&movements); err != nil || movements != 1 {
		t.Fatalf("history changed movements%d err%v", movements, err)
	}
}
