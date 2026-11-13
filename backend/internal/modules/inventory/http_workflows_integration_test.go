//go:build integration

package inventory

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"strings"
	"testing"
)

func data(t *testing.T, wbody []byte) map[string]any {
	t.Helper()
	var body struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(wbody, &body); err != nil {
		t.Fatal(err)
	}
	return body.Data
}

func TestPantryHTTPConsumptionAndFilterWorkflow(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	base := "/it4788/v1/spaces/" + f.sid + "/lots"
	body := fmt.Sprintf(`{"foodId":%q,"unitId":%q,"quantity":2,"storageLocation":"chill","purchasedOn":"2026-11-09","expiresOn":"2026-11-12"}`, f.food, f.unit)
	w := api(t, f, "POST", base, body, f.uid, "")
	if w.Code != 201 {
		t.Fatalf("create %d %s", w.Code, w.Body)
	}
	id := data(t, w.Body.Bytes())["id"].(string)
	consume := base + "/" + id + "/consume"
	for _, tc := range []struct {
		body   string
		status int
	}{{fmt.Sprintf(`{"quantity":0.25,"unitId":%q,"strategy":"fefo"}`, f.unit), 200}, {fmt.Sprintf(`{"quantity":3,"unitId":%q,"strategy":"lot"}`, f.unit), 422}, {`{"quantity":1,"strategy":"wrong"}`, 400}, {`{"quantity":0}`, 400}, {`{`, 400}} {
		w = api(t, f, "POST", consume, tc.body, f.uid, "")
		if w.Code != tc.status {
			t.Fatalf("consume %s: %d %s", tc.body, w.Code, w.Body)
		}
	}
	for _, tc := range []struct {
		body   string
		status int
	}{{`{"storageLocation":"freezer","notes":"Keep frozen","expiresOn":"2026-12-01"}`, 200}, {`{"storageLocation":"unknown"}`, 400}, {`{"expiresOn":"2026-11-01"}`, 400}, {`{"remainingQuantity":3}`, 400}, {`{"notes":"` + strings.Repeat("a", 201) + `"}`, 400}} {
		current := api(t, f, "GET", base+"/"+id, "", f.uid, "")
		if current.Code != 200 {
			t.Fatalf("current lot%d %s", current.Code, current.Body)
		}
		w = api(t, f, "PATCH", base+"/"+id, tc.body, f.uid, current.Header().Get("ETag"))
		if w.Code != tc.status {
			t.Fatalf("patch %s: %d %s", tc.body, w.Code, w.Body)
		}
	}
	for _, query := range []string{"?location=freezer", "?categoryId=00000000-0000-7000-8000-000000000001", "?q=carrot", "?limit=1"} {
		w = api(t, f, "GET", base+query, "", f.uid, "")
		if w.Code != 200 {
			t.Fatalf("filter %s %d %s", query, w.Code, w.Body)
		}
	}
	if w = api(t, f, "GET", base+"?limit=0", "", f.uid, ""); w.Code != 400 {
		t.Fatalf("bad limit %d", w.Code)
	}
	if w = api(t, f, "GET", base+"/expiring", "", f.uid, ""); w.Code != 200 || len(data(t, w.Body.Bytes())["lots"].([]any)) != 0 {
		t.Fatalf("expiry %d %s", w.Code, w.Body)
	}
	if w = api(t, f, "GET", base+"/"+id, "", f.uid, ""); w.Code != 200 || len(data(t, w.Body.Bytes())["movements"].([]any)) != 2 {
		t.Fatalf("history %d %s", w.Code, w.Body)
	}
	if w = api(t, f, "POST", base+"/"+id+"/discard", `{"quantity":1.75,"reason":"spoiled"}`, f.uid, ""); w.Code != 200 {
		t.Fatalf("discard all %d %s", w.Code, w.Body)
	}
	var state string
	if err := f.db.Pool.QueryRow(ctx, "SELECT lifecycle FROM pantry.food_lots WHERE id=$1", id).Scan(&state); err != nil || state != "discarded" {
		t.Fatalf("state %s %v", state, err)
	}
}

