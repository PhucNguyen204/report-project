//go:build integration

package planning

import (
	"context"
	"dicho/internal/modules/accounts"
	"dicho/internal/modules/inventory"
	"dicho/internal/platform"
	"dicho/internal/testutil"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type cookContractFixture struct {
	s                              *Service
	db                             *platform.DB
	user, sid, unit, first, second string
	meal                           Meal
	lots                           []string
}

func cookContractSetup(t *testing.T, first, second string) cookContractFixture {
	t.Helper()
	s, db, user, _, unit, food := setupPlanning(t)
	ctx := context.Background()
	sid := platform.ID()
	if err := db.Tx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "INSERT INTO household.spaces(id,kind,name,created_by) VALUES($1,'family','Cooking family',$2)", sid, user); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, "INSERT INTO household.space_members(space_id,user_id,role,space_kind) VALUES($1,$2,'leader','family')", sid, user)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	inv := inventory.New(db, s.catalog)
	s.stock = inv
	f := cookContractFixture{s: s, db: db, user: user, sid: sid, unit: unit, first: food, second: "00000000-0000-7000-8000-000000000022"}
	if _, err := db.Pool.Exec(ctx, "INSERT INTO catalog.foods(id,name,category_id,default_unit_id) VALUES($1,'Củ thứ hai','00000000-0000-7000-8000-000000000001',$2)", f.second, unit); err != nil {
		t.Fatal(err)
	}
	if err := db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		f.lots, err = inv.AddLots(ctx, tx, sid, user, []platform.StockInput{
			{FoodID: f.first, UnitID: unit, Quantity: qty(first), PurchasedOn: db.Today(), ExpiresOn: db.Today().AddDate(0, 0, 3), Location: "chill"},
			{FoodID: f.second, UnitID: unit, Quantity: qty(second), PurchasedOn: db.Today(), ExpiresOn: db.Today().AddDate(0, 0, 3), Location: "chill"},
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	r, err := s.CreateRecipe(ctx, user, sid, RecipeInput{Name: "Món hợp đồng", Servings: 1, Ingredients: []IngredientInput{{FoodID: f.first, UnitID: unit, Quantity: qty("0.1")}, {FoodID: f.second, UnitID: unit, Quantity: qty("0.2")}}})
	if err != nil {
		t.Fatal(err)
	}
	f.meal, err = s.CreateMeal(ctx, user, sid, MealInput{RecipeID: r.ID, Date: db.Today().Format("2006-01-02"), MealSlot: "lunch", Servings: 1})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestCookZeroIngredientUsesOutsideStockWithoutMovement(t *testing.T) {
	f := cookContractSetup(t, "0.5", "0.5")
	got, err := f.s.Cook(context.Background(), f.user, f.sid, f.meal.ID, CookInput{Version: 1, Lines: []CookLine{{FoodID: f.first, UnitID: f.unit, Quantity: qty("0.15")}, {FoodID: f.second, UnitID: f.unit, Quantity: 0}}})
	if err != nil {
		t.Fatalf("outside ingredient rejected: %v", err)
	}
	var first, second string
	var movements int
	if err := f.db.Pool.QueryRow(context.Background(), "SELECT (SELECT remaining_qty::text FROM pantry.food_lots WHERE id=$1),(SELECT remaining_qty::text FROM pantry.food_lots WHERE id=$2),(SELECT count(*) FROM pantry.stock_movements WHERE meal_id=$3)", f.lots[0], f.lots[1], f.meal.ID).Scan(&first, &second, &movements); err != nil {
		t.Fatal(err)
	}
	if got.Status != "cooked" || first != "0.35" || second != "0.50" || movements != 1 {
		t.Fatalf("outside ingredient changed stock: %s/%s movements%d", first, second, movements)
	}
}

func TestCookSkipStockReturnsCanonicalActorTimeAndReplayMetadata(t *testing.T) {
	f := cookContractSetup(t, "0.5", "0.5")
	ctx := context.Background()
	got, err := f.s.Cook(ctx, f.user, f.sid, f.meal.ID, CookInput{SkipStock: true})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err = json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	meal, ok := body["meal"].(map[string]any)
	if !ok || body["alreadyCooked"] != false || body["deductions"] == nil || meal["cookedBy"] != f.user || meal["cookedAt"] == nil || meal["skippedStock"] != true {
		t.Fatalf("cook lacks canonical metadata: %s", raw)
	}
	oldTime := meal["cookedAt"]
	again, err := f.s.Cook(ctx, f.user, f.sid, f.meal.ID, CookInput{Version: 1, Servings: 99})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(again)
	if err = json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	meal = body["meal"].(map[string]any)
	if body["alreadyCooked"] != true || meal["cookedAt"] != oldTime || meal["cookedBy"] != f.user {
		t.Fatalf("replay changed canonical metadata: %s", raw)
	}
}

func TestCookStockConflictSnapshotFollowsSavepointRollback(t *testing.T) {
	f := cookContractSetup(t, "0.5", "0.1")
	_, err := f.s.Cook(context.Background(), f.user, f.sid, f.meal.ID, CookInput{Version: 1})
	var conflict *platform.StockConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("stock shortage did not return typed409: %v", err)
	}
	levels := map[string]platform.Quantity{}
	for _, current := range conflict.Current {
		levels[current.FoodID] += current.BaseQuantity
	}
	if levels[f.first] != qty("500") || levels[f.second] != qty("100") {
		t.Fatalf("snapshot included rolled back consumption: %+v", levels)
	}
	var status string
	var movements int
	if err := f.db.Pool.QueryRow(context.Background(), "SELECT status,(SELECT count(*) FROM pantry.stock_movements WHERE meal_id=$1) FROM mealplan.meal_entries WHERE id=$1", f.meal.ID).Scan(&status, &movements); err != nil {
		t.Fatal(err)
	}
	if status != "planned" || movements != 0 {
		t.Fatalf("shortage partially committed: %s moves%d", status, movements)
	}
}

func TestCookMultipleSelectedLotsPreservesUnselectedStockAndCanonicalHistory(t *testing.T) {
	f := cookContractSetup(t, "0.5", "0.5")
	ctx := context.Background()
	inv := inventory.New(f.db, f.s.catalog)
	var selected []string
	if err := f.db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		selected, err = inv.AddLots(ctx, tx, f.sid, f.user, []platform.StockInput{
			{FoodID: f.first, UnitID: f.unit, Quantity: qty("0.1"), PurchasedOn: f.db.Today().AddDate(0, 0, -3), ExpiresOn: f.db.Today().AddDate(0, 0, -1), Location: "chill"},
			{FoodID: f.first, UnitID: f.unit, Quantity: qty("0.1"), PurchasedOn: f.db.Today(), ExpiresOn: f.db.Today().AddDate(0, 0, 5), Location: "chill"},
		})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	result, err := f.s.Cook(ctx, f.user, f.sid, f.meal.ID, CookInput{Lines: []CookLine{{FoodID: f.first, UnitID: f.unit, Quantity: qty("0.15"), LotIDs: selected}, {FoodID: f.second, UnitID: f.unit, Quantity: qty("0.05")}}})
	if err != nil {
		t.Fatal(err)
	}
	var unselected string
	if err := f.db.Pool.QueryRow(ctx, "SELECT remaining_qty::text FROM pantry.food_lots WHERE id=$1", f.lots[0]).Scan(&unselected); err != nil {
		t.Fatal(err)
	}
	if unselected != "0.50" {
		t.Fatalf("lotIds ignored and unselected stock consumed: %s", unselected)
	}
	want := []platform.StockDeduction{
		{FoodID: f.first, LotID: selected[0], UnitID: "00000000-0000-7000-8000-000000000011", Dimension: "mass", Quantity: qty("100")},
		{FoodID: f.first, LotID: selected[1], UnitID: "00000000-0000-7000-8000-000000000011", Dimension: "mass", Quantity: qty("50")},
		{FoodID: f.second, LotID: f.lots[1], UnitID: "00000000-0000-7000-8000-000000000011", Dimension: "mass", Quantity: qty("50")},
	}
	if !reflect.DeepEqual(result.Deductions, want) {
		t.Fatalf("selected lot history has incorrect order or converted quantities: got%+v want%+v", result.Deductions, want)
	}
	replay, err := f.s.Cook(ctx, f.user, f.sid, f.meal.ID, CookInput{Version: 99, Lines: []CookLine{{FoodID: "invalid", Quantity: qty("-1")}}})
	if err != nil || !replay.AlreadyCooked || !reflect.DeepEqual(replay.Deductions, want) || !reflect.DeepEqual(replay.Details, result.Details) {
		t.Fatalf("selected lot replay changed canonical result: %+v %v", replay, err)
	}
	raw, _ := json.Marshal(result)
	var body map[string]any
	if err = json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body["alreadyCooked"] != false || body["deductions"] == nil {
		t.Fatalf("missing canonical history: %s", raw)
	}
}

func TestCookOutsideLinesKeepCatalogAndSelectionValidation(t *testing.T) {
	for _, name := range []string{"unknown-food", "wrong-unit", "negative", "precision", "duplicate-food", "malformed-selection", "duplicate-selection", "mixed-selection", "too-many-selection"} {
		t.Run(name, func(t *testing.T) {
			f := cookContractSetup(t, "0.5", "0.5")
			line := CookLine{FoodID: f.first, UnitID: f.unit}
			lines := []CookLine{line}
			switch name {
			case "unknown-food":
				lines[0].FoodID = platform.ID()
			case "wrong-unit":
				lines[0].UnitID = "00000000-0000-7000-8000-000000000014"
			case "negative":
				lines[0].Quantity = qty("-0.1")
			case "precision":
				lines[0].Quantity = qty("0.001")
			case "duplicate-food":
				lines = append(lines, line)
			case "malformed-selection":
				lines[0].Quantity = qty("0.1")
				lines[0].LotIDs = []string{"invalid"}
			case "duplicate-selection":
				lines[0].Quantity = qty("0.1")
				lines[0].LotIDs = []string{f.lots[0], strings.ToUpper(f.lots[0])}
			case "mixed-selection":
				lines[0].Quantity = qty("0.1")
				lines[0].LotID = f.lots[0]
				lines[0].LotIDs = []string{f.lots[0]}
			case "too-many-selection":
				lines[0].Quantity = qty("0.1")
				for range 101 {
					lines[0].LotIDs = append(lines[0].LotIDs, platform.ID())
				}
			}
			if _, err := f.s.Cook(context.Background(), f.user, f.sid, f.meal.ID, CookInput{Lines: lines}); err == nil {
				t.Fatal("invalid outside ingredient bypassed validation")
			}
			var status string
			var moves int
			if err := f.db.Pool.QueryRow(context.Background(), "SELECT status,(SELECT count(*) FROM pantry.stock_movements WHERE meal_id=$1) FROM mealplan.meal_entries WHERE id=$1", f.meal.ID).Scan(&status, &moves); err != nil {
				t.Fatal(err)
			}
			if status != "planned" || moves != 0 {
				t.Fatal("invalid outside line wrote cooking state")
			}
		})
	}
}

func TestCookQuantityBoundIsCheckedBeforeStockAllocation(t *testing.T) {
	f := cookContractSetup(t, "0.5", "0.5")
	_, err := f.s.Cook(context.Background(), f.user, f.sid, f.meal.ID, CookInput{Lines: []CookLine{{FoodID: f.first, UnitID: "00000000-0000-7000-8000-000000000011", Quantity: qty("10000000000")}}})
	var typed *platform.Error
	if !errors.As(err, &typed) || typed.Status != 400 || typed.Code != 2002 {
		t.Fatalf("quantity beyond publicbound reached stock allocation: %v", err)
	}
}

func TestCookConcurrentActorsReplayStoredActorAndIdenticalDeductions(t *testing.T) {
	f := cookContractSetup(t, "0.5", "0.5")
	ctx := context.Background()
	other := platform.ID()
	if _, err := f.db.Pool.Exec(ctx, "INSERT INTO identity.users(id,email,full_name,password_hash,status) VALUES($1,$2,'Second cook','unused','active')", other, other+"@example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool.Exec(ctx, "INSERT INTO household.space_members(space_id,user_id,role,space_kind) VALUES($1,$2,'member','family')", f.sid, other); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan CookResult, 2)
	failures := make(chan error, 2)
	var wg sync.WaitGroup
	for _, user := range []string{f.user, other} {
		wg.Go(func() {
			<-start
			r, err := f.s.Cook(ctx, user, f.sid, f.meal.ID, CookInput{Version: 1})
			results <- r
			failures <- err
		})
	}
	close(start)
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	all := []CookResult{}
	fresh, replays := 0, 0
	for r := range results {
		all = append(all, r)
		if r.AlreadyCooked {
			replays++
		} else {
			fresh++
		}
	}
	if fresh != 1 || replays != 1 || all[0].CookedBy == nil || all[1].CookedBy == nil || *all[0].CookedBy != *all[1].CookedBy || !all[0].CookedAt.Equal(*all[1].CookedAt) || !reflect.DeepEqual(all[0].Deductions, all[1].Deductions) || len(all[0].Deductions) != 2 {
		t.Fatalf("concurrent replay metadata diverged: %+v", all)
	}
	var count int
	if err := f.db.Pool.QueryRow(ctx, "SELECT count(*) FROM pantry.stock_movements WHERE meal_id=$1", f.meal.ID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("duplicate deductions count%d err%v", count, err)
	}
}

type stockWithoutCookHistory struct{ platform.StockPort }
type failingCookHistory struct{ platform.StockPort }

func (failingCookHistory) MealDeductions(context.Context, pgx.Tx, string, string) ([]platform.StockDeduction, error) {
	return nil, errors.New("PRIVATE-HISTORY-ERROR")
}

func TestCookHistoryFailureRollsBackAndSkipStockNeedsNoHistoryPort(t *testing.T) {
	for _, name := range []string{"missing", "failed"} {
		t.Run(name, func(t *testing.T) {
			f := cookContractSetup(t, "0.5", "0.5")
			if name == "missing" {
				f.s.stock = stockWithoutCookHistory{f.s.stock}
			} else {
				f.s.stock = failingCookHistory{f.s.stock}
			}
			if _, err := f.s.Cook(context.Background(), f.user, f.sid, f.meal.ID, CookInput{}); err == nil {
				t.Fatal("cook succeeded without canonical history")
			}
			var status, remaining string
			var moves int
			if err := f.db.Pool.QueryRow(context.Background(), "SELECT status,(SELECT remaining_qty::text FROM pantry.food_lots WHERE id=$2),(SELECT count(*) FROM pantry.stock_movements WHERE meal_id=$1) FROM mealplan.meal_entries WHERE id=$1", f.meal.ID, f.lots[0]).Scan(&status, &remaining, &moves); err != nil {
				t.Fatal(err)
			}
			if status != "planned" || remaining != "0.50" || moves != 0 {
				t.Fatalf("history failure partially committed %s/%s/%d", status, remaining, moves)
			}
			result, err := f.s.Cook(context.Background(), f.user, f.sid, f.meal.ID, CookInput{SkipStock: true})
			if err != nil || len(result.Deductions) != 0 || !result.SkippedStock {
				t.Fatalf("skipStock required history: %+v %v", result, err)
			}
		})
	}
}

func TestCookMealListCarriesStoredActorAndRejectsOutsider(t *testing.T) {
	f := cookContractSetup(t, "0.5", "0.5")
	ctx := context.Background()
	if _, err := f.s.Cook(ctx, f.user, f.sid, f.meal.ID, CookInput{SkipStock: true}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	f.s.Register(mux)
	path := "/it4788/v1/spaces/" + f.sid + "/meals?from=" + f.db.Today().Format("2006-01-02") + "&to=" + f.db.Today().Format("2006-01-02")
	status, body := planningRequest(t, mux, platform.Principal{UserID: f.user}, "GET", path, nil, "")
	if status != 200 {
		t.Fatalf("meal list HTTP%d %v", status, body)
	}
	items := body["data"].(map[string]any)["items"].([]any)
	first := items[0].(map[string]any)
	if first["cookedBy"] != f.user || first["cookedAt"] == nil || first["skippedStock"] != true {
		t.Fatalf("meal list lost actor fields %v", first)
	}
	_, err := f.s.Cook(ctx, platform.ID(), f.sid, f.meal.ID, CookInput{Version: 1})
	if err == nil {
		t.Fatal("outsider got replay metadata")
	}
}

func TestCookWaitingForAuthorityDoesNotLeakReplayAfterRemoval(t *testing.T) {
	f := cookContractSetup(t, "0.5", "0.5")
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if _, err := f.s.Cook(ctx, f.user, f.sid, f.meal.ID, CookInput{SkipStock: true}); err != nil {
		t.Fatal(err)
	}
	blocker, err := f.db.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(context.Background())
	if _, err = blocker.Exec(ctx, "SELECT id FROM identity.users WHERE id=$1 FOR UPDATE", f.user); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	result := make(chan CookResult, 1)
	go func() {
		r, err := f.s.Cook(ctx, f.user, f.sid, f.meal.ID, CookInput{Version: 1})
		result <- r
		done <- err
	}()
	testutil.WaitForDatabaseLock(t, ctx, f.db, "FROM identity.users", done)
	if _, err = blocker.Exec(ctx, "DELETE FROM household.space_members WHERE space_id=$1 AND user_id=$2", f.sid, f.user); err != nil {
		t.Fatal(err)
	}
	if err = blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		var denied *platform.Error
		if !errors.As(err, &denied) || denied.Status != 404 {
			t.Fatalf("removed actor replay result %v", err)
		}
		r := <-result
		if r.ID != "" || r.CookedBy != nil || len(r.Deductions) != 0 {
			t.Fatal("removed actor received cooking history")
		}
	case <-ctx.Done():
		t.Fatal("replay authority guard stalled")
	}
}

func TestCookOutsideIngredientRejectsAnyExplicitStockSelection(t *testing.T) {
	for _, name := range []string{"legacy-valid", "legacy-foreign", "legacy-malformed", "array-valid", "array-foreign", "array-malformed"} {
		t.Run(name, func(t *testing.T) {
			f := cookContractSetup(t, "0.5", "0.5")
			line := CookLine{FoodID: f.first, UnitID: f.unit}
			switch name {
			case "legacy-valid":
				line.LotID = f.lots[0]
			case "legacy-foreign":
				line.LotID = platform.ID()
			case "legacy-malformed":
				line.LotID = "malformed"
			case "array-valid":
				line.LotIDs = []string{f.lots[0]}
			case "array-foreign":
				line.LotIDs = []string{platform.ID()}
			case "array-malformed":
				line.LotIDs = []string{"malformed"}
			}
			_, err := f.s.Cook(context.Background(), f.user, f.sid, f.meal.ID, CookInput{Lines: []CookLine{line}})
			var invalid *platform.Error
			if !errors.As(err, &invalid) || invalid.Status != 400 {
				t.Fatalf("outside stock selection was accepted: %v", err)
			}
			var state string
			var count int
			if err = f.db.Pool.QueryRow(context.Background(), "SELECT status,(SELECT count(*) FROM pantry.stock_movements WHERE meal_id=$1) FROM mealplan.meal_entries WHERE id=$1", f.meal.ID).Scan(&state, &count); err != nil {
				t.Fatal(err)
			}
			if state != "planned" || count != 0 {
				t.Fatal("rejected outside selection changed cooking state")
			}
		})
	}
	f := cookContractSetup(t, "0.5", "0.5")
	result, err := f.s.Cook(context.Background(), f.user, f.sid, f.meal.ID, CookInput{Lines: []CookLine{{FoodID: f.first, UnitID: f.unit, LotIDs: []string{}}}})
	if err != nil || result.Status != "cooked" || len(result.Deductions) != 0 {
		t.Fatalf("empty selection outside line rejected: %+v %v", result, err)
	}
}

func TestCookRejectsRecipeDimensionMismatchAfterFoodDefaultChanges(t *testing.T) {
	for _, amount := range []string{"0", "100", "omitted"} {
		t.Run(amount, func(t *testing.T) {
			f := cookContractSetup(t, "0.5", "0.5")
			ctx := context.Background()
			admin := platform.ID()
			if _, err := f.db.Pool.Exec(ctx, "INSERT INTO identity.users(id,email,full_name,password_hash,status,system_role) VALUES($1,$2,'Admin','unused','active','admin')", admin, admin+"@example.test"); err != nil {
				t.Fatal(err)
			}
			mux := http.NewServeMux()
			accounts.New(f.db, nil).RegisterCatalog(mux)
			r := httptest.NewRequest("PUT", "/it4788/v1/admin/foods/"+f.first, strings.NewReader(`{"name":"Carrot reconfigured","categoryId":"00000000-0000-7000-8000-000000000001","defaultUnitId":"00000000-0000-7000-8000-000000000014","status":"active"}`)).WithContext(platform.WithPrincipal(ctx, platform.Principal{UserID: admin, SystemRole: "admin"}))
			r.Header.Set("If-Match", "1")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != 200 {
				t.Fatalf("admin food update HTTP%d %s", w.Code, w.Body.String())
			}
			input := CookInput{}
			if amount == "omitted" {
				inv := inventory.New(f.db, f.s.catalog)
				if _, err := inv.CreateLot(ctx, f.sid, f.user, inventory.LotInput{FoodID: f.first, UnitID: "00000000-0000-7000-8000-000000000014", Quantity: qty("100"), StorageLocation: "chill", PurchasedOn: f.db.Today().Format("2006-01-02"), ExpiresOn: f.db.Today().AddDate(0, 0, 1).Format("2006-01-02")}); err != nil {
					t.Fatal(err)
				}
			} else {
				input.Lines = []CookLine{{FoodID: f.first, UnitID: "00000000-0000-7000-8000-000000000014", Quantity: qty(amount), LotIDs: nil}, {FoodID: f.second, UnitID: f.unit, Quantity: 0}}
			}
			result, err := f.s.Cook(ctx, f.user, f.sid, f.meal.ID, input)
			if amount == "omitted" {
				if err != nil {
					t.Fatalf("available mass stock rejected after catalog dimension changed: %v", err)
				}
				if len(result.Deductions) != 2 {
					t.Fatalf("omitted lines lost recipe demands: %+v", result.Deductions)
				}
				for _, deduction := range result.Deductions {
					if deduction.FoodID == f.first && (deduction.Dimension != "mass" || deduction.LotID != f.lots[0] || deduction.Quantity != qty("100")) {
						t.Fatalf("omitted mass recipe consumed incompatible lot or quantity: %+v", result.Deductions)
					}
				}
				return
			}
			var invalid *platform.Error
			if !errors.As(err, &invalid) || invalid.Status != 400 {
				t.Fatalf("volume proof accepted or returned wrong error for mass recipe/lot: status%s deductions%+v error%v", result.Status, result.Deductions, err)
			}
			var movements int
			var status string
			if err := f.db.Pool.QueryRow(ctx, "SELECT status,(SELECT count(*) FROM pantry.stock_movements WHERE meal_id=$1) FROM mealplan.meal_entries WHERE id=$1", f.meal.ID).Scan(&status, &movements); err != nil {
				t.Fatal(err)
			}
			if status != "planned" || movements != 0 {
				t.Fatalf("rejected dimension proof changed meal or stock: %s/%d", status, movements)
			}
		})
	}
}

func TestCookExplicitLineRequiresValidIDsBeforeCatalogDefault(t *testing.T) {
	for _, tc := range []struct{ field, value string }{{"unit", ""}, {"unit", "not-a-uuid"}, {"food", ""}, {"food", "not-a-uuid"}} {
		t.Run(tc.field+"/"+tc.value, func(t *testing.T) {
			f := cookContractSetup(t, "0.5", "0.5")
			line := CookLine{FoodID: f.first, UnitID: f.unit, Quantity: 0}
			if tc.field == "unit" {
				line.UnitID = tc.value
			} else {
				line.FoodID = tc.value
			}
			_, err := f.s.Cook(context.Background(), f.user, f.sid, f.meal.ID, CookInput{Lines: []CookLine{line}})
			var invalid *platform.Error
			if !errors.As(err, &invalid) || invalid.Status != 400 {
				t.Fatalf("missing or malformed %s reached catalog fallback: %v", tc.field, err)
			}
		})
	}
}
