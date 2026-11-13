//go:build integration

package inventory

import (
	"context"
	"encoding/json"
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
