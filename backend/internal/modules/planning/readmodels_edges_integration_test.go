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

func TestTemplatesRejectStalePrivateIngredientsAndReferencedDeletion(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	admin := addPlanningUser(t, db, sid, "")
	if _, err := db.Pool.Exec(ctx, "UPDATE identity.users SET system_role='admin' WHERE id=$1", admin); err != nil {
		t.Fatal(err)
	}
	in := TemplateInput{RecipeInput: RecipeInput{Name: "Món mẫu", Servings: 2, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("0.2")}}}}
	draft, err := s.SaveTemplate(ctx, admin, "", 0, in)
	if err != nil || draft.Status != "draft" {
		t.Fatalf("%+v %v", draft, err)
	}
	if _, err = s.GetRecipe(ctx, user, sid, draft.ID, 0); err == nil {
		t.Fatal("draft visible to user")
	}
	if _, err = s.CopyRecipe(ctx, user, sid, draft.ID, ""); err == nil {
		t.Fatal("draft copied")
	}
	if _, err = s.SaveTemplate(ctx, admin, draft.ID, 0, in); err == nil {
		t.Fatal("missing version accepted")
	}
	if _, err = s.SaveTemplate(ctx, admin, draft.ID, draft.Version+1, in); err == nil {
		t.Fatal("stale update accepted")
	}
	invalid := in
	invalid.Status = "invalid"
	if _, err = s.SaveTemplate(ctx, admin, "", 0, invalid); err == nil {
		t.Fatal("invalid status accepted")
	}
	invalid.RecipeInput.Name = "a"
	if _, err = s.SaveTemplate(ctx, admin, "", 0, invalid); err == nil {
		t.Fatal("invalid recipe accepted")
	}
	private := platform.ID()
	if _, err = db.Pool.Exec(ctx, "INSERT INTO catalog.foods(id,name,category_id,scope,owner_space_id,status,default_unit_id) SELECT $1,'Rau riêng',category_id,'space',$2,'pending_review',$3 FROM catalog.foods WHERE id=$4", private, sid, unit, food); err != nil {
		t.Fatal(err)
	}
	invalid = in
	invalid.Ingredients = []IngredientInput{{FoodID: private, UnitID: unit, Quantity: qty("1")}}
	if _, err = s.SaveTemplate(ctx, admin, "", 0, invalid); err == nil {
		t.Fatal("private food leaked into global template")
	}
	var n int
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM recipe.recipes WHERE kind='template'").Scan(&n); err != nil || n != 1 {
		t.Fatalf("partial template %d %v", n, err)
	}
	in.Status = "published"
	published, err := s.SaveTemplate(ctx, admin, draft.ID, 1, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CopyRecipe(ctx, user, sid, published.ID, "a"); err == nil {
		t.Fatal("invalid copy name accepted")
	}
	copy, err := s.CopyRecipe(ctx, user, sid, published.ID, "Món mẫu riêng")
	if err != nil || copy.Name != "Món mẫu riêng" || len(copy.Ingredients) != 1 {
		t.Fatalf("%+v %v", copy, err)
	}
	if err = s.DeleteTemplate(ctx, admin, published.ID); err == nil {
		t.Fatal("referenced template deleted")
	}
	in.Status = "hidden"
	hidden, err := s.SaveTemplate(ctx, admin, published.ID, published.Version, in)
	if err != nil || hidden.Status != "hidden" {
		t.Fatalf("%+v %v", hidden, err)
	}
	if _, err = s.GetRecipe(ctx, user, sid, published.ID, 0); err == nil {
		t.Fatal("hidden template visible")
	}
	if err = s.DeleteTemplate(ctx, user, published.ID); err == nil {
		t.Fatal("nonadmin deleted template")
	}
	if err = s.DeleteTemplate(ctx, admin, platform.ID()); err == nil {
		t.Fatal("missing template deleted")
	}
}

