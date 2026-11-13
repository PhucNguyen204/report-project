//go:build integration

package planning

import (
	"context"
	"dicho/internal/platform"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPlanningHTTPRejectsForeignSpacesAcrossEveryRoute(t *testing.T) {
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
	p := platform.Principal{UserID: platform.ID()}
	base := "/it4788/v1/spaces/" + sid
	recipeIn := RecipeInput{Name: "Canh", Servings: 1, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("0.2")}}}
	mealIn := MealInput{RecipeID: r.ID, Date: db.Today().Format("2006-01-02"), MealSlot: "lunch", Servings: 1}
	for _, v := range []struct {
		method, path string
		body         any
	}{
		{"GET", base + "/recipes", nil}, {"POST", base + "/recipes", recipeIn}, {"GET", base + "/recipes/" + r.ID, nil}, {"PUT", base + "/recipes/" + r.ID, recipeIn}, {"DELETE", base + "/recipes/" + r.ID, nil}, {"POST", base + "/recipes/" + r.ID + "/copy", map[string]string{}},
		{"PUT", "/it4788/v1/me/favorite-recipes/" + r.ID, nil}, {"DELETE", "/it4788/v1/me/favorite-recipes/" + r.ID, nil},
		{"GET", base + "/meals", nil}, {"POST", base + "/meals", mealIn}, {"PATCH", base + "/meals/" + m.ID, map[string]int{"servings": 2}}, {"DELETE", base + "/meals/" + m.ID, nil}, {"POST", base + "/meals/" + m.ID + "/cook", CookInput{SkipStock: true}},
		{"POST", base + "/shopping-lists/generate", GenerateInput{From: mealIn.Date, To: mealIn.Date, DryRun: true}}, {"POST", base + "/recipes/" + r.ID + "/missing-to-list", RecipeMissingInput{Servings: 1, DryRun: true}},
		{"GET", base + "/recipe-suggestions", nil}, {"GET", base + "/reports/spending", nil}, {"GET", base + "/reports/consumption", nil}, {"GET", base + "/reports/waste", nil}, {"GET", base + "/search", nil}, {"GET", base + "/foods/suggest?q=ca", nil},
	} {
		t.Run(v.method+v.path, func(t *testing.T) {
			status, body := planningRequest(t, mux, p, v.method, v.path, v.body, "1")
			if status != 404 || body["code"] != float64(4001) {
				t.Fatalf("foreign request %d %+v", status, body)
			}
		})
	}
}

func TestPlanningHTTPValidatesMalformedBodiesAndQueryBoundaries(t *testing.T) {
	s, db, user, sid, _, _ := setupPlanning(t)
	mux := http.NewServeMux()
	s.Register(mux)
	base := "/it4788/v1/spaces/" + sid
	p := platform.Principal{UserID: user, SystemRole: "admin"}
	id := platform.ID()
	for _, v := range []struct{ method, path string }{{"POST", base + "/recipes"}, {"PUT", base + "/recipes/" + id}, {"POST", base + "/recipes/" + id + "/copy"}, {"POST", base + "/meals"}, {"PATCH", base + "/meals/" + id}, {"POST", base + "/meals/" + id + "/cook"}, {"POST", base + "/shopping-lists/generate"}, {"POST", base + "/recipes/" + id + "/missing-to-list"}, {"POST", "/it4788/v1/admin/recipe-templates"}, {"PUT", "/it4788/v1/admin/recipe-templates/" + id}} {
		t.Run(v.method+v.path, func(t *testing.T) {
			req := httptest.NewRequest(v.method, v.path, strings.NewReader("{"))
			req = req.WithContext(platform.WithPrincipal(req.Context(), p))
			req.Header.Set("If-Match", "1")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != 400 {
				t.Fatalf("malformed body %d %s", w.Code, w.Body)
			}
		})
	}
	for _, path := range []string{base + "/recipe-suggestions?servings=bad", base + "/recipe-suggestions?limit=bad", base + "/recipes?limit=bad", base + "/recipes/" + id + "?servings=bad", base + "/meals?to=bad", base + "/meals?from=" + db.Today().Format("2006-01-02") + "&to=2000-01-01", base + "/reports/waste?to=bad", base + "/reports/waste?groupBy=bad"} {
		t.Run(path, func(t *testing.T) {
			status, body := planningRequest(t, mux, p, "GET", path, nil, "")
			if status != 400 {
				t.Fatalf("invalid query %d %+v", status, body)
			}
		})
	}
	status, _ := planningRequest(t, mux, p, "PATCH", base+"/meals/"+id, map[string]int{"servings": 2}, "")
	if status != 409 {
		t.Fatalf("missing version %d", status)
	}
	status, _ = planningRequest(t, mux, p, "PUT", base+"/recipes/"+id, map[string]int{}, "")
	if status != 409 {
		t.Fatalf("missing recipe version %d", status)
	}
	for _, method := range []string{"GET", "POST", "PUT", "DELETE"} {
		path := "/it4788/v1/admin/recipe-templates"
		if method == "PUT" || method == "DELETE" {
			path += "/" + id
		}
		status, _ = planningRequest(t, mux, platform.Principal{UserID: user}, method, path, map[string]any{}, "1")
		if status != 403 {
			t.Fatalf("admin %s %d", method, status)
		}
	}
}

func TestRecipeHTTPTemplateCopyFavoriteListAndDelete(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	admin := addPlanningUser(t, db, sid, "")
	if _, err := db.Pool.Exec(ctx, "UPDATE identity.users SET system_role='admin' WHERE id=$1", admin); err != nil {
		t.Fatal(err)
	}
	template, err := s.SaveTemplate(ctx, admin, "", 0, TemplateInput{RecipeInput: RecipeInput{Name: "Mẫu canh", Servings: 1, Ingredients: []IngredientInput{{FoodID: food, UnitID: unit, Quantity: qty("0.2")}}}, Status: "published"})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.Register(mux)
	base := "/it4788/v1/spaces/" + sid
	p := platform.Principal{UserID: user}
	status, body := planningRequest(t, mux, p, "POST", base+"/recipes/"+template.ID+"/copy", map[string]string{"name": "Canh bản sao"}, "")
	if status != 201 {
		t.Fatalf("copy %d %+v", status, body)
	}
	copyID := body["data"].(map[string]any)["id"].(string)
	status, body = planningRequest(t, mux, p, "PUT", "/it4788/v1/me/favorite-recipes/"+copyID, nil, "")
	if status != 200 {
		t.Fatal(status)
	}
	status, body = planningRequest(t, mux, p, "GET", base+"/recipes?source=favorite", nil, "")
	if status != 200 {
		t.Fatal(status)
	}
	items := body["data"].(map[string]any)["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["id"] != copyID {
		t.Fatalf("favorites %+v", body)
	}
	status, body = planningRequest(t, mux, p, "DELETE", base+"/recipes/"+copyID, nil, "")
	if status != 200 {
		t.Fatalf("delete %d %+v", status, body)
	}
	status, _ = planningRequest(t, mux, p, "GET", base+"/recipes/"+copyID, nil, "")
	if status != 404 {
		t.Fatalf("deleted visible %d", status)
	}
}
