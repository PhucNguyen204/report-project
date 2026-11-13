//go:build integration

package planning

import (
	"bytes"
	"context"
	"dicho/internal/platform"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func callPlanning(t *testing.T, h http.Handler, actor, method, path string, body any) (int, map[string]any) {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(b))
	r = r.WithContext(platform.WithPrincipal(context.Background(), platform.Principal{UserID: actor}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var out map[string]any
	if err = json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s %s status%d %s", method, path, w.Code, w.Body.String())
	}
	return w.Code, out
}

func TestRecipeHTTPRejectsOutsidersAndReturnsCreatedRecipe(t *testing.T) {
	s, _, user, sid, unit, food := setupPlanning(t)
	mux := http.NewServeMux()
	s.Register(mux)
	path := "/it4788/v1/spaces/" + sid + "/recipes"
	in := map[string]any{"name": "Canh cà rốt", "servings": 2, "ingredients": []any{map[string]any{"foodId": food, "unitId": unit, "quantity": .2}}}
	status, body := callPlanning(t, mux, user, "POST", path, in)
	if status != 201 {
		t.Fatalf("%d %+v", status, body)
	}
	data := body["data"].(map[string]any)
	rid := data["id"].(string)
	status, body = callPlanning(t, mux, platform.ID(), "GET", path+"/"+rid, nil)
	if status != 404 || body["code"] != float64(4001) {
		t.Fatalf("outsider %d %+v", status, body)
	}
	status, body = callPlanning(t, mux, user, "GET", path, nil)
	if status != 200 {
		t.Fatalf("%d %+v", status, body)
	}
	status, _ = callPlanning(t, mux, user, "POST", path, map[string]any{"name": "x", "servings": 0})
	if status != 400 {
		t.Fatalf("invalid recipe status%d", status)
	}
}
