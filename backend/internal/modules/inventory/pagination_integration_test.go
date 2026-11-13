//go:build integration

package inventory

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"testing"
)

type inventoryPage struct {
	Data map[string]json.RawMessage `json:"data"`
	Meta struct {
		NextCursor string `json:"nextCursor"`
	} `json:"meta"`
}

func readInventoryPage(t *testing.T, w *httptest.ResponseRecorder, field string) (inventoryPage, []map[string]any) {
	t.Helper()
	var body inventoryPage
	if w.Code != 200 {
		t.Fatalf("page status%d %s", w.Code, w.Body)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	var items []map[string]any
	if err := json.Unmarshal(body.Data[field], &items); err != nil {
		t.Fatal(err)
	}
	return body, items
}
func TestInventoryKeysetsDefaultToTwentyAndDoNotRepeatAfterNewInsert(t *testing.T) {
	for _, kind := range []string{"lots", "shopping-lists"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t)
			ctx := context.Background()
			field := "lots"
			for i := range 21 {
				if kind == "lots" {
					f.stock(t, "1", "2026-11-12")
				} else {
					field = "lists"
					if _, err := f.s.CreateList(ctx, f.sid, f.uid, ListInput{Name: fmt.Sprintf("List%02d", i), PlannedDate: "2026-11-09"}); err != nil {
						t.Fatal(err)
					}
				}
			}
			path := "/it4788/v1/spaces/" + f.sid + "/" + kind
			page, items := readInventoryPage(t, api(t, f, "GET", path, "", f.uid, ""), field)
			if len(items) != 20 || page.Meta.NextCursor == "" {
				t.Fatalf("default page length%d cursor%q", len(items), page.Meta.NextCursor)
			}
			if kind == "lots" {
				var facets map[string]map[string]int
				if err := json.Unmarshal(page.Data["facets"], &facets); err != nil || facets["location"]["chill"] != 21 {
					t.Fatalf("facets must cover all pages: %+v %v", facets, err)
				}
			}
			seen := map[string]bool{}
			for _, item := range items {
				seen[item["id"].(string)] = true
			}
			if kind == "lots" {
				_, err := f.s.CreateLot(ctx, f.sid, f.uid, LotInput{ID: "00000000-0000-7000-8000-000000000099", FoodID: f.food, UnitID: f.unit, Quantity: qty("1"), StorageLocation: "chill", PurchasedOn: "2026-11-09", ExpiresOn: "2026-11-12"})
				if err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := f.s.CreateList(ctx, f.sid, f.uid, ListInput{Name: "Inserted before cursor", PlannedDate: "2026-11-10"}); err != nil {
					t.Fatal(err)
				}
			}
			second, remaining := readInventoryPage(t, api(t, f, "GET", path+"?limit=7&cursor="+url.QueryEscape(page.Meta.NextCursor), "", f.uid, ""), field)
			if len(remaining) != 1 || second.Meta.NextCursor != "" || seen[remaining[0]["id"].(string)] {
				t.Fatalf("second page%+v cursor%q", remaining, second.Meta.NextCursor)
			}
			if kind == "lots" {
				var facets map[string]map[string]int
				if err := json.Unmarshal(second.Data["facets"], &facets); err != nil || facets["location"]["chill"] != 22 {
					t.Fatalf("cursor changed full-set facets: %+v %v", facets, err)
				}
			}
			for _, query := range []string{"limit=101", "cursor=bad-cursor", "q=changed&cursor=" + url.QueryEscape(page.Meta.NextCursor)} {
				if w := api(t, f, "GET", path+"?"+query, "", f.uid, ""); w.Code != 400 {
					t.Fatalf("bad pagination%d %s", w.Code, w.Body)
				}
			}
			if w := api(t, f, "GET", path, "", f.other, ""); w.Code != 404 {
				t.Fatalf("scope bypass%d", w.Code)
			}
		})
	}
}