func TestShoppingHTTPListItemCopyAndDeleteWorkflow(t *testing.T) {
	f := setup(t)
	base := "/it4788/v1/spaces/" + f.sid
	w := api(t, f, "POST", base+"/shopping-lists", `{"name":"Market","plannedDate":"2026-11-09"}`, f.uid, "")
	if w.Code != 201 {
		t.Fatalf("create %d %s", w.Code, w.Body)
	}
	id := data(t, w.Body.Bytes())["id"].(string)
	path := base + "/shopping-lists/" + id
	w = api(t, f, "PATCH", path, `{"name":"Market updated","notes":"Remember","plannedDate":"2026-11-10"}`, f.uid, "1")
	if w.Code != 200 {
		t.Fatalf("patch %d %s", w.Code, w.Body)
	}
	w = api(t, f, "PATCH", path, `{"name":"Stale"}`, f.uid, "1")
	checkOverwritten(t, inventoryMutation(t, w), "name")
	w = api(t, f, "POST", path+"/items", fmt.Sprintf(`{"foodId":%q,"unitId":%q,"quantity":1}`, f.food, f.unit), f.uid, "")
	if w.Code != 201 {
		t.Fatalf("item %d %s", w.Code, w.Body)
	}
	item := data(t, w.Body.Bytes())["id"].(string)
	itemPath := base + "/shopping-items/" + item
	if w = api(t, f, "PATCH", itemPath, `{"quantity":1.5,"notes":"Large carrots"}`, f.uid, "1"); w.Code != 200 {
		t.Fatalf("item patch %d %s", w.Code, w.Body)
	}
	if w = api(t, f, "GET", path, "", f.uid, ""); w.Code != 200 {
		t.Fatalf("get %d %s", w.Code, w.Body)
	}
	if w = api(t, f, "GET", base+"/shopping-lists?from=2026-11-09&to=2026-11-30&status=draft", "", f.uid, ""); w.Code != 200 || len(data(t, w.Body.Bytes())["lists"].([]any)) != 1 {
		t.Fatalf("list %d %s", w.Code, w.Body)
	}
	if w = api(t, f, "POST", base+"/shopping-items/purchase", fmt.Sprintf(`{"items":[{"itemId":%q,"actualQuantity":2,"unitPrice":12500}]}`, item), f.uid, ""); w.Code != 200 {
		t.Fatalf("purchase %d %s", w.Code, w.Body)
	}
	if w = api(t, f, "GET", path, "", f.uid, ""); w.Code != 200 || data(t, w.Body.Bytes())["list"].(map[string]any)["totalCost"] != float64(25000) {
		t.Fatalf("cost %d %s", w.Code, w.Body)
	}
	if w = api(t, f, "PATCH", itemPath, `{"unitPrice":10000,"actualQuantity":1}`, f.uid, "1"); w.Code != 200 {
		t.Fatalf("completed price %d %s", w.Code, w.Body)
	}
	if w = api(t, f, "PATCH", itemPath, `{"quantity":2}`, f.uid, "1"); w.Code != 422 {
		t.Fatalf("completed quantity %d", w.Code)
	}
	if w = api(t, f, "DELETE", path, "", f.uid, ""); w.Code != 422 {
		t.Fatalf("delete completed %d", w.Code)
	}
	w = api(t, f, "POST", path+"/copy", `{"plannedDate":"2026-11-11"}`, f.uid, "")
	if w.Code != 201 {
		t.Fatalf("copy %d %s", w.Code, w.Body)
	}
	copyID := data(t, w.Body.Bytes())["id"].(string)
	w = api(t, f, "GET", base+"/shopping-lists/"+copyID, "", f.uid, "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	out := data(t, w.Body.Bytes())
	copied := out["items"].([]any)[0].(map[string]any)
	if copied["status"] != "to_buy" || copied["actualQuantity"] != nil || copied["unitPrice"] != nil || copied["assigneeId"] != nil {
		t.Fatalf("copy retained purchase state %+v", copied)
	}
	copiedItem := copied["id"].(string)
	if w = api(t, f, "DELETE", base+"/shopping-items/"+copiedItem, "", f.uid, ""); w.Code != 200 {
		t.Fatalf("item delete %d %s", w.Code, w.Body)
	}
	if w = api(t, f, "DELETE", base+"/shopping-lists/"+copyID, "", f.uid, ""); w.Code != 200 {
		t.Fatalf("list delete %d %s", w.Code, w.Body)
	}
	if w = api(t, f, "GET", base+"/shopping-lists/"+copyID, "", f.uid, ""); w.Code != 404 {
		t.Fatalf("deleted list read %d", w.Code)
	}
}

func TestInventoryHTTPRejectsInvalidMutationsAndForeignReads(t *testing.T) {
	f := setup(t)
	base := "/it4788/v1/spaces/" + f.sid
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{{"POST", "/lots", `{`, 400}, {"POST", "/shopping-lists", `{"name":"","plannedDate":"2026-11-09"}`, 400}, {"POST", "/shopping-lists", `{"name":"Market","plannedDate":"bad"}`, 400}, {"POST", "/shopping-lists", `{"name":"Market","plannedDate":"2026-11-08"}`, 400}, {"GET", "/shopping-lists?limit=101", "", 400}, {"GET", "/lots?limit=bad", "", 400}} {
		w := api(t, f, tc.method, base+tc.path, tc.body, f.uid, "")
		if w.Code != tc.status {
			t.Fatalf("%s %s %d %s", tc.method, tc.path, w.Code, w.Body)
		}
	}
	list, err := f.s.CreateList(context.Background(), f.sid, f.uid, ListInput{Name: "Market", PlannedDate: "2026-11-09"})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/shopping-lists", "/shopping-lists/" + list.ID, "/lots", "/lots/expiring"} {
		w := api(t, f, "GET", base+path, "", f.other, "")
		if w.Code != 404 {
			t.Fatalf("foreign %s %d %s", path, w.Code, w.Body)
		}
	}
	item, err := f.s.AddItem(context.Background(), f.sid, f.uid, list.ID, ItemInput{Name: "Free", UnitID: f.unit, Quantity: qty("1")})
	if err != nil {
		t.Fatal(err)
	}
	itemPath := base + "/shopping-items/" + item.ID
	for _, tc := range []struct {
		body   string
		status int
	}{{`{"name":"Renamed","notes":"Note","quantity":2}`, 200}, {`{"name":""}`, 400}, {`{"quantity":0}`, 400}, {`{"actualQuantity":1}`, 422}, {`{"unitPrice":-1}`, 400}, {`{"status":"purchased"}`, 422}, {`{"status":"skipped"}`, 200}} {
		w := api(t, f, "PATCH", itemPath, tc.body, f.uid, "1")
		if w.Code != tc.status {
			t.Fatalf("patch %s %d %s", tc.body, w.Code, w.Body)
		}
	}
	if w := api(t, f, "POST", itemPath+"/undo-purchase", "", f.uid, ""); w.Code != 422 {
		t.Fatalf("undo unpurchased %d", w.Code)
	}
}

