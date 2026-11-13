//go:build integration

package inventory

import (
	"context"
	"dicho/internal/platform"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"strings"
	"testing"
)

func TestPurchaseAndStockRejectInvalidBatchesWithoutChangingItems(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	list, err := f.s.CreateList(ctx, f.sid, f.uid, ListInput{Name: "Bounds", PlannedDate: "2026-11-09"})
	if err != nil {
		t.Fatal(err)
	}
	item, err := f.s.AddItem(ctx, f.sid, f.uid, list.ID, ItemInput{FoodID: f.food, UnitID: f.unit, Quantity: qty("1")})
	if err != nil {
		t.Fatal(err)
	}
	zero := qty("0")
	negative := int64(-1)
	future := f.db.Clock.Now().AddDate(0, 0, 1)
	for _, in := range [][]PurchaseInput{{}, {{ItemID: item.ID}, {ItemID: item.ID}}, {{ItemID: item.ID, ActualQuantity: &zero}}, {{ItemID: item.ID, UnitPrice: &negative}}, {{ItemID: item.ID, PurchasedAt: &future}}} {
		if _, err = f.s.Purchase(ctx, f.sid, f.uid, in); err == nil {
			t.Fatalf("invalid purchase accepted %+v", in)
		}
	}
	for _, in := range []ItemInput{{FoodID: f.food, UnitID: f.unit, Quantity: 0}, {FoodID: f.food, UnitID: f.unit, Quantity: qty("1"), Notes: strings.Repeat("n", 201)}, {UnitID: f.unit, Quantity: qty("1")}, {Name: "Unknown unit", UnitID: platform.ID(), Quantity: qty("1")}, {FoodID: platform.ID(), UnitID: f.unit, Quantity: qty("1")}} {
		if _, err = f.s.AddItem(ctx, f.sid, f.uid, list.ID, in); err == nil {
			t.Fatalf("invalid item accepted %+v", in)
		}
	}
	if _, err = f.s.StockItems(ctx, f.sid, f.uid, []StockItemInput{{ItemID: item.ID, StorageLocation: "chill", ExpiresOn: "2026-11-12"}}); err == nil {
		t.Fatal("unbought item stocked")
	}
	if _, err = f.s.Purchase(ctx, f.sid, f.uid, []PurchaseInput{{ItemID: item.ID}}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.StockItems(ctx, f.sid, f.uid, []StockItemInput{{ItemID: item.ID, PurchasedOn: "bad", StorageLocation: "chill", ExpiresOn: "2026-11-12"}}); err == nil {
		t.Fatal("invalid stock date accepted")
	}
	amount := qty("0.5")
	if _, err = f.s.StockItems(ctx, f.sid, f.uid, []StockItemInput{{ItemID: item.ID, Quantity: &amount, PurchasedOn: "2026-11-08", StorageLocation: "chill", ExpiresOn: "2026-11-12"}}); err != nil {
		t.Fatal(err)
	}
	var remaining string
	if err = f.db.Pool.QueryRow(ctx, `SELECT remaining_qty::text FROM pantry.food_lots WHERE source_item_id=$1`, item.ID).Scan(&remaining); err != nil || remaining != "0.50" {
		t.Fatalf("stock adjustment %s %v", remaining, err)
	}
}

func TestLotAndListPaginationFiltersAndAssignmentCancellation(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	for range 3 {
		f.stock(t, "1", "2026-11-12")
	}
	base := "/it4788/v1/spaces/" + f.sid
	w := api(t, f, "GET", base+"/lots?limit=1&storageLocation=chill", "", f.uid, "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var body struct {
		Data struct {
			Items []Lot  `json:"lots"`
			Next  string `json:"nextCursor"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data.Items) != 1 || body.Data.Next == "" {
		t.Fatalf("pagination %s", w.Body)
	}
	w = api(t, f, "GET", base+"/lots?storageLocation=door", "", f.uid, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"lots":[]`) {
		t.Fatalf("location filter %s", w.Body)
	}
	for range 3 {
		if _, err := f.s.CreateList(ctx, f.sid, f.uid, ListInput{Name: "Filter", PlannedDate: "2026-11-09"}); err != nil {
			t.Fatal(err)
		}
	}
	w = api(t, f, "GET", base+"/shopping-lists?limit=1", "", f.uid, "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = api(t, f, "GET", base+"/shopping-lists?status=completed", "", f.uid, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"lists":[]`) {
		t.Fatalf("list filter %s", w.Body)
	}
	payload, _ := json.Marshal(map[string]string{"spaceId": f.sid, "userId": f.other, "listId": platform.ID()})
	if err := f.s.HandleAssignmentDigest(ctx, platform.Event{Key: "removed", Payload: payload}); err != nil {
		t.Fatal(err)
	}
	var jobs int
	if err := f.db.Pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE args->>'event'='notification'`).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatalf("removed user notified %d %v", jobs, err)
	}
	if err := f.s.HandleAssignmentDigest(ctx, platform.Event{Payload: []byte("{")}); err == nil {
		t.Fatal("malformed event accepted")
	}
	if err := f.db.Tx(ctx, func(tx pgx.Tx) error {
		_, err := f.s.Compose(ctx, tx, f.sid, f.uid, "", []platform.MissingLine{{FoodID: f.food, UnitID: f.unit, Quantity: qty("0.3")}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
