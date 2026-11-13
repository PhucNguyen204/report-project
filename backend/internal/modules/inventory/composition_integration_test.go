//go:build integration

package inventory

import (
	"context"
	"dicho/internal/platform"
	"github.com/jackc/pgx/v5"
	"testing"
)

func TestComposePlannedKeepsSelectedDateAndName(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	var listID string
	err := f.db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		listID, err = f.s.ComposePlanned(ctx, tx, f.sid, f.uid, platform.Composition{Name: "Week meals", PlannedDate: day("2026-11-11"), Lines: []platform.MissingLine{{FoodID: f.food, UnitID: f.unit, Quantity: qty("0.3")}}})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	var name, date string
	if err = f.db.Pool.QueryRow(ctx, "SELECT name,to_char(planned_date,'YYYY-MM-DD') FROM shopping.shopping_lists WHERE id=$1", listID).Scan(&name, &date); err != nil || name != "Week meals" || date != "2026-11-11" {
		t.Fatalf("%s %s %v", name, date, err)
	}
	var amount string
	if err = f.db.Pool.QueryRow(ctx, "SELECT quantity::text FROM shopping.shopping_items WHERE list_id=$1", listID).Scan(&amount); err != nil || amount != "0.30" {
		t.Fatalf("%s %v", amount, err)
	}
}

func TestComposePlannedRejectsPastAndCompletedTarget(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if err := f.db.Tx(ctx, func(tx pgx.Tx) error {
		_, err := f.s.ComposePlanned(ctx, tx, f.sid, f.uid, platform.Composition{PlannedDate: day("2026-11-08")})
		return err
	}); err == nil {
		t.Fatal("accepted past date")
	}
	list, err := f.s.CreateList(ctx, f.sid, f.uid, ListInput{Name: "Done", PlannedDate: "2026-11-09"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Pool.Exec(ctx, "UPDATE shopping.shopping_lists SET status='completed' WHERE id=$1", list.ID); err != nil {
		t.Fatal(err)
	}
	err = f.db.Tx(ctx, func(tx pgx.Tx) error {
		_, err := f.s.ComposePlanned(ctx, tx, f.sid, f.uid, platform.Composition{TargetListID: list.ID, Lines: []platform.MissingLine{{FoodID: f.food, UnitID: f.unit, Quantity: qty("1")}}})
		return err
	})
	if err == nil {
		t.Fatal("completed target accepted")
	}
}
