//go:build integration

package app

import "testing"

func TestOpenAPICookingExternalIngredientSelectedLotsAndReplay(t *testing.T) {
	h := newAPIHarness(t)
	token, sid, user := h.user(t, "cook-contract@example.test")
	base := "/it4788/v1/spaces/" + sid
	today := h.system.DB.Today()
	food, unit := "00000000-0000-7000-8000-000000000021", "00000000-0000-7000-8000-000000000011"
	r := h.call(t, "POST", base+"/recipes", map[string]any{"name": "Rau hấp xác nhận", "servings": 2, "ingredients": []any{map[string]any{"foodId": food, "unitId": unit, "quantity": 100}}}, token, "", 201)
	meal := func(slot string) map[string]any {
		return h.call(t, "POST", base+"/meals", map[string]any{"recipeId": field(r, "id"), "date": today.Format("2006-01-02"), "mealSlot": slot, "servings": 2}, token, "", 201)
	}
	m0 := meal("breakfast")
	zero := h.call(t, "POST", base+"/meals/"+field(m0, "id")+"/cook", map[string]any{"lines": []any{map[string]any{"foodId": food, "unitId": unit, "quantity": 0}}}, token, "1", 200)["data"].(map[string]any)
	if zero["alreadyCooked"] != false || len(zero["deductions"].([]any)) != 0 || zero["meal"].(map[string]any)["cookedBy"] != user || zero["meal"].(map[string]any)["cookedAt"] == nil {
		t.Fatalf("external ingredient metadata missing: %v", zero)
	}
	ids := []string{}
	for days := 1; days <= 2; days++ {
		lot := h.call(t, "POST", base+"/lots", map[string]any{"foodId": food, "unitId": unit, "quantity": 150, "storageLocation": "chill", "purchasedOn": today.Format("2006-01-02"), "expiresOn": today.AddDate(0, 0, days).Format("2006-01-02")}, token, "", 201)
		ids = append(ids, field(lot, "id"))
	}
	m := meal("lunch")
	path := base + "/meals/" + field(m, "id") + "/cook"
	cooked := h.call(t, "POST", path, map[string]any{"lines": []any{map[string]any{"foodId": food, "unitId": unit, "quantity": 200, "lotIds": ids}}}, token, "1", 200)["data"].(map[string]any)
	lines := cooked["deductions"].([]any)
	if cooked["alreadyCooked"] != false || len(lines) != 2 {
		t.Fatalf("selected lot deductions missing: %v", cooked)
	}
	var quantity float64
	for _, line := range lines {
		quantity += line.(map[string]any)["quantity"].(float64)
	}
	if quantity != 200 {
		t.Fatalf("incorrect base deduction quantity %v", quantity)
	}
	replay := h.call(t, "POST", path, map[string]any{"skipStock": true}, token, "1", 200)["data"].(map[string]any)
	if replay["alreadyCooked"] != true || len(replay["deductions"].([]any)) != 2 || replay["meal"].(map[string]any)["cookedAt"] != cooked["meal"].(map[string]any)["cookedAt"] || replay["meal"].(map[string]any)["cookedBy"] != user {
		t.Fatal("replay lost original actor/time/history")
	}
	short := meal("dinner")
	conflict := h.call(t, "POST", base+"/meals/"+field(short, "id")+"/cook", map[string]any{"lines": []any{map[string]any{"foodId": food, "unitId": unit, "quantity": 500}}}, token, "1", 409)
	current := conflict["current"].([]any)
	if conflict["code"] != float64(4002) || len(current) == 0 || current[0].(map[string]any)["foodId"] != food {
		t.Fatal("stock conflict lacks current scoped stock")
	}
	for _, selection := range []map[string]any{{"lotIds": []string{ids[0]}}, {"lotId": ids[0]}} {
		selection["foodId"], selection["unitId"], selection["quantity"] = food, unit, 1
		stale := h.call(t, "POST", base+"/meals/"+field(short, "id")+"/cook", map[string]any{"lines": []any{selection}}, token, "1", 409)
		if _, ok := stale["current"].([]any); !ok {
			t.Fatal("depleted selected lot conflict lost snapshot")
		}
	}
	h.call(t, "GET", base+"/meals?from="+today.Format("2006-01-02")+"&to="+today.Format("2006-01-02"), nil, token, "", 200)
}
