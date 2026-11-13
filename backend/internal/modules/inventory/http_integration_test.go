//go:build integration

package inventory

import (
	"bytes"
	"context"
	"dicho/internal/kernel"
	"dicho/internal/platform"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"net/http"
	"net/http/httptest"
	"testing"
)

func api(t *testing.T, f fixture, method, path, body, uid string, version string) *httptest.ResponseRecorder {
	t.Helper()
	m := http.NewServeMux()
	registrar, ok := any(f.s).(interface{ Register(*http.ServeMux) })
	if !ok {
		t.Fatal("inventory service does not register HTTP API")
	}
	registrar.Register(m)
	r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	r = r.WithContext(platform.WithPrincipal(r.Context(), platform.Principal{UserID: uid}))
	if version != "" {
		r.Header.Set("If-Match", version)
	}
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	return w
}
func TestLotHTTPReadFilterAndMutation(t *testing.T) {
	f := setup(t)
	lot := f.stock(t, "1.25", "2026-11-12")
	base := "/it4788/v1/spaces/" + f.sid + "/lots"
	if w := api(t, f, "GET", base+"?expiryState=expiring", "", f.uid, ""); w.Code != 200 {
		t.Fatalf("list %d %s", w.Code, w.Body.String())
	}
	if w := api(t, f, "GET", base+"/"+lot.ID, "", f.other, ""); w.Code != 404 {
		t.Fatalf("nonmember %d %s", w.Code, w.Body.String())
	}
	if w := api(t, f, "PATCH", base+"/"+lot.ID, `{"remainingQuantity":1}`, f.uid, "99"); w.Code != 409 {
		t.Fatalf("stale patch %d %s", w.Code, w.Body.String())
	}
	if w := api(t, f, "PATCH", base+"/"+lot.ID, `{"remainingQuantity":1}`, f.uid, "1"); w.Code != 200 {
		t.Fatalf("patch %d %s", w.Code, w.Body.String())
	}
	if w := api(t, f, "POST", base+"/"+lot.ID+"/discard", `{"quantity":0.25,"reason":"expired"}`, f.uid, ""); w.Code != 200 {
		t.Fatalf("discard %d %s", w.Code, w.Body.String())
	}
	var rem kernel.Quantity
	if err := f.db.Pool.QueryRow(context.Background(), "SELECT remaining_qty::text FROM pantry.food_lots WHERE id=$1", lot.ID).Scan(&rem); err != nil || rem != qty("0.75") {
		t.Fatalf("remain %s %v", rem, err)
	}
	if w := api(t, f, "POST", base+"/"+lot.ID+"/discard", `{"quantity":0.25}`, f.uid, ""); w.Code != 400 {
		t.Fatalf("missing reason %d", w.Code)
	}
}
func TestShoppingAssignmentAndLeave(t *testing.T) {
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
	base := "/it4788/v1/spaces/" + f.sid + "/shopping-items/" + item.ID
	body, _ := json.Marshal(map[string]string{"userId": f.other})
	if w := api(t, f, "PUT", base+"/assignee", string(body), f.other, "1"); w.Code != 403 {
		t.Fatalf("member assigned %d %s", w.Code, w.Body.String())
	}
	if w := api(t, f, "PUT", base+"/assignee", string(body), f.uid, "1"); w.Code != 200 {
		t.Fatalf("assign %d %s", w.Code, w.Body.String())
	}
	if _, err := f.s.Purchase(ctx, f.sid, f.uid, []PurchaseInput{{ItemID: item.ID}}); err == nil {
		t.Fatal("purchase without onBehalf accepted")
	}
	if err := f.db.Tx(ctx, func(tx pgx.Tx) error { return f.s.OnMemberLeave(ctx, tx, f.sid, f.other) }); err != nil {
		t.Fatal(err)
	}
	var state string
	var assignee *string
	if err := f.db.Pool.QueryRow(ctx, `SELECT status,assignee_id::text FROM shopping.shopping_items WHERE id=$1`, item.ID).Scan(&state, &assignee); err != nil || state != "to_buy" || assignee != nil {
		t.Fatalf("cleanup %s %v %v", state, assignee, err)
	}
}
