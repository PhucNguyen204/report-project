//go:build integration

package planning

import (
	"context"
	"dicho/internal/platform"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"testing"
	"time"
)

func planningPage(t *testing.T, mux http.Handler, p platform.Principal, path string) ([]map[string]any, string) {
	t.Helper()
	status, body := planningRequest(t, mux, p, "GET", path, nil, "")
	if status != 200 {
		t.Fatalf("page %s status%d %+v", path, status, body)
	}
	raw, ok := body["data"].(map[string]any)["items"].([]any)
	if !ok {
		t.Fatalf("missing data.items %+v", body)
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		out = append(out, item.(map[string]any))
	}
	meta, ok := body["meta"].(map[string]any)
	if !ok {
		t.Fatalf("missing pagination metadata %+v", body)
	}
	next, ok := meta["nextCursor"].(string)
	if !ok {
		t.Fatalf("missing nextCursor %+v", body)
	}
	if at, err := time.Parse(time.RFC3339Nano, fmt.Sprint(meta["serverTime"])); err != nil || at.IsZero() {
		t.Fatalf("invalid serverTime %+v", meta)
	}
	return out, next
}
func planningPageID(n int) string { return fmt.Sprintf("00000000-0000-7000-8000-%012d", n) }
func planningPageAdmin(t *testing.T, db *platform.DB) string {
	t.Helper()
	id := platform.ID()
	if _, err := db.Pool.Exec(context.Background(), "INSERT INTO identity.users(id,email,full_name,password_hash,status,system_role) VALUES($1,$2,'Admin pagination','hash','active','admin')", id, id+"@pagination.test"); err != nil {
		t.Fatal(err)
	}
	return id
}
func changedPlanningCursor(t *testing.T, encoded string, after []string) string {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	var c map[string]any
	if err = json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	c["after"] = after
	raw, err = json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}
func requirePlanningPageError(t *testing.T, mux http.Handler, p platform.Principal, path string, status int) {
	t.Helper()
	got, body := planningRequest(t, mux, p, "GET", path, nil, "")
	if got != status {
		t.Fatalf("%s status%d want%d %+v", path, got, status, body)
	}
}

func TestRecipePaginationDefaultsToTwentyAndKeepsNameIDTiesAfterInsert(t *testing.T) {
	s, _, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	mux := http.NewServeMux()
	s.Register(mux)
	p := platform.Principal{UserID: user}
	path := "/it4788/v1/spaces/" + sid + "/recipes"
	for i := 0; i < 21; i++ {
		if _, err := s.CreateRecipe(ctx, user, sid, RecipeInput{ID: planningPageID(100 + i), Name: "Món trùng tên", Servings: 1, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("0.2")}}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.CreateRecipe(ctx, user, sid, RecipeInput{ID: planningPageID(1), Name: "Z Món cuối", Servings: 1, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("0.2")}}}); err != nil {
		t.Fatal(err)
	}
	// The first twenty IDs tie on name; a new earlier ID must not repeat the page.
	status, body := planningRequest(t, mux, p, "GET", path, nil, "")
	if status != 200 {
		t.Fatal(body)
	}
	raw := body["data"].(map[string]any)["items"].([]any)
	if len(raw) != 20 {
		t.Fatalf("default returned%d recipes, want20", len(raw))
	}
	items, next := planningPage(t, mux, p, path)
	if next == "" {
		t.Fatal("last recipe became unreachable")
	}
	for i, item := range items {
		if item["id"] != planningPageID(100+i) {
			t.Fatalf("name/id ordering %+v", items)
		}
	}
	if _, err := s.CreateRecipe(ctx, user, sid, RecipeInput{ID: planningPageID(50), Name: "Món trùng tên", Servings: 1, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("0.2")}}}); err != nil {
		t.Fatal(err)
	}
	rest, last := planningPage(t, mux, p, path+"?limit=7&cursor="+url.QueryEscape(next))
	if len(rest) != 2 || rest[0]["id"] != planningPageID(120) || rest[1]["id"] != planningPageID(1) || last != "" {
		t.Fatalf("continuation %+v cursor%q", rest, last)
	}
	all, last := planningPage(t, mux, p, path+"?limit=100")
	if len(all) != 23 || last != "" {
		t.Fatalf("max page count%d cursor%q", len(all), last)
	}
	for _, query := range []string{"limit=0", "limit=101", "limit=bad", "limit=1&limit=2", "cursor=bad", "q=changed&cursor=" + url.QueryEscape(next), "cursor=" + url.QueryEscape(changedPlanningCursor(t, next, []string{"Món trùng tên", "not-a-uuid"}))} {
		requirePlanningPageError(t, mux, p, path+"?"+query, 400)
	}
	requirePlanningPageError(t, mux, platform.Principal{UserID: platform.ID()}, path, 404)
	requirePlanningPageError(t, mux, platform.Principal{UserID: platform.ID()}, path+"?cursor="+url.QueryEscape(next), 400)
}