func TestPendingSupplyUsesPurchaseAndPlannedDatesAndExcludesStocked(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	a, err := f.s.CreateList(ctx, f.sid, f.uid, ListInput{Name: "Early", PlannedDate: "2026-11-10"})
	if err != nil {
		t.Fatal(err)
	}
	item, err := f.s.AddItem(ctx, f.sid, f.uid, a.ID, ItemInput{FoodID: f.food, UnitID: f.unit, Quantity: qty("0.1")})
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.s.CreateList(ctx, f.sid, f.uid, ListInput{Name: "Late", PlannedDate: "2026-11-20"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.AddItem(ctx, f.sid, f.uid, b.ID, ItemInput{FoodID: f.food, UnitID: f.unit, Quantity: qty("9")}); err != nil {
		t.Fatal(err)
	}
	err = f.db.Tx(ctx, func(tx pgx.Tx) error {
		pending, err := f.s.Pending(ctx, tx, f.sid, day("2026-11-09"), day("2026-11-12"))
		if err != nil {
			return err
		}
		if len(pending) != 1 || pending[0].Quantity != qty("0.1") || pending[0].AvailableOn.Format("2006-01-02") != "2026-11-10" {
			t.Fatalf("pending %+v", pending)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Purchase(ctx, f.sid, f.uid, []PurchaseInput{{ItemID: item.ID}}); err != nil {
		t.Fatal(err)
	}
	err = f.db.Tx(ctx, func(tx pgx.Tx) error {
		pending, err := f.s.Pending(ctx, tx, f.sid, day("2026-11-09"), day("2026-11-12"))
		if err != nil {
			return err
		}
		if len(pending) != 1 || pending[0].AvailableOn.Format("2006-01-02") != "2026-11-09" {
			t.Fatalf("purchased pending %+v", pending)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.StockItems(ctx, f.sid, f.uid, []StockItemInput{{ItemID: item.ID, StorageLocation: "chill", ExpiresOn: "2026-11-12"}}); err != nil {
		t.Fatal(err)
	}
	err = f.db.Tx(ctx, func(tx pgx.Tx) error {
		pending, err := f.s.Pending(ctx, tx, f.sid, day("2026-11-09"), day("2026-11-12"))
		if err != nil {
			return err
		}
		if len(pending) != 0 {
			t.Fatalf("stocked counted twice %+v", pending)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
