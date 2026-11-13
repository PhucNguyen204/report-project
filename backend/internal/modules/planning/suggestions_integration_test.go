//go:build integration

package planning

import (
	"context"
	"dicho/internal/modules/inventory"
	"dicho/internal/platform"
	"github.com/jackc/pgx/v5"
	"testing"
)

func TestSuggestLiveResultsRespectSettingsAndStockChanges(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	inv := inventory.New(db, s.catalog)
	s.stock = inv
	_, err := s.CreateRecipe(ctx, user, sid, RecipeInput{Name: "Canh cà rốt", Servings: 1, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("0.2")}}})
	if err != nil {
		t.Fatal(err)
	}
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		_, err := inv.AddLots(ctx, tx, sid, user, []platform.StockInput{{FoodID: food, UnitID: unit, Quantity: qty("0.3"), PurchasedOn: db.Today(), ExpiresOn: db.Today().AddDate(0, 0, 1), Location: "chill"}})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Suggest(ctx, user, sid, SuggestionInput{Servings: 1, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 1 || got.Items[0].R != 1 || got.Items[0].E != .75 || got.Meta.Fallback != "none" {
		t.Fatalf("%+v", got)
	}
	_, err = db.Pool.Exec(ctx, "INSERT INTO identity.user_settings(user_id,reminder_days) VALUES($1,1)", user)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := s.Suggest(ctx, user, sid, SuggestionInput{Servings: 1, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if changed.Items[0].E != .5 {
		t.Fatalf("settings ignored %+v", changed)
	}
	err = db.Tx(ctx, func(tx pgx.Tx) error {
		return inv.Consume(ctx, tx, sid, "", []platform.Consumption{{FoodID: food, Quantity: qty("300")}})
	})
	if err != nil {
		t.Fatal(err)
	}
	empty, err := s.Suggest(ctx, user, sid, SuggestionInput{Servings: 1, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if empty.Meta.Fallback != "empty_pantry" || len(empty.Items) != 0 {
		t.Fatalf("stale stock %+v", empty)
	}
	if _, err = s.Suggest(ctx, platform.ID(), sid, SuggestionInput{Servings: 1}); err == nil {
		t.Fatal("outsider suggestions")
	}
	if _, err = s.Suggest(ctx, user, sid, SuggestionInput{Servings: 21}); err == nil {
		t.Fatal("invalid servings accepted")
	}
}