func TestRecipePaginationPreservesFavoriteSourceQueryAndTenantScope(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	admin := planningPageAdmin(t, db)
	mux := http.NewServeMux()
	s.Register(mux)
	p := platform.Principal{UserID: user}
	path := "/it4788/v1/spaces/" + sid + "/recipes"
	own := []string{}
	for _, name := range []string{"Canh cà rốt A", "Canh cà rốt B"} {
		r, err := s.CreateRecipe(ctx, user, sid, RecipeInput{Name: name, Servings: 1, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("0.2")}}})
		if err != nil {
			t.Fatal(err)
		}
		own = append(own, r.ID)
	}
	templates := []string{}
	for _, status := range []string{"published", "published", "hidden"} {
		r, err := s.SaveTemplate(ctx, admin, "", 0, TemplateInput{RecipeInput: RecipeInput{Name: "Canh cà rốt mẫu", Servings: 1, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("0.2")}}}, Status: status})
		if err != nil {
			t.Fatal(err)
		}
		if status == "published" {
			templates = append(templates, r.ID)
		}
	}
	other := platform.ID()
	if _, err := db.Pool.Exec(ctx, "INSERT INTO household.spaces(id,kind,name,created_by) VALUES($1,'personal','Foreign pantry',$2)", other, admin); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, "INSERT INTO household.space_members(space_id,user_id,role,space_kind) VALUES($1,$2,'leader','personal')", other, admin); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateRecipe(ctx, admin, other, RecipeInput{Name: "Canh cà rốt bí mật", Servings: 1, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("0.2")}}}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{own[0], templates[0]} {
		if err := s.SetFavorite(ctx, user, id, true); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range []struct {
		query string
		want  []string
	}{{"source=space&q=ca%20rot", own}, {"source=template", templates}, {"source=favorite", []string{own[0], templates[0]}}} {
		seen := map[string]bool{}
		cursor := ""
		for page := 0; page < 5; page++ {
			q := v.query + "&limit=1"
			if cursor != "" {
				q += "&cursor=" + url.QueryEscape(cursor)
			}
			items, next := planningPage(t, mux, p, path+"?"+q)
			for _, item := range items {
				id := item["id"].(string)
				if seen[id] {
					t.Fatalf("duplicate filtered recipe %s", id)
				}
				seen[id] = true
			}
			if next == "" {
				break
			}
			cursor = next
		}
		if len(seen) != len(v.want) {
			t.Fatalf("filter%s returned%v want%v", v.query, seen, v.want)
		}
		for _, id := range v.want {
			if !seen[id] {
				t.Fatalf("missing filtered recipe%s", id)
			}
		}
	}
	_, next := planningPage(t, mux, p, path+"?source=favorite&limit=1")
	requirePlanningPageError(t, mux, p, path+"?source=space&limit=1&cursor="+url.QueryEscape(next), 400)
}

