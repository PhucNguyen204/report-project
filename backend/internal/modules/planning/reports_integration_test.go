//go:build integration

package planning

import (
	"context"
	"dicho/internal/modules/inventory"
	"dicho/internal/platform"
	"github.com/jackc/pgx/v5"
	"testing"
)

func TestReportWasteUsesBaseQuantityAndSeparatesUnpricedLots(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	inv := inventory.New(db, s.catalog)
	cost := int64(12000)
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		ids, err := inv.AddLots(ctx, tx, sid, user, []platform.StockInput{{FoodID: food, UnitID: unit, Quantity: qty("1"), PurchasedOn: db.Today(), ExpiresOn: db.Today().AddDate(0, 0, 3), Location: "chill", TotalCost: &cost}, {FoodID: food, UnitID: unit, Quantity: qty("1"), PurchasedOn: db.Today(), ExpiresOn: db.Today().AddDate(0, 0, 3), Location: "chill"}})
		if err != nil {
			return err
		}
		for i, id := range ids {
			amount := "200"
			if i == 1 {
				amount = "100"
			}
			if _, err = tx.Exec(ctx, "INSERT INTO pantry.stock_movements(id,space_id,lot_id,food_id,kind,quantity,reason,actor_id) VALUES($1,$2,$3,$4,'discard',$5,'expired',$6)", platform.ID(), sid, id, food, amount, user); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.Report(ctx, user, sid, "waste", db.Today(), db.Today(), "day")
	if err != nil {
		t.Fatal(err)
	}
	if out.TotalCost != 2400 || out.UnpricedLots != 1 || out.Lots != 2 || out.Quantities["mass"] != qty("300") || out.WasteRate != 1 {
		t.Fatalf("%+v", out)
	}
	if len(out.Series) != 1 || out.Empty {
		t.Fatalf("%+v", out)
	}
	if len(out.Categories) != 1 || out.Categories[0].TotalCost != 2400 {
		t.Fatalf("category breakdown %+v", out)
	}
	if _, err = s.Report(ctx, platform.ID(), sid, "waste", db.Today(), db.Today(), "day"); err == nil {
		t.Fatal("outsider report")
	}
	if _, err = s.Report(ctx, user, sid, "waste", db.Today(), db.Today().AddDate(1, 0, 1), "day"); err == nil {
		t.Fatal("range over12 months accepted")
	}
	empty, err := s.Report(ctx, user, sid, "consumption", db.Today(), db.Today(), "month")
	if err != nil || !empty.Empty || empty.TotalCost != 0 {
		t.Fatalf("%+v %v", empty, err)
	}
}

func TestReportComparesEqualPreviousPeriodAcrossVietnamMidnight(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	inv := inventory.New(db, s.catalog)
	cost := int64(12000)
	if err := db.Tx(ctx, func(tx pgx.Tx) error {
		ids, err := inv.AddLots(ctx, tx, sid, user, []platform.StockInput{{FoodID: food, UnitID: unit, Quantity: qty("1"), PurchasedOn: db.Today(), ExpiresOn: db.Today().AddDate(0, 0, 3), Location: "chill", TotalCost: &cost}})
		if err != nil {
			return err
		}
		for _, v := range []struct{ amount, at string }{{"100", "2026-11-08T16:59:59Z"}, {"200", "2026-11-08T17:00:00Z"}, {"500", "2026-11-09T17:00:00Z"}} {
			if _, err = tx.Exec(ctx, "INSERT INTO pantry.stock_movements(id,space_id,lot_id,food_id,kind,quantity,reason,actor_id,occurred_at) VALUES($1,$2,$3,$4,'discard',$5,'expired',$6,$7)", platform.ID(), sid, ids[0], food, v.amount, user, v.at); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Report(ctx, user, sid, "waste", day("2026-11-09"), day("2026-11-09"), "day")
	if err != nil {
		t.Fatal(err)
	}
	if got.TotalCost != 2400 || got.Quantities["mass"] != qty("200") || got.Previous.TotalCost != 1200 || got.Previous.Quantities["mass"] != qty("100") || got.Previous.From != "2026-11-08" || got.Previous.To != "2026-11-08" || got.Change.TotalCost != 1200 || got.Change.Quantities["mass"] != qty("100") {
		t.Fatalf("%+v previous=%+v change=%+v", got, got.Previous, got.Change)
	}
	if len(got.Series) != 1 || got.Series[0].Period != "2026-11-09" {
		t.Fatalf("series %+v", got.Series)
	}
}

func TestReportRejectsInclusiveRangeExceedingTwelveMonths(t *testing.T) {
	s, _, user, sid, _, _ := setupPlanning(t)
	if _, err := s.Report(context.Background(), user, sid, "waste", day("2026-01-01"), day("2027-01-01"), "day"); err == nil {
		t.Fatal("inclusive range exceeds twelve months")
	}
	if _, err := s.Report(context.Background(), user, sid, "waste", day("2026-01-01"), day("2026-12-31"), "day"); err != nil {
		t.Fatal(err)
	}
}
