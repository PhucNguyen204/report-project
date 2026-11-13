//go:build integration

package inventory

import (
	"context"
	"dicho/internal/platform"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

func TestRepeatedPurchasedBatchReturnsConflictWhenNothingChanges(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	list, err := f.s.CreateList(ctx, f.sid, f.uid, ListInput{Name: "Market", PlannedDate: "2026-11-09"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := f.s.AddItem(ctx, f.sid, f.uid, list.ID, ItemInput{FoodID: f.food, UnitID: f.unit, Quantity: qty("1")})
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.s.AddItem(ctx, f.sid, f.uid, list.ID, ItemInput{Name: "Free food", UnitID: f.unit, Quantity: qty("1")})
	if err != nil {
		t.Fatal(err)
	}
	in := []PurchaseInput{{ItemID: a.ID}, {ItemID: b.ID}}
	if _, err = f.s.Purchase(ctx, f.sid, f.uid, in); err != nil {
		t.Fatal(err)
	}
	_, err = f.s.Purchase(ctx, f.sid, f.uid, in)
	var typed *platform.Error
	if !errors.As(err, &typed) || typed.Status != 409 || typed.Code != 4002 {
		t.Fatalf("all-repeat batch must conflict: %v", err)
	}
}

func TestUndoCompletedListKeepsShoppingState(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	list, err := f.s.CreateList(ctx, f.sid, f.uid, ListInput{Name: "Market", PlannedDate: "2026-11-09"})
	if err != nil {
		t.Fatal(err)
	}
	item, err := f.s.AddItem(ctx, f.sid, f.uid, list.ID, ItemInput{FoodID: f.food, UnitID: f.unit, Quantity: qty("1")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Purchase(ctx, f.sid, f.uid, []PurchaseInput{{ItemID: item.ID}}); err != nil {
		t.Fatal(err)
	}
	path := "/it4788/v1/spaces/" + f.sid + "/shopping-items/" + item.ID + "/undo-purchase"
	if w := api(t, f, "POST", path, "", f.uid, ""); w.Code != 200 {
		t.Fatalf("undo %d %s", w.Code, w.Body)
	}
	var state string
	if err = f.db.Pool.QueryRow(ctx, "SELECT status FROM shopping.shopping_lists WHERE id=$1", list.ID).Scan(&state); err != nil || state != "shopping" {
		t.Fatalf("state %s %v", state, err)
	}
}

func TestLotDetailIncludesMovementHistory(t *testing.T) {
	f := setup(t)
	lot := f.stock(t, "1", "2026-11-12")
	path := "/it4788/v1/spaces/" + f.sid + "/lots/" + lot.ID
	w := api(t, f, "GET", path, "", f.uid, "")
	if w.Code != 200 {
		t.Fatalf("detail %d %s", w.Code, w.Body)
	}
	var body struct {
		Data struct {
			Movements []map[string]any `json:"movements"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data.Movements) != 1 || body.Data.Movements[0]["kind"] != "stock_in" {
		t.Fatalf("history %s", w.Body)
	}
}

func TestAssignmentDigestFutureBurstGetsNewJob(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if _, err := f.db.Pool.Exec(ctx, `INSERT INTO household.space_members(space_id,user_id,role,space_kind) VALUES($1,$2,'member','family')`, f.sid, f.other); err != nil {
		t.Fatal(err)
	}
	list, err := f.s.CreateList(ctx, f.sid, f.uid, ListInput{Name: "Market", PlannedDate: "2026-11-09"})
	if err != nil {
		t.Fatal(err)
	}
	item, err := f.s.AddItem(ctx, f.sid, f.uid, list.ID, ItemInput{FoodID: f.food, UnitID: f.unit, Quantity: qty("1")})
	if err != nil {
		t.Fatal(err)
	}
	path := "/it4788/v1/spaces/" + f.sid + "/shopping-items/" + item.ID + "/assignee"
	body := fmt.Sprintf(`{"userId":%q}`, f.other)
	if w := api(t, f, "PUT", path, body, f.uid, "1"); w.Code != 200 {
		t.Fatalf("assign %d %s", w.Code, w.Body)
	}
	if _, err = f.db.Pool.Exec(ctx, `UPDATE river_job SET state='completed',finalized_at=now() WHERE args->>'event'='assignment.digest'`); err != nil {
		t.Fatal(err)
	}
	if w := api(t, f, "DELETE", path, "", f.uid, "2"); w.Code != 200 {
		t.Fatalf("unassign %d %s", w.Code, w.Body)
	}
	var jobs int
	if err = f.db.Pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE args->>'event'='assignment.digest'`).Scan(&jobs); err != nil || jobs != 2 {
		t.Fatalf("jobs %d %v", jobs, err)
	}
	var payload []byte
	if err = f.db.Pool.QueryRow(ctx, `SELECT args->'payload' FROM river_job WHERE args->>'event'='assignment.digest' ORDER BY id DESC LIMIT 1`).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var p map[string]any
	_ = json.Unmarshal(payload, &p)
	if p["batchId"] == nil {
		t.Fatalf("missing distinct burst id: %s", payload)
	}
	e := platform.Event{Event: "assignment.digest", Key: "testburst", Payload: payload}
	if err = f.s.HandleAssignmentDigest(ctx, e); err != nil {
		t.Fatal(err)
	}
}
