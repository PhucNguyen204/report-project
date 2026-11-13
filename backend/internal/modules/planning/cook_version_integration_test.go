//go:build integration

package planning

import (
	"context"
	"dicho/internal/modules/inventory"
	"dicho/internal/platform"
	"github.com/jackc/pgx/v5"
	"net/http"
	"testing"
)

func TestCookHTTPRequiresValidIfMatchBeforeChangingMeal(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	r, err := s.CreateRecipe(ctx, user, sid, RecipeInput{Name: "Canh", Servings: 1, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("0.2")}}})
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.CreateMeal(ctx, user, sid, MealInput{RecipeID: r.ID, Date: db.Today().Format("2006-01-02"), MealSlot: "lunch", Servings: 1})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.Register(mux)
	for _, header := range []string{"", "bad", "0", "-1"} {
		t.Run("header="+header, func(t *testing.T) {
			status, body := planningRequest(t, mux, platform.Principal{UserID: user}, "POST", "/it4788/v1/spaces/"+sid+"/meals/"+m.ID+"/cook", map[string]bool{"skipStock": true}, header)
			if status != 400 || body["code"] != float64(2002) {
				t.Fatalf("invalid If-Match %q accepted: %d %+v", header, status, body)
			}
		})
	}
	var state string
	var version int64
	if err = db.Pool.QueryRow(ctx, "SELECT status,version FROM mealplan.meal_entries WHERE id=$1", m.ID).Scan(&state, &version); err != nil || state != "planned" || version != 1 {
		t.Fatalf("header error mutated meal %s %d %v", state, version, err)
	}
}

func TestCookHTTPStalePlannedVersionRollsBackBeforeConsumptionAndReplayIsIdempotent(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	inv := inventory.New(db, s.catalog)
	s.stock = inv
	if err := db.Tx(ctx, func(tx pgx.Tx) error {
		_, err := inv.AddLots(ctx, tx, sid, user, []platform.StockInput{{FoodID: food, UnitID: unit, Quantity: qty("0.5"), PurchasedOn: db.Today(), ExpiresOn: db.Today().AddDate(0, 0, 3), Location: "chill"}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	r, err := s.CreateRecipe(ctx, user, sid, RecipeInput{Name: "Canh", Servings: 1, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("0.2")}}})
	if err != nil {
		t.Fatal(err)
	}
	in := MealInput{RecipeID: r.ID, Date: db.Today().Format("2006-01-02"), MealSlot: "lunch", Servings: 1}
	m, err := s.CreateMeal(ctx, user, sid, in)
	if err != nil {
		t.Fatal(err)
	}
	in.Servings = 2
	if _, err = s.UpdateMeal(ctx, user, sid, m.ID, 1, in); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.Register(mux)
	p := platform.Principal{UserID: user}
	path := "/it4788/v1/spaces/" + sid + "/meals/" + m.ID + "/cook"
	status, body := planningRequest(t, mux, p, "POST", path, CookInput{}, "1")
	if status != 409 || body["code"] != float64(4002) {
		t.Fatalf("stale planned meal cooked: %d %+v", status, body)
	}
	var state, remaining string
	var version int64
	var movements int
	if err = db.Pool.QueryRow(ctx, "SELECT status,version FROM mealplan.meal_entries WHERE id=$1", m.ID).Scan(&state, &version); err != nil {
		t.Fatal(err)
	}
	if err = db.Pool.QueryRow(ctx, "SELECT remaining_qty::text FROM pantry.food_lots WHERE space_id=$1", sid).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM pantry.stock_movements WHERE meal_id=$1 AND kind='consume'", m.ID).Scan(&movements); err != nil {
		t.Fatal(err)
	}
	if state != "planned" || version != 2 || remaining != "0.50" || movements != 0 {
		t.Fatalf("stale call changed state %s %d stock=%s movements=%d", state, version, remaining, movements)
	}
	status, body = planningRequest(t, mux, p, "POST", path, CookInput{}, "\"2\"")
	if status != 200 || body["data"].(map[string]any)["version"] != float64(3) {
		t.Fatalf("current version failed %d %+v", status, body)
	}
	status, body = planningRequest(t, mux, p, "POST", path, CookInput{Servings: 1}, "1")
	if status != 200 || body["data"].(map[string]any)["version"] != float64(3) {
		t.Fatalf("cooked replay failed %d %+v", status, body)
	}
	if err = db.Pool.QueryRow(ctx, "SELECT remaining_qty::text FROM pantry.food_lots WHERE space_id=$1", sid).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM pantry.stock_movements WHERE meal_id=$1 AND kind='consume'", m.ID).Scan(&movements); err != nil {
		t.Fatal(err)
	}
	if remaining != "0.10" || movements != 1 {
		t.Fatalf("replay consumed twice stock=%s movements=%d", remaining, movements)
	}
}
