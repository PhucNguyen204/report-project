//go:build integration

package planning

import (
	"context"
	"dicho/internal/modules/inventory"
	"dicho/internal/platform"
	"github.com/jackc/pgx/v5"
	"strings"
	"testing"
)

func addPlanningUser(t *testing.T, db *platform.DB, sid, role string) string {
	t.Helper()
	id := platform.ID()
	ctx := context.Background()
	if _, err := db.Pool.Exec(ctx, "INSERT INTO identity.users(id,email,full_name,password_hash,status) VALUES($1,$2,'Người dùng','hash','active')", id, id+"@test.local"); err != nil {
		t.Fatal(err)
	}
	if role != "" {
		if _, err := db.Pool.Exec(ctx, "INSERT INTO household.space_members(space_id,user_id,role,space_kind) VALUES($1,$2,$3,'personal')", sid, id, role); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

func TestRecipeMutationsEnforceCreatorLeaderAndRollbackInvalidReplacement(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	member := addPlanningUser(t, db, sid, "member")
	outsider := addPlanningUser(t, db, sid, "")
	in := RecipeInput{Name: "Canh ban đầu", Servings: 2, Steps: []string{"Rửa rau", "Nấu chín"}, Notes: "ít muối", Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("0.2")}}}
	r, err := s.CreateRecipe(ctx, member, sid, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateRecipe(ctx, outsider, sid, r.ID, 1, in); err == nil {
		t.Fatal("outsider changed recipe")
	}
	if _, err = s.CreateRecipe(ctx, outsider, sid, in); err == nil {
		t.Fatal("outsider created recipe")
	}
	if err = s.DeleteRecipe(ctx, outsider, sid, r.ID); err == nil {
		t.Fatal("outsider deleted recipe")
	}
	other := addPlanningUser(t, db, sid, "member")
	if _, err = s.UpdateRecipe(ctx, other, sid, r.ID, 1, in); err == nil {
		t.Fatal("noncreator member changed recipe")
	}
	if err = s.DeleteRecipe(ctx, other, sid, r.ID); err == nil {
		t.Fatal("noncreator member deleted recipe")
	}
	changed := in
	changed.Name = "Tên không nên lưu"
	changed.Ingredients = []IngredientInput{{FoodID: platform.ID(), UnitID: unit, Quantity: qty("1")}}
	if _, err = s.UpdateRecipe(ctx, member, sid, r.ID, 1, changed); err == nil {
		t.Fatal("unknown replacement food accepted")
	}
	got, err := s.GetRecipe(ctx, user, sid, r.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Canh ban đầu" || got.Version != 1 || len(got.Ingredients) != 1 || got.Ingredients[0].Quantity != qty("0.2") || len(got.Steps) != 2 {
		t.Fatalf("replacement partially committed %+v", got)
	}
	in.Name = "Trưởng nhóm sửa"
	got, err = s.UpdateRecipe(ctx, user, sid, r.ID, 1, in)
	if err != nil || got.Version != 2 {
		t.Fatalf("leader update %+v %v", got, err)
	}
	if _, err = s.CopyRecipe(ctx, user, sid, r.ID, ""); err == nil {
		t.Fatal("space recipe copied as template")
	}
	if err = s.SetFavorite(ctx, outsider, r.ID, true); err == nil {
		t.Fatal("outsider favorited private recipe")
	}
	if err = s.DeleteRecipe(ctx, user, sid, platform.ID()); err == nil {
		t.Fatal("missing recipe deletion succeeded")
	}
	if err = s.DeleteRecipe(ctx, user, sid, r.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.SetFavorite(ctx, member, r.ID, false); err == nil {
		t.Fatal("deleted recipe remains visible")
	}
}

func TestMealsRejectInvalidTransitionsWithoutChangingPlannedState(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	today := db.Today().Format("2006-01-02")
	outsider := platform.ID()
	r, err := s.CreateRecipe(ctx, user, sid, RecipeInput{Name: "Món thử", Servings: 1, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("0.2")}}})
	if err != nil {
		t.Fatal(err)
	}
	good := MealInput{RecipeID: r.ID, Date: today, MealSlot: "lunch", Servings: 1}
	for name, in := range map[string]MealInput{"date": {Name: "Món thử", Date: "bad", MealSlot: "lunch", Servings: 1}, "servings": {Name: "Món thử", Date: today, MealSlot: "lunch", Servings: 0}, "slot": {Name: "Món thử", Date: today, MealSlot: "snack", Servings: 1}, "name": {Name: "a", Date: today, MealSlot: "lunch", Servings: 1}, "recipe": {RecipeID: platform.ID(), Date: today, MealSlot: "lunch", Servings: 1}} {
		t.Run(name, func(t *testing.T) {
			if _, err := s.CreateMeal(ctx, user, sid, in); err == nil {
				t.Fatal("invalid meal accepted")
			}
		})
	}
	m, err := s.CreateMeal(ctx, user, sid, good)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateMeal(ctx, outsider, sid, good); err == nil {
		t.Fatal("outsider created meal")
	}
	if _, err = s.UpdateMeal(ctx, outsider, sid, m.ID, 1, good); err == nil {
		t.Fatal("outsider edited meal")
	}
	if _, err = s.UpdateMeal(ctx, user, sid, m.ID, 0, good); err == nil {
		t.Fatal("missing version accepted")
	}
	if _, err = s.UpdateMeal(ctx, user, sid, platform.ID(), 1, good); err == nil {
		t.Fatal("missing meal updated")
	}
	bad := good
	bad.Date = "bad"
	if _, err = s.UpdateMeal(ctx, user, sid, m.ID, 1, bad); err == nil {
		t.Fatal("invalid date update accepted")
	}
	good.MealSlot = "dinner"
	updated, err := s.UpdateMeal(ctx, user, sid, m.ID, 1, good)
	if err != nil || updated.Version != 2 {
		t.Fatalf("%+v %v", updated, err)
	}
	if err = s.DeleteMeal(ctx, outsider, sid, m.ID); err == nil {
		t.Fatal("outsider deleted meal")
	}
	if err = s.DeleteMeal(ctx, user, sid, platform.ID()); err == nil {
		t.Fatal("missing meal deleted")
	}
	if _, err = s.Cook(ctx, outsider, sid, m.ID, CookInput{}); err == nil {
		t.Fatal("outsider cooked meal")
	}
	if _, err = s.Cook(ctx, user, sid, platform.ID(), CookInput{}); err == nil {
		t.Fatal("missing meal cooked")
	}
	if _, err = s.Cook(ctx, user, sid, m.ID, CookInput{Servings: 21}); err == nil {
		t.Fatal("invalid actual servings")
	}
	if _, err = s.Cook(ctx, user, sid, m.ID, CookInput{}); err == nil {
		t.Fatal("unconfigured stock accepted")
	}
	if _, err = s.Cook(ctx, user, sid, m.ID, CookInput{SkipStock: true}); err != nil {
		t.Fatal(err)
	}
	if err = s.DeleteMeal(ctx, user, sid, m.ID); err == nil {
		t.Fatal("cooked meal deleted")
	}
	future := good
	future.Date = db.Today().AddDate(0, 0, 1).Format("2006-01-02")
	fm, err := s.CreateMeal(ctx, user, sid, future)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Cook(ctx, user, sid, fm.ID, CookInput{SkipStock: true}); err == nil {
		t.Fatal("future meal cooked")
	}
	free := good
	free.RecipeID = ""
	free.Name = "Món tự do"
	fr, err := s.CreateMeal(ctx, user, sid, free)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Cook(ctx, user, sid, fr.ID, CookInput{SkipStock: true}); err == nil {
		t.Fatal("free text meal cooked as recipe")
	}
	var state string
	if err = db.Pool.QueryRow(ctx, "SELECT status FROM mealplan.meal_entries WHERE id=$1", fm.ID).Scan(&state); err != nil || state != "planned" {
		t.Fatalf("future state %s %v", state, err)
	}
}

