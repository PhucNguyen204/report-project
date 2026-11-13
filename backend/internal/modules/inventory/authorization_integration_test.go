//go:build integration

package inventory

import (
	"context"
	"fmt"
	"testing"
)

func TestEveryInventoryWriteRejectsForeignSpace(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	lot := f.stock(t, "1", "2026-11-12")
	list, err := f.s.CreateList(ctx, f.sid, f.uid, ListInput{Name: "Market", PlannedDate: "2026-11-09"})
	if err != nil {
		t.Fatal(err)
	}
	item, err := f.s.AddItem(ctx, f.sid, f.uid, list.ID, ItemInput{FoodID: f.food, UnitID: f.unit, Quantity: qty("1")})
	if err != nil {
		t.Fatal(err)
	}
	base := "/it4788/v1/spaces/" + f.sid
	lp := "/lots/" + lot.ID
	sp := "/shopping-lists/" + list.ID
	ip := "/shopping-items/" + item.ID
	for _, tc := range []struct{ method, path, body string }{
		{"POST", "/lots", fmt.Sprintf(`{"foodId":%q,"unitId":%q,"quantity":1,"storageLocation":"chill","expiresOn":"2026-11-12"}`, f.food, f.unit)},
		{"PATCH", lp, `{"remainingQuantity":0.5}`}, {"POST", lp + "/consume", fmt.Sprintf(`{"quantity":0.5,"unitId":%q}`, f.unit)}, {"POST", lp + "/discard", `{"quantity":0.5,"reason":"bad"}`},
		{"POST", "/shopping-lists", `{"name":"Foreign list","plannedDate":"2026-11-09"}`}, {"PATCH", sp, `{"name":"Hacked"}`}, {"DELETE", sp, ""}, {"POST", sp + "/copy", `{"plannedDate":"2026-11-10"}`},
		{"POST", sp + "/items", fmt.Sprintf(`{"foodId":%q,"unitId":%q,"quantity":1}`, f.food, f.unit)}, {"PATCH", ip, `{"notes":"Hacked"}`}, {"DELETE", ip, ""},
		{"PUT", ip + "/assignee", fmt.Sprintf(`{"userId":%q}`, f.uid)}, {"DELETE", ip + "/assignee", ""}, {"POST", "/shopping-items/purchase", fmt.Sprintf(`{"items":[{"itemId":%q}]}`, item.ID)}, {"POST", ip + "/undo-purchase", ""}, {"POST", "/shopping-items/stock", fmt.Sprintf(`{"items":[{"itemId":%q,"storageLocation":"chill","expiresOn":"2026-11-12"}]}`, item.ID)},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			w := api(t, f, tc.method, base+tc.path, tc.body, f.other, "1")
			if w.Code != 404 {
				t.Fatalf("foreign mutation %d %s", w.Code, w.Body)
			}
		})
	}
	var amount, name, state string
	var lists, lots int
	if err = f.db.Pool.QueryRow(ctx, "SELECT remaining_qty::text FROM pantry.food_lots WHERE id=$1", lot.ID).Scan(&amount); err != nil || amount != "1.00" {
		t.Fatalf("foreign quantity changed %s %v", amount, err)
	}
	if err = f.db.Pool.QueryRow(ctx, "SELECT name FROM shopping.shopping_lists WHERE id=$1", list.ID).Scan(&name); err != nil || name != "Market" {
		t.Fatalf("foreign name changed %s %v", name, err)
	}
	if err = f.db.Pool.QueryRow(ctx, "SELECT status FROM shopping.shopping_items WHERE id=$1", item.ID).Scan(&state); err != nil || state != "to_buy" {
		t.Fatalf("foreign state changed %s %v", state, err)
	}
	_ = f.db.Pool.QueryRow(ctx, "SELECT count(*) FROM shopping.shopping_lists").Scan(&lists)
	_ = f.db.Pool.QueryRow(ctx, "SELECT count(*) FROM pantry.food_lots").Scan(&lots)
	if lists != 1 || lots != 1 {
		t.Fatalf("foreign inserts %d %d", lists, lots)
	}
}

func TestInventoryMutationJSONAndMissingResourceErrors(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	list, err := f.s.CreateList(ctx, f.sid, f.uid, ListInput{Name: "Market", PlannedDate: "2026-11-09"})
	if err != nil {
		t.Fatal(err)
	}
	base := "/it4788/v1/spaces/" + f.sid
	missing := "00000000-0000-7000-8000-999999999999"
	lp := "/lots/" + missing
	sp := "/shopping-lists/" + missing
	ip := "/shopping-items/" + missing
	for _, tc := range []struct{ method, path string }{{"POST", "/lots"}, {"PATCH", lp}, {"POST", lp + "/consume"}, {"POST", lp + "/discard"}, {"POST", "/shopping-lists"}, {"PATCH", sp}, {"POST", sp + "/copy"}, {"POST", "/shopping-lists/" + list.ID + "/items"}, {"PATCH", ip}, {"PUT", ip + "/assignee"}, {"POST", "/shopping-items/purchase"}, {"POST", "/shopping-items/stock"}} {
		w := api(t, f, tc.method, base+tc.path, `{`, f.uid, "1")
		if w.Code != 400 {
			t.Fatalf("invalid json %s %s: %d %s", tc.method, tc.path, w.Code, w.Body)
		}
	}
	for _, tc := range []struct{ method, path, body string }{{"GET", lp, ""}, {"PATCH", lp, `{"notes":"x"}`}, {"POST", lp + "/consume", fmt.Sprintf(`{"quantity":1,"unitId":%q}`, f.unit)}, {"POST", lp + "/discard", `{"quantity":1,"reason":"bad"}`}, {"GET", sp, ""}, {"PATCH", sp, `{"name":"New"}`}, {"DELETE", sp, ""}, {"POST", sp + "/copy", `{"plannedDate":"2026-11-10"}`}, {"PATCH", ip, `{"notes":"x"}`}, {"DELETE", ip, ""}, {"POST", ip + "/undo-purchase", ""}, {"PUT", ip + "/assignee", fmt.Sprintf(`{"userId":%q}`, f.uid)}, {"POST", "/shopping-items/purchase", fmt.Sprintf(`{"items":[{"itemId":%q}]}`, missing)}, {"POST", "/shopping-items/stock", fmt.Sprintf(`{"items":[{"itemId":%q,"storageLocation":"chill"}]}`, missing)}} {
		w := api(t, f, tc.method, base+tc.path, tc.body, f.uid, "1")
		if w.Code != 404 {
			t.Fatalf("missing %s %s: %d %s", tc.method, tc.path, w.Code, w.Body)
		}
	}
}
