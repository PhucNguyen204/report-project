//go:build integration

package planning

import (
	"context"
	"dicho/internal/modules/inventory"
	"dicho/internal/platform"
	"github.com/jackc/pgx/v5"
	"sync"
	"testing"
)

func TestMealsRespectDateAndRecipeDeletionKeepsPlannedName(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	today := db.Today().Format("2006-01-02")
	r, err := s.CreateRecipe(ctx, user, sid, RecipeInput{Name: "Canh cà rốt", Servings: 2, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("0.2")}}})
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.CreateMeal(ctx, user, sid, MealInput{RecipeID: r.ID, Date: today, MealSlot: "lunch", Servings: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateMeal(ctx, user, sid, MealInput{RecipeID: r.ID, Date: db.Today().AddDate(0, 0, 29).Format("2006-01-02"), MealSlot: "lunch", Servings: 2}); err == nil {
		t.Fatal("out of horizon accepted")
	}
	if err = s.DeleteRecipe(ctx, user, sid, r.ID); err != nil {
		t.Fatal(err)
	}
	var rid *string
	var name string
	if err = db.Pool.QueryRow(ctx, "SELECT recipe_id::text,name FROM mealplan.meal_entries WHERE id=$1", m.ID).Scan(&rid, &name); err != nil {
		t.Fatal(err)
	}
	if rid != nil || name != "Canh cà rốt" {
		t.Fatalf("%v %s", rid, name)
	}
}

func TestConcurrentCookConsumesOnlyOnceAndRejectsCookedEdits(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	inv := inventory.New(db, s.catalog)
	s.stock = inv
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		_, err := inv.AddLots(ctx, tx, sid, user, []platform.StockInput{{FoodID: food, UnitID: unit, Quantity: qty("0.5"), PurchasedOn: db.Today(), ExpiresOn: db.Today().AddDate(0, 0, 3), Location: "chill"}})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.CreateRecipe(ctx, user, sid, RecipeInput{Name: "Canh cà rốt", Servings: 2, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("0.2")}}})
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.CreateMeal(ctx, user, sid, MealInput{RecipeID: r.ID, Date: db.Today().Format("2006-01-02"), MealSlot: "lunch", Servings: 2})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := s.Cook(ctx, user, sid, m.ID, CookInput{Version: 1})
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var remaining string
	var moves int
	if err = db.Pool.QueryRow(ctx, "SELECT remaining_qty::text FROM pantry.food_lots WHERE space_id=$1", sid).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != "0.30" {
		t.Fatalf("remaining %s", remaining)
	}
	db.Pool.QueryRow(ctx, "SELECT count(*) FROM pantry.stock_movements WHERE meal_id=$1 AND kind='consume'", m.ID).Scan(&moves)
	if moves != 1 {
		t.Fatalf("duplicate moves %d", moves)
	}
	if _, err = s.UpdateMeal(ctx, user, sid, m.ID, 1, MealInput{Name: "Changed", Date: db.Today().Format("2006-01-02"), MealSlot: "lunch", Servings: 2}); err == nil {
		t.Fatal("cooked edited")
	}
}

func TestCookShortStockRollsBackMealAndAllLots(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	inv := inventory.New(db, s.catalog)
	s.stock = inv
	err := db.Tx(ctx, func(tx pgx.Tx) error {
		_, err := inv.AddLots(ctx, tx, sid, user, []platform.StockInput{{FoodID: food, UnitID: unit, Quantity: qty("0.1"), PurchasedOn: db.Today(), ExpiresOn: db.Today().AddDate(0, 0, 3), Location: "chill"}})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.CreateRecipe(ctx, user, sid, RecipeInput{Name: "Canh cà rốt", Servings: 2, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("0.2")}}})
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.CreateMeal(ctx, user, sid, MealInput{RecipeID: r.ID, Date: db.Today().Format("2006-01-02"), MealSlot: "lunch", Servings: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Cook(ctx, user, sid, m.ID, CookInput{}); err == nil {
		t.Fatal("insufficient stock cooked")
	}
	var state, remaining string
	db.Pool.QueryRow(ctx, "SELECT status FROM mealplan.meal_entries WHERE id=$1", m.ID).Scan(&state)
	db.Pool.QueryRow(ctx, "SELECT remaining_qty::text FROM pantry.food_lots WHERE space_id=$1", sid).Scan(&remaining)
	if state != "planned" || remaining != "0.10" {
		t.Fatalf("partial commit %s %s", state, remaining)
	}
	if _, err = s.Cook(ctx, user, sid, m.ID, CookInput{SkipStock: true}); err != nil {
		t.Fatal(err)
	}
}
