//go:build integration

package inventory

import (
	"context"
	"dicho/internal/platform"
	"encoding/json"
	"fmt"
	"testing"
)

func TestManualLotHonorsClientUUIDAndRejectsDuplicateOrWrongVersion(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	input := LotInput{ID: "00000000-0000-7000-8000-000000000099", FoodID: f.food, UnitID: f.unit, Quantity: qty("1"), StorageLocation: "chill", PurchasedOn: "2026-11-09", ExpiresOn: "2026-11-12"}
	lot, err := f.s.CreateLot(ctx, f.sid, f.uid, input)
	if err != nil || lot.ID != input.ID {
		t.Fatalf("client id ignored id=%s err=%v", lot.ID, err)
	}
	path := "/it4788/v1/spaces/" + f.sid + "/lots"
	body, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if w := api(t, f, "POST", path, string(body), f.uid, ""); w.Code != 409 {
		t.Fatalf("duplicate id%d %s", w.Code, w.Body)
	}
	input.ID = "00000000-0000-4000-8000-000000000099"
	body, err = json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if w := api(t, f, "POST", path, string(body), f.uid, ""); w.Code != 400 {
		t.Fatalf("invalid id version%d %s", w.Code, w.Body)
	}
}

func TestShoppingRejectsInvalidClientIDsWithoutMutation(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	list, err := f.s.CreateList(ctx, f.sid, f.uid, ListInput{Name: "Source", Notes: "source notes"})
	if err != nil {
		t.Fatal(err)
	}
	base := "/it4788/v1/spaces/" + f.sid + "/shopping-lists"
	for _, kind := range []string{"list", "item", "copy"} {
		for i, id := range []string{"00000000-0000-4000-8000-000000000099", "00000000-0000-7000-0000-000000000099", "bad-id"} {
			t.Run(fmt.Sprintf("%s/%d", kind, i), func(t *testing.T) {
				path := base
				body := map[string]any{"id": id, "name": "New list"}
				if kind == "copy" {
					path += "/" + list.ID + "/copy"
				}
				if kind == "item" {
					path += "/" + list.ID + "/items"
					body = map[string]any{"id": id, "foodId": f.food, "unitId": f.unit, "quantity": "1"}
				}
				var before, after int
				count := `SELECT (SELECT count(*) FROM shopping.shopping_lists)+(SELECT count(*) FROM shopping.shopping_items)+(SELECT count(*) FROM platform.activities)`
				if err := f.db.Pool.QueryRow(ctx, count).Scan(&before); err != nil {
					t.Fatal(err)
				}
				raw, _ := json.Marshal(body)
				w := api(t, f, "POST", path, string(raw), f.uid, "")
				if w.Code != 400 {
					t.Errorf("invalid resource ID accepted/status%d %s", w.Code, w.Body)
				}
				if err := f.db.Pool.QueryRow(ctx, count).Scan(&after); err != nil {
					t.Fatal(err)
				}
				if after != before {
					t.Fatalf("rejected create changed counts %d→%d", before, after)
				}
			})
		}
	}
}

func TestShoppingItemClientIDCollisionDoesNotMergeQuantity(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	list, err := f.s.CreateList(ctx, f.sid, f.uid, ListInput{Name: "Client IDs"})
	if err != nil {
		t.Fatal(err)
	}
	input := ItemInput{ID: platform.ID(), FoodID: f.food, UnitID: f.unit, Quantity: qty("1")}
	item, err := f.s.AddItem(ctx, f.sid, f.uid, list.ID, input)
	if err != nil || item.ID != input.ID {
		t.Fatalf("client ID lost %+v %v", item, err)
	}
	var before int
	if err := f.db.Pool.QueryRow(ctx, "SELECT count(*) FROM platform.activities").Scan(&before); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(input)
	w := api(t, f, "POST", "/it4788/v1/spaces/"+f.sid+"/shopping-lists/"+list.ID+"/items", string(raw), f.uid, "")
	if w.Code != 409 {
		t.Errorf("duplicate client ID merged instead of rejected%d %s", w.Code, w.Body)
	}
	var amount string
	var version int64
	var activities int
	if err := f.db.Pool.QueryRow(ctx, "SELECT quantity::text,version,(SELECT count(*) FROM platform.activities) FROM shopping.shopping_items WHERE id=$1", item.ID).Scan(&amount, &version, &activities); err != nil {
		t.Fatal(err)
	}
	if qty(amount) != qty("1") || version != 1 || activities != before {
		t.Fatalf("duplicate mutated item quantity%s version%d activity%d→%d", amount, version, before, activities)
	}
}

func TestShoppingCopyHonorsClientIDAndNotesOverride(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	source, err := f.s.CreateList(ctx, f.sid, f.uid, ListInput{Name: "Copy source", Notes: "source notes"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name  string
		notes *string
		want  string
	}{{"inherit", nil, "source notes"}, {"override", pointer("copy notes"), "copy notes"}, {"clear", pointer(""), ""}} {
		t.Run(tt.name, func(t *testing.T) {
			id := platform.ID()
			body := map[string]any{"id": id, "name": "Copied"}
			if tt.notes != nil {
				body["notes"] = *tt.notes
			}
			raw, _ := json.Marshal(body)
			path := "/it4788/v1/spaces/" + f.sid + "/shopping-lists/" + source.ID + "/copy"
			w := api(t, f, "POST", path, string(raw), f.uid, "")
			if w.Code != 201 {
				t.Fatalf("copy%d %s", w.Code, w.Body)
			}
			var out struct {
				Data struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			if out.Data.ID != id {
				t.Errorf("copy ignored client ID got%s want%s", out.Data.ID, id)
			}
			var notes string
			if err := f.db.Pool.QueryRow(ctx, "SELECT notes FROM shopping.shopping_lists WHERE id=$1", out.Data.ID).Scan(&notes); err != nil {
				t.Fatal(err)
			}
			if notes != tt.want {
				t.Errorf("copy notes got%q want%q", notes, tt.want)
			}
			if w := api(t, f, "POST", path, string(raw), f.uid, ""); w.Code != 409 {
				t.Fatalf("duplicate copy ID accepted%d %s", w.Code, w.Body)
			}
		})
	}
}

func pointer(v string) *string { return &v }
