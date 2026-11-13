//go:build integration

package planning

import (
	"bytes"
	"context"
	"dicho/internal/modules/inventory"
	"dicho/internal/platform"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func planningRequest(t *testing.T, h http.Handler, p platform.Principal, method, path string, body any, version string) (int, map[string]any) {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(b))
	r = r.WithContext(platform.WithPrincipal(context.Background(), p))
	if version != "" {
		r.Header.Set("If-Match", version)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var result map[string]any
	if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body)
	}
	return w.Code, result
}
func TestPlanningHTTPCompleteFlowsAndValidation(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	inv := inventory.New(db, s.catalog)
	s.stock = inv
	s.lists = inv
	mux := http.NewServeMux()
	s.Register(mux)
	principal := platform.Principal{UserID: user}
	base := "/it4788/v1/spaces/" + sid
	in := map[string]any{"name": "Canh cà rốt", "servings": 1, "ingredients": []any{map[string]any{"foodId": food, "unitId": unit, "quantity": "0.2"}}}
	status, body := planningRequest(t, mux, principal, "POST", base+"/recipes", in, "")
	if status != 201 {
		t.Fatalf("%d %+v", status, body)
	}
	rid := body["data"].(map[string]any)["id"].(string)
	in["name"] = "Canh mới"
	status, body = planningRequest(t, mux, principal, "PUT", base+"/recipes/"+rid, in, "1")
	if status != 200 {
		t.Fatalf("%d %+v", status, body)
	}
	for _, path := range []string{base + "/recipes?source=space&limit=10", base + "/recipes/" + rid + "?servings=2", base + "/search?q=ca&limit=10", base + "/foods/suggest?q=ca&limit=10", base + "/recipe-suggestions?servings=1&limit=3&expiringOnly=true"} {
		status, body = planningRequest(t, mux, principal, "GET", path, nil, "")
		if status != 200 {
			t.Fatalf("%s %d %+v", path, status, body)
		}
	}
	for _, method := range []string{"PUT", "DELETE"} {
		status, body = planningRequest(t, mux, principal, method, "/it4788/v1/me/favorite-recipes/"+rid, nil, "")
		if status != 200 {
			t.Fatalf("%d %+v", status, body)
		}
	}
	mealIn := map[string]any{"recipeId": rid, "date": db.Today().Format("2006-01-02"), "mealSlot": "lunch", "servings": 1}
	status, body = planningRequest(t, mux, principal, "POST", base+"/meals", mealIn, "")
	if status != 201 {
		t.Fatalf("%d %+v", status, body)
	}
	mid := body["data"].(map[string]any)["id"].(string)
	status, body = planningRequest(t, mux, principal, "PATCH", base+"/meals/"+mid, map[string]any{"servings": 2}, "1")
	if status != 200 {
		t.Fatalf("%d %+v", status, body)
	}
	status, body = planningRequest(t, mux, principal, "GET", base+"/meals?from="+db.Today().Format("2006-01-02")+"&to="+db.Today().Format("2006-01-02"), nil, "")
	if status != 200 {
		t.Fatalf("%d %+v", status, body)
	}
	generate := map[string]any{"from": db.Today().Format("2006-01-02"), "to": db.Today().Format("2006-01-02"), "dryRun": true}
	status, body = planningRequest(t, mux, principal, "POST", base+"/shopping-lists/generate", generate, "")
	if status != 200 {
		t.Fatalf("%d %+v", status, body)
	}
	status, body = planningRequest(t, mux, principal, "POST", base+"/recipes/"+rid+"/missing-to-list", map[string]any{"servings": 1, "dryRun": true}, "")
	if status != 200 {
		t.Fatalf("%d %+v", status, body)
	}
	status, body = planningRequest(t, mux, principal, "POST", base+"/meals/"+mid+"/cook", map[string]any{"skipStock": true}, "2")
	if status != 200 {
		t.Fatalf("%d %+v", status, body)
	}
	for _, kind := range []string{"spending", "consumption", "waste"} {
		status, body = planningRequest(t, mux, principal, "GET", base+"/reports/"+kind+"?groupBy=week", nil, "")
		if status != 200 {
			t.Fatalf("%d %+v", status, body)
		}
	}
	for _, path := range []string{base + "/recipes?source=bad", base + "/recipes?limit=0", base + "/recipes/" + rid + "?servings=21", base + "/recipe-suggestions?servings=0", base + "/recipe-suggestions?limit=-1", base + "/recipe-suggestions?expiringOnly=bad", base + "/meals?from=bad", base + "/reports/waste?from=bad", base + "/search?limit=bad", base + "/foods/suggest?q=a"} {
		status, _ = planningRequest(t, mux, principal, "GET", path, nil, "")
		if status != 400 {
			t.Fatalf("invalid %s status %d", path, status)
		}
	}
	free, err := s.CreateMeal(ctx, user, sid, MealInput{Name: "Món tự do", Date: db.Today().Format("2006-01-02"), MealSlot: "dinner", Servings: 1})
	if err != nil {
		t.Fatal(err)
	}
	status, _ = planningRequest(t, mux, principal, "DELETE", base+"/meals/"+free.ID, nil, "")
	if status != 200 {
		t.Fatal(status)
	}
}

func TestTemplateHTTPAndUserMutationForbidden(t *testing.T) {
	s, db, user, sid, unit, food := setupPlanning(t)
	ctx := context.Background()
	admin := platform.ID()
	_, err := db.Pool.Exec(ctx, "INSERT INTO identity.users(id,email,full_name,password_hash,status,system_role) VALUES($1,'templatehttp@example.com','Admin HTTP','hash','active','admin')", admin)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s.Register(mux)
	p := platform.Principal{UserID: admin, SystemRole: "admin"}
	path := "/it4788/v1/admin/recipe-templates"
	in := map[string]any{"name": "Công thức mẫu", "servings": 1, "status": "draft", "ingredients": []any{map[string]any{"foodId": food, "unitId": unit, "quantity": "1"}}}
	status, body := planningRequest(t, mux, p, "POST", path, in, "")
	if status != 201 {
		t.Fatalf("%d %+v", status, body)
	}
	id := body["data"].(map[string]any)["id"].(string)
	in["status"] = "published"
	status, body = planningRequest(t, mux, p, "PUT", path+"/"+id, in, "1")
	if status != 200 {
		t.Fatalf("%d %+v", status, body)
	}
	status, _ = planningRequest(t, mux, p, "GET", path, nil, "")
	if status != 200 {
		t.Fatal(status)
	}
	inUser := map[string]any{"name": "Sửa trái phép", "servings": 1, "ingredients": in["ingredients"]}
	status, body = planningRequest(t, mux, platform.Principal{UserID: user}, "PUT", "/it4788/v1/spaces/"+sid+"/recipes/"+id, inUser, "2")
	if status != 403 {
		t.Fatalf("template mutation %d %+v", status, body)
	}
	status, _ = planningRequest(t, mux, p, "DELETE", path+"/"+id, nil, "")
	if status != 200 {
		t.Fatal(status)
	}
}