func TestMealPaginationUsesDateSlotIDAndKeepsDateFilter(t *testing.T) {
	s, db, user, sid, _, _ := setupPlanning(t)
	ctx := context.Background()
	mux := http.NewServeMux()
	s.Register(mux)
	p := platform.Principal{UserID: user}
	path := "/it4788/v1/spaces/" + sid + "/meals"
	today := db.Today().Format("2006-01-02")
	tomorrow := db.Today().AddDate(0, 0, 1).Format("2006-01-02")
	for i := 0; i < 21; i++ {
		if _, err := s.CreateMeal(ctx, user, sid, MealInput{ID: planningPageID(300 + i), Name: "Bữa trùng", Date: today, MealSlot: "lunch", Servings: 1}); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range []struct{ id, date, slot string }{{planningPageID(350), today, "dinner"}, {planningPageID(400), tomorrow, "breakfast"}, {planningPageID(450), db.Today().AddDate(0, 0, 10).Format("2006-01-02"), "lunch"}} {
		if _, err := s.CreateMeal(ctx, user, sid, MealInput{ID: v.id, Name: "Bữa khác", Date: v.date, MealSlot: v.slot, Servings: 1}); err != nil {
			t.Fatal(err)
		}
	}
	status, body := planningRequest(t, mux, p, "GET", path, nil, "")
	if status != 200 {
		t.Fatal(body)
	}
	raw := body["data"].(map[string]any)["items"].([]any)
	if len(raw) != 20 {
		t.Fatalf("default returned%d meals, want20", len(raw))
	}
	first, next := planningPage(t, mux, p, path)
	if next == "" || first[0]["id"] != planningPageID(350) || first[19]["id"] != planningPageID(318) {
		t.Fatalf("first date/slot page %+v cursor%q", first, next)
	}
	if _, err := s.CreateMeal(ctx, user, sid, MealInput{ID: planningPageID(99), Name: "Bữa chèn trước", Date: today, MealSlot: "breakfast", Servings: 1}); err != nil {
		t.Fatal(err)
	}
	rest, last := planningPage(t, mux, p, path+"?limit=7&cursor="+url.QueryEscape(next))
	want := []string{planningPageID(319), planningPageID(320), planningPageID(400)}
	if len(rest) != len(want) || last != "" {
		t.Fatalf("remaining %+v cursor%q", rest, last)
	}
	for i, item := range rest {
		if item["id"] != want[i] {
			t.Fatalf("date/slot/id order %+v", rest)
		}
	}
	query := "from=" + today + "&to=" + today
	seen := map[string]bool{}
	cursor := ""
	for page := 0; page < 10; page++ {
		q := query + "&limit=4"
		if cursor != "" {
			q += "&cursor=" + url.QueryEscape(cursor)
		}
		items, after := planningPage(t, mux, p, path+"?"+q)
		for _, item := range items {
			id := item["id"].(string)
			if item["date"] != today || seen[id] {
				t.Fatalf("date filter/duplicate %+v", item)
			}
			seen[id] = true
		}
		if after == "" {
			break
		}
		cursor = after
	}
	if len(seen) != 23 {
		t.Fatalf("filtered count%d want23", len(seen))
	}
	for _, q := range []string{"limit=101", "cursor=bad", "from=" + today + "&to=" + today + "&cursor=" + url.QueryEscape(next), "cursor=" + url.QueryEscape(changedPlanningCursor(t, next, []string{"2026-02-30", "lunch", planningPageID(318)}))} {
		requirePlanningPageError(t, mux, p, path+"?"+q, 400)
	}
	requirePlanningPageError(t, mux, platform.Principal{UserID: platform.ID()}, path, 404)
}

func TestAdminTemplatePaginationUsesNameIDAndRechecksRole(t *testing.T) {
	s, db, user, _, unit, food := setupPlanning(t)
	ctx := context.Background()
	admin := planningPageAdmin(t, db)
	p := platform.Principal{UserID: admin, SystemRole: "admin"}
	mux := http.NewServeMux()
	s.Register(mux)
	path := "/it4788/v1/admin/recipe-templates"
	ids := []string{}
	for i := 0; i < 21; i++ {
		state := []string{"draft", "published", "hidden"}[i%3]
		r, err := s.SaveTemplate(ctx, admin, "", 0, TemplateInput{RecipeInput: RecipeInput{Name: "Mẫu trùng tên", Servings: 1, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("0.2")}}}, Status: state})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, r.ID)
	}
	sort.Strings(ids)
	status, body := planningRequest(t, mux, p, "GET", path, nil, "")
	if status != 200 {
		t.Fatal(body)
	}
	raw := body["data"].(map[string]any)["items"].([]any)
	if len(raw) != 20 {
		t.Fatalf("default returned%d templates, want20", len(raw))
	}
	first, next := planningPage(t, mux, p, path)
	if next == "" {
		t.Fatal("template after first page unreachable")
	}
	for i, item := range first {
		if item["id"] != ids[i] {
			t.Fatalf("template tie order %+v", first)
		}
	}
	if _, err := s.SaveTemplate(ctx, admin, "", 0, TemplateInput{RecipeInput: RecipeInput{Name: "A mẫu chèn trước", Servings: 1, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("0.2")}}}}); err != nil {
		t.Fatal(err)
	}
	rest, last := planningPage(t, mux, p, path+"?limit=7&cursor="+url.QueryEscape(next))
	if len(rest) != 1 || rest[0]["id"] != ids[20] || last != "" {
		t.Fatalf("template continuation %+v cursor%q", rest, last)
	}
	for _, q := range []string{"limit=0", "limit=101", "cursor=bad", "q=changed&cursor=" + url.QueryEscape(next)} {
		requirePlanningPageError(t, mux, p, path+"?"+q, 400)
	}
	requirePlanningPageError(t, mux, platform.Principal{UserID: user}, path, 403)
	requirePlanningPageError(t, mux, platform.Principal{UserID: user, SystemRole: "admin"}, path, 403)
}