func TestCookAdjustedConsumptionValidatesEveryLineAndUsesExactAmount(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	inv := inventory.New(db, s.catalog)
	s.stock = inv
	r, err := s.CreateRecipe(ctx, user, sid, RecipeInput{Name: "Canh tùy chỉnh", Servings: 1, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("0.2")}}})
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.CreateMeal(ctx, user, sid, MealInput{RecipeID: r.ID, Date: db.Today().Format("2006-01-02"), MealSlot: "lunch", Servings: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Tx(ctx, func(tx pgx.Tx) error {
		_, err := inv.AddLots(ctx, tx, sid, user, []platform.StockInput{{FoodID: food, UnitID: unit, Quantity: qty("0.5"), PurchasedOn: db.Today(), ExpiresOn: db.Today().AddDate(0, 0, 3), Location: "chill"}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for name, lines := range map[string][]CookLine{"zero": {{FoodID: food, UnitID: unit}}, "precision": {{FoodID: food, UnitID: unit, Quantity: qty("0.001")}}, "duplicate": {{FoodID: food, UnitID: unit, Quantity: qty("0.1")}, {FoodID: food, UnitID: unit, Quantity: qty("0.1")}}, "other food": {{FoodID: "00000000-0000-7000-8000-000000000022", UnitID: unit, Quantity: qty("0.1")}}, "unit": {{FoodID: food, UnitID: platform.ID(), Quantity: qty("0.1")}}} {
		t.Run(name, func(t *testing.T) {
			if _, err := s.Cook(ctx, user, sid, m.ID, CookInput{Lines: lines}); err == nil {
				t.Fatal("invalid adjustment accepted")
			}
		})
	}
	got, err := s.Cook(ctx, user, sid, m.ID, CookInput{Servings: 2, Lines: []CookLine{{FoodID: food, UnitID: unit, Quantity: qty("0.15")}}})
	if err != nil || got.Status != "cooked" {
		t.Fatalf("%+v %v", got, err)
	}
	var remaining string
	var actual int
	if err = db.Pool.QueryRow(ctx, "SELECT remaining_qty::text FROM pantry.food_lots WHERE space_id=$1", sid).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if err = db.Pool.QueryRow(ctx, "SELECT actual_servings FROM mealplan.meal_entries WHERE id=$1", m.ID).Scan(&actual); err != nil {
		t.Fatal(err)
	}
	if remaining != "0.35" || actual != 2 {
		t.Fatalf("remaining %s servings %d", remaining, actual)
	}
}

func TestMissingRejectsInvalidConfirmationWithoutWritingLists(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	inv := inventory.New(db, s.catalog)
	s.stock = inv
	s.lists = inv
	r, err := s.CreateRecipe(ctx, user, sid, RecipeInput{Name: "Canh đủ", Servings: 1, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("0.2")}}})
	if err != nil {
		t.Fatal(err)
	}
	today := db.Today().Format("2006-01-02")
	for name, in := range map[string]RecipeMissingInput{
		"no lines": {Servings: 1, PlannedDate: today}, "date": {Servings: 1, PlannedDate: "bad", Lines: []ConfirmLine{{FoodID: food, UnitID: unit, Quantity: qty("0.1")}}}, "past": {Servings: 1, PlannedDate: db.Today().AddDate(0, 0, -1).Format("2006-01-02"), Lines: []ConfirmLine{{FoodID: food, UnitID: unit, Quantity: qty("0.1")}}}, "zero": {Servings: 1, PlannedDate: today, Lines: []ConfirmLine{{FoodID: food, UnitID: unit}}}, "duplicate": {Servings: 1, PlannedDate: today, Lines: []ConfirmLine{{FoodID: food, UnitID: unit, Quantity: qty("0.1")}, {FoodID: food, UnitID: unit, Quantity: qty("0.1")}}}, "food": {Servings: 1, PlannedDate: today, Lines: []ConfirmLine{{FoodID: platform.ID(), UnitID: unit, Quantity: qty("0.1")}}}, "name": {Servings: 1, PlannedDate: today, Name: strings.Repeat("a", 101), Lines: []ConfirmLine{{FoodID: food, UnitID: unit, Quantity: qty("0.1")}}}, "servings": {Servings: 0, DryRun: true}} {
		t.Run(name, func(t *testing.T) {
			if _, err := s.MissingRecipe(ctx, user, sid, r.ID, in); err == nil {
				t.Fatal("invalid confirmation accepted")
			}
		})
	}
	for _, in := range []GenerateInput{{From: "bad", To: today, DryRun: true}, {From: today, To: "bad", DryRun: true}, {From: db.Today().AddDate(0, 0, 1).Format("2006-01-02"), To: today, DryRun: true}, {From: today, To: db.Today().AddDate(0, 0, 61).Format("2006-01-02"), DryRun: true}} {
		if _, err := s.Generate(ctx, user, sid, in); err == nil {
			t.Fatal("invalid generation range accepted")
		}
	}
	var n int
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM shopping.shopping_lists").Scan(&n); err != nil || n != 0 {
		t.Fatalf("invalid confirmations wrote %d lists %v", n, err)
	}
	s.lists = nil
	if _, err = s.MissingRecipe(ctx, user, sid, r.ID, RecipeMissingInput{Servings: 1, PlannedDate: today, Lines: []ConfirmLine{{FoodID: food, UnitID: unit, Quantity: qty("0.1")}}}); err == nil {
		t.Fatal("missing composer accepted")
	}
	s.stock = nil
	if _, err = s.MissingRecipe(ctx, user, sid, r.ID, RecipeMissingInput{Servings: 1, DryRun: true}); err == nil {
		t.Fatal("missing stock accepted")
	}
}