func TestShoppingHistoryTotalsCoverTheWholeFilterAndRoundEachItem(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if _, err := f.db.Pool.Exec(ctx, `INSERT INTO household.space_members(space_id,user_id,role,space_kind) VALUES($1,$2,'member','family')`, f.sid, f.other); err != nil {
		t.Fatal(err)
	}
	first, err := f.s.CreateList(ctx, f.sid, f.uid, ListInput{Name: "First", PlannedDate: "2026-11-09"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := f.s.AddItem(ctx, f.sid, f.uid, first.ID, ItemInput{FoodID: f.food, UnitID: f.unit, Quantity: qty("2")})
	if err != nil {
		t.Fatal(err)
	}
	b, err := f.s.AddItem(ctx, f.sid, f.uid, first.ID, ItemInput{Name: "Milk", UnitID: f.unit, Quantity: qty("0.5")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.AddItem(ctx, f.sid, f.uid, first.ID, ItemInput{Name: "Still pending", UnitID: f.unit, Quantity: qty("1")}); err != nil {
		t.Fatal(err)
	}
	p1, p2 := int64(100), int64(301)
	if _, err := f.s.Purchase(ctx, f.sid, f.other, []PurchaseInput{{ItemID: a.ID, UnitPrice: &p1}, {ItemID: b.ID, UnitPrice: &p2}}); err != nil {
		t.Fatal(err)
	}
	second, err := f.s.CreateList(ctx, f.sid, f.uid, ListInput{Name: "Second", PlannedDate: "2026-11-10"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := f.s.AddItem(ctx, f.sid, f.uid, second.ID, ItemInput{FoodID: f.food, UnitID: f.unit, Quantity: qty("1")})
	if err != nil {
		t.Fatal(err)
	}
	p3 := int64(1000)
	if _, err := f.s.Purchase(ctx, f.sid, f.other, []PurchaseInput{{ItemID: c.ID, UnitPrice: &p3}}); err != nil {
		t.Fatal(err)
	}
	path := "/it4788/v1/spaces/" + f.sid + "/shopping-lists"
	page, items := readInventoryPage(t, api(t, f, "GET", path+"?groupBy=month&limit=1", "", f.uid, ""), "lists")
	var months []struct {
		Month     string
		TotalCost int64
		ListCount int
	}
	if err := json.Unmarshal(page.Data["months"], &months); err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || len(months) != 1 || months[0].Month != "2026-11" || months[0].TotalCost != 1351 || months[0].ListCount != 2 {
		t.Fatalf("monthly totals%+v items%+v", months, items)
	}
	_, items = readInventoryPage(t, api(t, f, "GET", path+"?status=shopping", "", f.uid, ""), "lists")
	if len(items) != 1 || items[0]["totalCost"] != float64(351) || items[0]["itemCount"] != float64(3) {
		t.Fatalf("history summary%+v", items)
	}
	buyers, ok := items[0]["purchasedBy"].([]any)
	if !ok || len(buyers) != 1 || buyers[0] != f.other {
		t.Fatalf("buyers%+v", items[0]["purchasedBy"])
	}
}

func TestShoppingHistoryRejectsListAndMonthlyCostOverflow(t *testing.T) {
	for _, monthly := range []bool{false, true} {
		t.Run(fmt.Sprint(monthly), func(t *testing.T) {
			f := setup(t)
			ctx := context.Background()
			price := int64(9223372036854775807)
			listID := ""
			inputs := []PurchaseInput{}
			for i := range 2 {
				if i == 0 || monthly {
					list, err := f.s.CreateList(ctx, f.sid, f.uid, ListInput{Name: fmt.Sprintf("Large%02d", i), PlannedDate: "2026-11-09"})
					if err != nil {
						t.Fatal(err)
					}
					listID = list.ID
				}
				item, err := f.s.AddItem(ctx, f.sid, f.uid, listID, ItemInput{Name: fmt.Sprintf("Large item%02d", i), UnitID: f.unit, Quantity: qty("1")})
				if err != nil {
					t.Fatal(err)
				}
				inputs = append(inputs, PurchaseInput{ItemID: item.ID, UnitPrice: &price})
			}
			if _, err := f.s.Purchase(ctx, f.sid, f.uid, inputs); err != nil {
				t.Fatal(err)
			}
			path := "/it4788/v1/spaces/" + f.sid + "/shopping-lists"
			if monthly {
				path += "?groupBy=month&limit=1"
			}
			if w := api(t, f, "GET", path, "", f.uid, ""); w.Code != 400 {
				t.Fatalf("overflow status%d %s", w.Code, w.Body)
			}
		})
	}
}
