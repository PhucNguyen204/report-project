//go:build integration

package planning

import (
	"context"
	"dicho/internal/modules/inventory"
	"dicho/internal/platform"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func searchPage(t *testing.T, mux http.Handler, user, path string) (map[string]any, string) {
	t.Helper()
	status, body := planningRequest(t, mux, platform.Principal{UserID: user}, "GET", path, nil, "")
	if status != 200 {
		t.Fatalf("search%d %+v", status, body)
	}
	meta, ok := body["meta"].(map[string]any)
	if !ok {
		t.Fatalf("search missing metadata %+v", body)
	}
	return body["data"].(map[string]any), meta["nextCursor"].(string)
}
func TestSearchPagesCountAllSourcesBeyondOneHundredAndKeepGlobalTuple(t *testing.T) {
	s, db, user, sid, unit, _ := setupPlanning(t)
	ctx := context.Background()
	s.stock = inventory.New(db, s.catalog)
	mux := http.NewServeMux()
	s.Register(mux)
	for _, stmt := range []string{
		`INSERT INTO catalog.foods(id,name,category_id,default_unit_id) SELECT ('00000000-0000-7000-8000-'||lpad((1000+i)::text,12,'0'))::uuid,'Match'||lpad(i::text,3,'0'),'00000000-0000-7000-8000-000000000001',$1 FROM generate_series(0,104)i`,
	} {
		if _, err := db.Pool.Exec(ctx, stmt, unit); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO recipe.recipes(id,space_id,created_by,name,servings) SELECT ('00000000-0000-7000-8000-'||lpad((2000+i)::text,12,'0'))::uuid,$1,$2,'Match'||lpad(i::text,3,'0'),1 FROM generate_series(0,104)i`, sid, user); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO recipe.recipe_ingredients(id,recipe_id,food_id,unit_id,unit_factor,dimension,quantity) SELECT ('00000000-0000-7000-8000-'||lpad((4000+i)::text,12,'0'))::uuid,('00000000-0000-7000-8000-'||lpad((2000+i)::text,12,'0'))::uuid,('00000000-0000-7000-8000-'||lpad((1000+i)::text,12,'0'))::uuid,$1,1000,'mass',0.2 FROM generate_series(0,104)i`, unit); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO pantry.food_lots(id,space_id,food_id,unit_id,unit_factor,dimension,initial_qty,remaining_qty,base_remaining,purchased_on,expires_on,storage_location) SELECT ('00000000-0000-7000-8000-'||lpad((3000+i)::text,12,'0'))::uuid,$1,('00000000-0000-7000-8000-'||lpad((1000+i)::text,12,'0'))::uuid,$2,1000,'mass',1,1,1000,$3::date,$3::date+2,'chill' FROM generate_series(0,104)i`, sid, unit, db.Today()); err != nil {
		t.Fatal(err)
	}
	path := "/it4788/v1/spaces/" + sid + "/search?q=Match"
	status, body := planningRequest(t, mux, platform.Principal{UserID: user}, "GET", path, nil, "")
	if status != 200 {
		t.Fatal(body)
	}
	first := body["data"].(map[string]any)["items"].([]any)
	if len(first) != 20 {
		t.Fatalf("default search returned%d want20", len(first))
	}
	data, next := searchPage(t, mux, user, path)
	if next == "" {
		t.Fatal("search truncates remainder")
	}
	facets := data["facets"].(map[string]any)
	for _, kind := range []string{"food", "recipe", "lot"} {
		if facets[kind] != float64(105) {
			t.Fatalf("truncated %s facet %+v", kind, facets)
		}
	}
	filter := data["filterFacets"].(map[string]any)
	if filter["categoryId"].(map[string]any)["00000000-0000-7000-8000-000000000001"] != float64(210) {
		t.Fatalf("category count must include all food+lot hits %+v", filter)
	}
	seen := map[string]bool{}
	lastKey := ""
	for _, raw := range first {
		item := raw.(map[string]any)
		key := item["name"].(string) + "/" + item["type"].(string) + "/" + item["id"].(string)
		if lastKey > key {
			t.Fatal("wrong initial order")
		}
		lastKey = key
		seen[key] = true
	}
	if _, err := db.Pool.Exec(ctx, "INSERT INTO catalog.foods(id,name,category_id,default_unit_id) VALUES($1,'Match-Inserted','00000000-0000-7000-8000-000000000001',$2)", platform.ID(), unit); err != nil {
		t.Fatal(err)
	}
	for page := 0; next != "" && page < 100; page++ {
		data, cursor := searchPage(t, mux, user, path+"&limit=7&cursor="+url.QueryEscape(next))
		for _, raw := range data["items"].([]any) {
			item := raw.(map[string]any)
			key := item["name"].(string) + "/" + item["type"].(string) + "/" + item["id"].(string)
			if seen[key] || key < lastKey || item["name"] == "Match-Inserted" {
				t.Fatalf("duplicate/skip ordering %+v", item)
			}
			seen[key] = true
			lastKey = key
		}
		if data["facets"].(map[string]any)["food"] != float64(106) {
			t.Fatal("facets depend on cursor")
		}
		groups := data["filterFacets"].(map[string]any)
		if groups["categoryId"].(map[string]any)["00000000-0000-7000-8000-000000000001"] != float64(211) || groups["location"].(map[string]any)["chill"] != float64(105) || groups["expiryState"].(map[string]any)["expiring"] != float64(105) {
			t.Fatalf("filter facets depend on page %+v", groups)
		}
		next = cursor
	}
	if next != "" || len(seen) != 315 {
		t.Fatalf("lost mixed results %d next=%q", len(seen), next)
	}
	status, suggest := planningRequest(t, mux, platform.Principal{UserID: user}, "GET", "/it4788/v1/spaces/"+sid+"/foods/suggest?q=Match", nil, "")
	if status != 200 {
		t.Fatal(suggest)
	}
	result := suggest["data"].(map[string]any)
	if len(result["items"].([]any)) != 10 || result["facets"].(map[string]any)["food"] != float64(106) {
		t.Fatalf("autocomplete cap/count %+v", result)
	}
}

func TestSearchFiltersApplyORAndANDToPhysicalHitsAndViewerExpiry(t *testing.T) {
	s, db, user, _, unit, carrot := setupPlanning(t)
	ctx := context.Background()
	family := platform.ID()
	if _, err := db.Pool.Exec(ctx, "INSERT INTO household.spaces(id,kind,name,created_by) VALUES($1,'family','Search family',$2)", family, user); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, "INSERT INTO household.space_members(space_id,user_id,role,space_kind) VALUES($1,$2,'leader','family')", family, user); err != nil {
		t.Fatal(err)
	}
	sid := family
	inv := inventory.New(db, s.catalog)
	s.stock = inv
	other := addPlanningUser(t, db, sid, "")
	if _, err := db.Pool.Exec(ctx, "INSERT INTO household.space_members(space_id,user_id,role,space_kind) VALUES($1,$2,'member','family')", sid, other); err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct {
		user string
		n    int
	}{{user, 1}, {other, 3}} {
		if _, err := db.Pool.Exec(ctx, "INSERT INTO identity.user_settings(user_id,reminder_days) VALUES($1,$2)", v.user, v.n); err != nil {
			t.Fatal(err)
		}
	}
	bean := platform.ID()
	if _, err := db.Pool.Exec(ctx, "INSERT INTO catalog.foods(id,name,category_id,scope,owner_space_id,status,default_unit_id) VALUES($1,'Đậu %_tươi','00000000-0000-7000-8000-000000000002','space',$2,'pending_review',$3)", bean, sid, unit); err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct {
		food, location string
		days           int
	}{{carrot, "chill", 2}, {carrot, "freezer", 6}, {bean, "crisper", -1}} {
		if _, err := db.Pool.Exec(ctx, "INSERT INTO pantry.food_lots(id,space_id,food_id,unit_id,unit_factor,dimension,initial_qty,remaining_qty,base_remaining,purchased_on,expires_on,storage_location) VALUES($1,$2,$3,$4,1000,'mass',1,1,1000,$5::date-3,$5::date+$6::int,$7)", platform.ID(), sid, v.food, unit, db.Today(), v.days, v.location); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.CreateRecipe(ctx, user, sid, RecipeInput{Name: "Đậu hầm", Servings: 1, Ingredients: []IngredientInput{{FoodID: bean, UnitID: unit, Quantity: qty("0.2")}}}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.Register(mux)
	path := "/it4788/v1/spaces/" + sid + "/search"
	q := "?location=chill,crisper&expiryState=expired&expiryState=expiring"
	a, _ := searchPage(t, mux, user, path+q)
	b, _ := searchPage(t, mux, other, path+q)
	if len(a["items"].([]any)) != 1 || len(b["items"].([]any)) != 2 {
		t.Fatalf("viewer N ignored A%+v B%+v", a, b)
	}
	c, _ := searchPage(t, mux, other, path+q+"&categoryId=00000000-0000-7000-8000-000000000001")
	if len(c["items"].([]any)) != 1 || c["items"].([]any)[0].(map[string]any)["foodId"] != carrot {
		t.Fatalf("AND filter %+v", c)
	}
	groups := c["filterFacets"].(map[string]any)
	if groups["categoryId"].(map[string]any)["00000000-0000-7000-8000-000000000001"] != float64(1) || groups["location"].(map[string]any)["chill"] != float64(1) || groups["expiryState"].(map[string]any)["expiring"] != float64(1) {
		t.Fatalf("facets ignore full AND filter %+v", groups)
	}
	category, _ := searchPage(t, mux, user, path+"?categoryId=00000000-0000-7000-8000-000000000001")
	if len(category["items"].([]any)) != 3 || category["facets"].(map[string]any)["recipe"] != float64(0) {
		t.Fatalf("category must include food+lots, not infer recipes %+v", category)
	}
	for _, literal := range []string{"%", "_"} {
		data, _ := searchPage(t, mux, user, path+"?q="+url.QueryEscape(literal))
		if len(data["items"].([]any)) != 2 {
			t.Fatalf("literal %q became wildcard %+v", literal, data)
		}
	}
	folded, _ := searchPage(t, mux, user, path+"?q=dau")
	if len(folded["items"].([]any)) != 3 {
		t.Fatalf("Vietnamese folding %+v", folded)
	}
	noMatch, _ := searchPage(t, mux, user, path+"?categoryId="+platform.ID())
	if len(noMatch["items"].([]any)) != 0 {
		t.Fatal("nonexistent category matched")
	}
}

func TestSearchRejectsBadCursorsFiltersAndAutocompletePagination(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	s.stock = inventory.New(db, s.catalog)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, err := s.CreateRecipe(ctx, user, sid, RecipeInput{Name: fmt.Sprintf("Canh%d", i), Servings: 1, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("1")}}}); err != nil {
			t.Fatal(err)
		}
	}
	mux := http.NewServeMux()
	s.Register(mux)
	p := platform.Principal{UserID: user}
	path := "/it4788/v1/spaces/" + sid + "/search"
	_, cursor := searchPage(t, mux, user, path+"?limit=1")
	raw, _ := base64.RawURLEncoding.DecodeString(cursor)
	var c map[string]any
	_ = json.Unmarshal(raw, &c)
	keys := c["after"].([]any)
	keys[1] = "unknown"
	raw, _ = json.Marshal(c)
	badKind := base64.RawURLEncoding.EncodeToString(raw)
	for _, query := range []string{"limit=0", "limit=101", "limit=1&limit=2", "cursor=bad", "categoryId=bad", "location=garage", "expiryState=unknown", "q=a&q=b", "q=" + strings.Repeat("a", 101), "q=changed&cursor=" + url.QueryEscape(cursor), "cursor=" + url.QueryEscape(badKind)} {
		requirePlanningPageError(t, mux, p, path+"?"+query, 400)
	}
	requirePlanningPageError(t, mux, platform.Principal{UserID: platform.ID()}, path, 404)
	requirePlanningPageError(t, mux, platform.Principal{UserID: platform.ID()}, path+"?cursor="+url.QueryEscape(cursor), 400)
	for _, query := range []string{"q=a", "q=carrot&limit=11", "q=carrot&limit=0", "q=carrot&cursor=" + url.QueryEscape(cursor), "q=carrot&location=chill"} {
		requirePlanningPageError(t, mux, p, "/it4788/v1/spaces/"+sid+"/foods/suggest?"+query, 400)
	}
	if _, err := db.Pool.Exec(ctx, "DELETE FROM household.space_members WHERE space_id=$1 AND user_id=$2", sid, user); err != nil {
		t.Fatal(err)
	}
	requirePlanningPageError(t, mux, p, path+"?cursor="+url.QueryEscape(cursor), 404)
}

func TestSearchPagesExcludeForeignPrivateDataAndUnpublishedRecipes(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	s.stock = inventory.New(db, s.catalog)
	foreign := addPlanningUser(t, db, sid, "")
	foreignSID := platform.ID()
	if _, err := db.Pool.Exec(ctx, "INSERT INTO household.spaces(id,kind,name,created_by) VALUES($1,'personal','Foreign space',$2)", foreignSID, foreign); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, "INSERT INTO household.space_members(space_id,user_id,role,space_kind) VALUES($1,$2,'leader','personal')", foreignSID, foreign); err != nil {
		t.Fatal(err)
	}
	ownFood, foreignFood := platform.ID(), platform.ID()
	for _, v := range []struct{ id, sid, name string }{{ownFood, sid, "Scoped own food"}, {foreignFood, foreignSID, "Scoped foreign food"}} {
		if _, err := db.Pool.Exec(ctx, "INSERT INTO catalog.foods(id,name,category_id,scope,owner_space_id,status,default_unit_id) VALUES($1,$2,'00000000-0000-7000-8000-000000000001','space',$3,'pending_review',$4)", v.id, v.name, v.sid, unit); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Pool.Exec(ctx, "UPDATE catalog.foods SET name='Scoped global food' WHERE id=$1", food); err != nil {
		t.Fatal(err)
	}
	ownRecipe, err := s.CreateRecipe(ctx, user, sid, RecipeInput{Name: "Scoped own recipe", Servings: 1, Ingredients: []IngredientInput{{FoodID: ownFood, UnitID: unit, Quantity: qty("0.2")}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateRecipe(ctx, foreign, foreignSID, RecipeInput{Name: "Scoped foreign recipe", Servings: 1, Ingredients: []IngredientInput{{FoodID: foreignFood, UnitID: unit, Quantity: qty("0.2")}}}); err != nil {
		t.Fatal(err)
	}
	admin := planningPageAdmin(t, db)
	var published string
	for _, state := range []string{"draft", "published", "hidden"} {
		r, err := s.SaveTemplate(ctx, admin, "", 0, TemplateInput{RecipeInput: RecipeInput{Name: "Scoped " + state, Servings: 1, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("1")}}}, Status: state})
		if err != nil {
			t.Fatal(err)
		}
		if state == "published" {
			published = r.ID
		}
	}
	lot := platform.ID()
	for _, v := range []struct{ id, sid, food string }{{lot, sid, food}, {platform.ID(), foreignSID, foreignFood}} {
		if _, err := db.Pool.Exec(ctx, "INSERT INTO pantry.food_lots(id,space_id,food_id,unit_id,unit_factor,dimension,initial_qty,remaining_qty,base_remaining,purchased_on,expires_on,storage_location) VALUES($1,$2,$3,$4,1000,'mass',1,1,1000,$5::date,$5::date+3,'chill')", v.id, v.sid, v.food, unit, db.Today()); err != nil {
			t.Fatal(err)
		}
	}
	mux := http.NewServeMux()
	s.Register(mux)
	path := "/it4788/v1/spaces/" + sid + "/search?q=Scoped&limit=1"
	expected := map[string]bool{"food:" + food: true, "food:" + ownFood: true, "recipe:" + ownRecipe.ID: true, "recipe:" + published: true, "lot:" + lot: true}
	seen := map[string]bool{}
	cursor := ""
	for i := 0; i < 10; i++ {
		query := path
		if cursor != "" {
			query += "&cursor=" + url.QueryEscape(cursor)
		}
		page, next := searchPage(t, mux, user, query)
		for _, raw := range page["items"].([]any) {
			item := raw.(map[string]any)
			key := item["type"].(string) + ":" + item["id"].(string)
			if !expected[key] || seen[key] {
				t.Fatalf("private/unpublished/duplicate result %+v", item)
			}
			seen[key] = true
		}
		facets := page["facets"].(map[string]any)
		if facets["food"] != float64(2) || facets["recipe"] != float64(2) || facets["lot"] != float64(1) {
			t.Fatalf("private data leaked through counts %+v", facets)
		}
		if next == "" {
			break
		}
		cursor = next
	}
	if len(seen) != 5 {
		t.Fatalf("missing authorized results %+v", seen)
	}
}

func TestSearchCategoryUUIDFilterAcceptsUppercase(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	s.stock = inventory.New(db, s.catalog)
	ctx := context.Background()
	category := "aaaaaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa"
	if _, err := db.Pool.Exec(ctx, "INSERT INTO catalog.food_categories(id,name) VALUES($1,'Search category')", category); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, "UPDATE catalog.foods SET category_id=$1 WHERE id=$2", category, food); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, "INSERT INTO pantry.food_lots(id,space_id,food_id,unit_id,unit_factor,dimension,initial_qty,remaining_qty,base_remaining,purchased_on,expires_on,storage_location) VALUES($1,$2,$3,$4,1000,'mass',1,1,1000,$5::date,$5::date+3,'chill')", platform.ID(), sid, food, unit, db.Today()); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.Register(mux)
	data, _ := searchPage(t, mux, user, "/it4788/v1/spaces/"+sid+"/search?categoryId="+strings.ToUpper(category))
	if len(data["items"].([]any)) != 2 {
		t.Fatalf("uppercase UUID lost food/lot hits %+v", data)
	}
}