func TestSuggestFallbackOrderingFavoriteAndFiltersAreUserScoped(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	inv := inventory.New(db, s.catalog)
	s.stock = inv
	ids := []string{}
	for _, name := range []string{"Bữa D", "Bữa C", "Bữa B", "Bữa A"} {
		r, err := s.CreateRecipe(ctx, user, sid, RecipeInput{Name: name, Servings: 1, MealSlot: "lunch", Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("1")}}})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, r.ID)
	}
	if err := db.Tx(ctx, func(tx pgx.Tx) error {
		_, err := inv.AddLots(ctx, tx, sid, user, []platform.StockInput{{FoodID: food, UnitID: unit, Quantity: qty("0.1"), PurchasedOn: db.Today(), ExpiresOn: db.Today().AddDate(0, 0, 7), Location: "chill"}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetFavorite(ctx, user, ids[0], true); err != nil {
		t.Fatal(err)
	}
	got, err := s.Suggest(ctx, user, sid, SuggestionInput{MealSlot: "lunch"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Meta.Fallback != "below_threshold" || len(got.Items) != 3 || got.Items[0].Recipe.Name != "Bữa D" || got.Items[1].Recipe.Name != "Bữa A" || got.Items[0].F != 1 {
		t.Fatalf("fallback %+v", got)
	}
	other := addPlanningUser(t, db, sid, "member")
	different, err := s.Suggest(ctx, other, sid, SuggestionInput{Servings: 1})
	if err != nil {
		t.Fatal(err)
	}
	if different.Items[0].Recipe.Name != "Bữa A" || different.Items[0].F != 0 {
		t.Fatalf("favorite leaked %+v", different)
	}
	filtered, err := s.Suggest(ctx, user, sid, SuggestionInput{MealSlot: "dinner", ExpiringOnly: true})
	if err != nil || len(filtered.Items) != 0 {
		t.Fatalf("slot filter %+v %v", filtered, err)
	}
	filtered, err = s.Suggest(ctx, user, sid, SuggestionInput{ExpiringOnly: true})
	if err != nil || len(filtered.Items) != 0 {
		t.Fatalf("expiry filter %+v %v", filtered, err)
	}
	for name, in := range map[string]SuggestionInput{"date": {Date: "bad"}, "horizon": {Date: db.Today().AddDate(0, 0, 61).Format("2006-01-02")}, "slot": {MealSlot: "snack"}, "limit": {Limit: 11}, "negative": {Limit: -1}} {
		t.Run(name, func(t *testing.T) {
			if _, err := s.Suggest(ctx, user, sid, in); err == nil {
				t.Fatal("invalid suggestion accepted")
			}
		})
	}
	s.stock = nil
	if _, err = s.Suggest(ctx, user, sid, SuggestionInput{}); err == nil {
		t.Fatal("unconfigured stock accepted")
	}
}

func TestReportsSpendingAndConsumptionAggregatePricedAndUnpricedSources(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	inv := inventory.New(db, s.catalog)
	list, err := inv.CreateList(ctx, sid, user, inventory.ListInput{Name: "Đi chợ", PlannedDate: db.Today().Format("2006-01-02")})
	if err != nil {
		t.Fatal(err)
	}
	a, err := inv.AddItem(ctx, sid, user, list.ID, inventory.ItemInput{FoodID: food, UnitID: unit, Quantity: qty("0.3")})
	if err != nil {
		t.Fatal(err)
	}
	b, err := inv.AddItem(ctx, sid, user, list.ID, inventory.ItemInput{Name: "Món chưa có giá", UnitID: unit, Quantity: qty("0.2")})
	if err != nil {
		t.Fatal(err)
	}
	price := int64(12000)
	if _, err = inv.Purchase(ctx, sid, user, []inventory.PurchaseInput{{ItemID: a.ID, UnitPrice: &price}, {ItemID: b.ID}}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Report(ctx, user, sid, "spending", db.Today(), db.Today(), "month")
	if err != nil {
		t.Fatal(err)
	}
	if got.TotalCost != 3600 || got.UnpricedLots != 1 || got.Lots != 2 || got.Quantities["mass"] != qty("500") || len(got.Categories) != 2 || got.Previous.TotalCost != 0 || got.Change.TotalCost != 3600 {
		t.Fatalf("spending %+v", got)
	}
	s.stock = inv
	r, err := s.CreateRecipe(ctx, user, sid, RecipeInput{Name: "Canh", Servings: 1, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("0.2")}}})
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.CreateMeal(ctx, user, sid, MealInput{RecipeID: r.ID, Date: db.Today().Format("2006-01-02"), MealSlot: "lunch", Servings: 1})
	if err != nil {
		t.Fatal(err)
	}
	cost := int64(10000)
	if err = db.Tx(ctx, func(tx pgx.Tx) error {
		_, err := inv.AddLots(ctx, tx, sid, user, []platform.StockInput{{FoodID: food, UnitID: unit, Quantity: qty("1"), PurchasedOn: db.Today(), ExpiresOn: db.Today().AddDate(0, 0, 3), Location: "chill", TotalCost: &cost}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Cook(ctx, user, sid, m.ID, CookInput{}); err != nil {
		t.Fatal(err)
	}
	consumed, err := s.Report(ctx, user, sid, "consumption", db.Today(), db.Today(), "week")
	if err != nil {
		t.Fatal(err)
	}
	if consumed.CookedMeals != 1 || consumed.TotalCost != 2000 || consumed.Quantities["mass"] != qty("200") || consumed.Lots != 1 {
		t.Fatalf("consumption %+v", consumed)
	}
	for _, v := range []struct{ kind, group string }{{"unknown", "day"}, {"waste", "year"}} {
		if _, err = s.Report(ctx, user, sid, v.kind, db.Today(), db.Today(), v.group); err == nil {
			t.Fatal("invalid report accepted")
		}
	}
	if _, err = s.Report(ctx, user, sid, "waste", db.Today(), db.Today().AddDate(0, 0, -1), "day"); err == nil {
		t.Fatal("reversed report accepted")
	}
}

func TestSearchRejectsUnsafeBoundsAndReturnsScopedStockFacets(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	inv := inventory.New(db, s.catalog)
	s.stock = inv
	if err := db.Tx(ctx, func(tx pgx.Tx) error {
		_, err := inv.AddLots(ctx, tx, sid, user, []platform.StockInput{{FoodID: food, UnitID: unit, Quantity: qty("1"), PurchasedOn: db.Today(), ExpiresOn: db.Today().AddDate(0, 0, 3), Location: "chill"}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Search(ctx, user, sid, "ca rot", 10, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Facets["lot"] != 1 || got.Facets["food"] != 1 {
		t.Fatalf("facets %+v", got)
	}
	for _, v := range []struct {
		q     string
		limit int
		foods bool
	}{{"ca", 0, false}, {strings.Repeat("a", 101), 10, false}, {"c", 10, true}, {"ca", 11, true}} {
		if _, err = s.Search(ctx, user, sid, v.q, v.limit, v.foods); err == nil {
			t.Fatal("invalid search bounds accepted")
		}
	}
	injection, err := s.Search(ctx, user, sid, "' OR true --", 10, false)
	if err != nil || len(injection.Items) != 0 {
		t.Fatalf("SQL injection query %+v %v", injection, err)
	}
}
