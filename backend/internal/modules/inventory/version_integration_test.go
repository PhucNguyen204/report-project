//go:build integration

package inventory

import (
	"context"
	"dicho/internal/platform"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInventoryMetadataPatchesRequireIfMatch(t *testing.T) {
	f := setup(t)
	lot := f.stock(t, "1", "2026-11-12")
	list, err := f.s.CreateList(context.Background(), f.sid, f.uid, ListInput{Name: "Versioned", PlannedDate: "2026-11-09"})
	if err != nil {
		t.Fatal(err)
	}
	item, err := f.s.AddItem(context.Background(), f.sid, f.uid, list.ID, ItemInput{Name: "Free item", UnitID: f.unit, Quantity: qty("1")})
	if err != nil {
		t.Fatal(err)
	}
	base := "/it4788/v1/spaces/" + f.sid
	for _, tc := range []struct{ path, body string }{{base + "/lots/" + lot.ID, `{"notes":"Missing version"}`}, {base + "/shopping-lists/" + list.ID, `{"notes":"Missing version"}`}, {base + "/shopping-items/" + item.ID, `{"notes":"Missing version"}`}} {
		for _, match := range []string{"", "*", "W/\"1\"", "1,2", "0", "-1", "\"1", "+1"} {
			w := api(t, f, "PATCH", tc.path, tc.body, f.uid, match)
			if w.Code != 400 {
				t.Fatalf("metadata patch%s match%q got%d %s", tc.path, match, w.Code, w.Body)
			}
		}
	}
}

func inventoryMutation(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("mutation%d %s", w.Code, w.Body)
	}
	var body struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Data
}
func checkOverwritten(t *testing.T, data map[string]any, want ...string) {
	t.Helper()
	raw, ok := data["overwrittenFields"].([]any)
	if !ok {
		t.Fatalf("missing overwrittenFields %+v", data)
	}
	got := []string{}
	for _, v := range raw {
		got = append(got, v.(string))
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("overwritten%v want%v", got, want)
	}
}
func TestShoppingMetadataMergesDisjointFieldsAndReportsOnlyOverwrites(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	list, err := f.s.CreateList(ctx, f.sid, f.uid, ListInput{Name: "Original", PlannedDate: "2026-11-09"})
	if err != nil {
		t.Fatal(err)
	}
	base := "/it4788/v1/spaces/" + f.sid
	path := base + "/shopping-lists/" + list.ID
	checkOverwritten(t, inventoryMutation(t, api(t, f, "PATCH", path, `{"name":"First"}`, f.uid, `"1"`)))
	checkOverwritten(t, inventoryMutation(t, api(t, f, "PATCH", path, `{"notes":"Independent"}`, f.uid, `"1"`)))
	checkOverwritten(t, inventoryMutation(t, api(t, f, "PATCH", path, `{"name":"Last"}`, f.uid, `"1"`)), "name")
	item, err := f.s.AddItem(ctx, f.sid, f.uid, list.ID, ItemInput{Name: "Free item", UnitID: f.unit, Quantity: qty("1")})
	if err != nil {
		t.Fatal(err)
	}
	path = base + "/shopping-items/" + item.ID
	checkOverwritten(t, inventoryMutation(t, api(t, f, "PATCH", path, `{"quantity":2}`, f.uid, `"1"`)))
	checkOverwritten(t, inventoryMutation(t, api(t, f, "PATCH", path, `{"notes":"Keep both"}`, f.uid, `"1"`)))
	checkOverwritten(t, inventoryMutation(t, api(t, f, "PATCH", path, `{"quantity":3,"notes":"Keep both"}`, f.uid, `"1"`)), "quantity")
}
func TestLotMetadataRemainsMergeableAfterConsumptionButRemainingQtyIsStrict(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	lot := f.stock(t, "2", "2026-11-12")
	base := "/it4788/v1/spaces/" + f.sid + "/lots/" + lot.ID
	if err := f.db.Tx(ctx, func(tx pgx.Tx) error {
		return f.s.Consume(ctx, tx, f.sid, "", []platform.Consumption{{FoodID: f.food, Quantity: qty("0.5")}})
	}); err != nil {
		t.Fatal(err)
	}
	checkOverwritten(t, inventoryMutation(t, api(t, f, "PATCH", base, `{"storageLocation":"door"}`, f.uid, `"1"`)))
	checkOverwritten(t, inventoryMutation(t, api(t, f, "PATCH", base, `{"notes":"Independent"}`, f.uid, `"1"`)))
	checkOverwritten(t, inventoryMutation(t, api(t, f, "PATCH", base, `{"storageLocation":"freezer"}`, f.uid, `"1"`)), "storageLocation")
	if w := api(t, f, "PATCH", base, `{"remainingQty":2}`, f.uid, `"1"`); w.Code != 409 {
		t.Fatalf("stale quantity%d %s", w.Code, w.Body)
	}
	var amount string
	if err := f.db.Pool.QueryRow(ctx, `SELECT remaining_qty::text FROM pantry.food_lots WHERE id=$1`, lot.ID).Scan(&amount); err != nil || amount != "1.50" {
		t.Fatalf("consumption overwritten %s %v", amount, err)
	}
}

func TestAssignmentPUTRejectsStaleVersionWithoutOverwritingAssignee(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if _, err := f.db.Pool.Exec(ctx, `INSERT INTO household.space_members(space_id,user_id,role,space_kind) VALUES($1,$2,'member','family')`, f.sid, f.other); err != nil {
		t.Fatal(err)
	}
	list, err := f.s.CreateList(ctx, f.sid, f.uid, ListInput{Name: "Strict assignment", PlannedDate: "2026-11-09"})
	if err != nil {
		t.Fatal(err)
	}
	item, err := f.s.AddItem(ctx, f.sid, f.uid, list.ID, ItemInput{Name: "Free item", UnitID: f.unit, Quantity: qty("1")})
	if err != nil {
		t.Fatal(err)
	}
	path := "/it4788/v1/spaces/" + f.sid + "/shopping-items/" + item.ID + "/assignee"
	if w := api(t, f, "PUT", path, `{"userId":"`+f.other+`"}`, f.uid, `"1"`); w.Code != 200 {
		t.Fatalf("assign%d %s", w.Code, w.Body)
	}
	if w := api(t, f, "PUT", path, `{"userId":"`+f.uid+`"}`, f.uid, `"1"`); w.Code != 409 {
		t.Fatalf("stale assignment%d %s", w.Code, w.Body)
	}
	var assignee string
	if err := f.db.Pool.QueryRow(ctx, `SELECT assignee_id::text FROM shopping.shopping_items WHERE id=$1`, item.ID).Scan(&assignee); err != nil || assignee != f.other {
		t.Fatalf("stale assignment applied %s %v", assignee, err)
	}
}
