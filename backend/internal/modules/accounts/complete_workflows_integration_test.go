//go:build integration

package accounts

import (
	"context"
	"dicho/internal/platform"
	"github.com/jackc/pgx/v5"
	"testing"
)

func TestGroupReadUpdateRemoveAndRevocationEnforceMembership(t *testing.T) {
	f := setupAuth(t)
	ctx := context.Background()
	a := f.activate(t, "group-read-leader@example.com")
	b := f.activate(t, "group-read-member@example.com")
	at := a["accessToken"].(string)
	bt := b["accessToken"].(string)
	status, body := f.call(t, "POST", "/it4788/v1/spaces", map[string]any{"name": "Home group", "copyPersonalData": map[string]bool{"lists": true, "lots": true, "recipes": true}}, at)
	if status != 201 {
		t.Fatal(body)
	}
	sid := body["data"].(map[string]any)["id"].(string)
	base := "/it4788/v1/spaces/" + sid
	var source string
	if err := f.db.Pool.QueryRow(ctx, `SELECT args->'payload'->>'sourceSpaceId' FROM river_job WHERE args->>'event'='space.copy'`).Scan(&source); err != nil || source != a["spaceId"] {
		t.Fatalf("copy source %q %v", source, err)
	}
	status, body = f.call(t, "GET", "/it4788/v1/spaces", nil, at)
	if status != 200 || len(body["data"].(map[string]any)["items"].([]any)) != 2 {
		t.Fatal(body)
	}
	status, _ = f.call(t, "GET", base, nil, bt)
	if status != 404 {
		t.Fatalf("outsider detail %d", status)
	}
	status, body = f.call(t, "POST", base+"/invites", map[string]any{}, at)
	if status != 201 {
		t.Fatal(body)
	}
	code := body["data"].(map[string]any)["code"]
	status, body = f.call(t, "POST", "/it4788/v1/spaces/join", map[string]any{"code": code}, bt)
	if status != 200 {
		t.Fatal(body)
	}
	status, _ = f.call(t, "PATCH", base, map[string]any{"name": "Denied name"}, bt)
	if status != 403 {
		t.Fatal(status)
	}
	status, body = f.call(t, "PATCH", base, map[string]any{"name": " Updated Home ", "avatarObjectId": ""}, at)
	if status != 200 {
		t.Fatal(body)
	}
	for i := 0; i < 25; i++ {
		if err := f.db.Tx(ctx, func(tx pgx.Tx) error {
			return f.db.Activity(ctx, tx, sid, a["userId"].(string), "test.event", platform.ID(), map[string]any{})
		}); err != nil {
			t.Fatal(err)
		}
	}
	status, body = f.call(t, "GET", base, nil, at)
	if status != 200 {
		t.Fatal(body)
	}
	data := body["data"].(map[string]any)
	if data["name"] != "Updated Home" || len(data["members"].([]any)) != 2 || len(data["recentActivity"].([]any)) != 20 {
		t.Fatal(body)
	}
	status, _ = f.call(t, "DELETE", base+"/invites/current", nil, bt)
	if status != 403 {
		t.Fatal(status)
	}
	status, body = f.call(t, "DELETE", base+"/invites/current", nil, at)
	if status != 200 {
		t.Fatal(body)
	}
	status, _ = f.call(t, "DELETE", base+"/members/"+a["userId"].(string), nil, at)
	if status != 422 {
		t.Fatal(status)
	}
	status, _ = f.call(t, "DELETE", base+"/members/"+a["userId"].(string), nil, bt)
	if status != 403 {
		t.Fatal(status)
	}
	status, body = f.call(t, "DELETE", base+"/members/"+b["userId"].(string), nil, at)
	if status != 200 {
		t.Fatal(body)
	}
	status, _ = f.call(t, "GET", base, nil, bt)
	if status != 404 {
		t.Fatalf("removed member sees family %d", status)
	}
	status, _ = f.call(t, "DELETE", base+"/members/"+b["userId"].(string), nil, at)
	if status != 404 {
		t.Fatal(status)
	}
	admin := makeAdmin(t, f, "group-hidden-admin@example.com")
	for _, path := range []string{"/it4788/v1/spaces", base} {
		status, _ = f.call(t, "GET", path, nil, admin)
		if status != 404 {
			t.Fatalf("admin family access %d", status)
		}
	}
}

func TestDeviceRegistrationCannotOverwriteOrDeleteAnotherUsersDevice(t *testing.T) {
	f := setupAuth(t)
	a := f.activate(t, "device-a@example.com")
	b := f.activate(t, "device-b@example.com")
	at := a["accessToken"].(string)
	bt := b["accessToken"].(string)
	status, body := f.call(t, "POST", "/it4788/v1/me/devices", map[string]any{"fcmToken": "token-a", "platform": "android"}, at)
	if status != 200 {
		t.Fatal(body)
	}
	id := body["data"].(map[string]any)["deviceId"].(string)
	status, _ = f.call(t, "POST", "/it4788/v1/me/devices", map[string]any{"deviceId": id, "fcmToken": "stolen", "platform": "ios"}, bt)
	if status != 404 {
		t.Fatal(status)
	}
	status, _ = f.call(t, "DELETE", "/it4788/v1/me/devices/"+id, nil, bt)
	if status != 404 {
		t.Fatal(status)
	}
	status, body = f.call(t, "POST", "/it4788/v1/me/devices", map[string]any{"deviceId": id, "fcmToken": "token-a-new", "platform": "ios"}, at)
	if status != 200 {
		t.Fatal(body)
	}
	var token string
	if err := f.db.Pool.QueryRow(context.Background(), `SELECT fcm_token FROM identity.devices WHERE id=$1`, id).Scan(&token); err != nil || token != "token-a-new" {
		t.Fatalf("device unchanged %s %v", token, err)
	}
	status, _ = f.call(t, "POST", "/it4788/v1/me/devices", map[string]any{"fcmToken": "", "platform": "web"}, at)
	if status != 400 {
		t.Fatal(status)
	}
	status, body = f.call(t, "DELETE", "/it4788/v1/me/devices/"+id, nil, at)
	if status != 200 {
		t.Fatal(body)
	}
}

func TestAdminCatalogCRUDReviewAndAuditMaintainScope(t *testing.T) {
	f := setupAuth(t)
	ctx := context.Background()
	u := f.activate(t, "catalog-owner@example.com")
	token := makeAdmin(t, f, "catalog-crud-admin@example.com")
	create := func(kind string, in map[string]any) string {
		t.Helper()
		status, body := f.call(t, "POST", "/it4788/v1/admin/"+kind, in, token)
		if status != 201 {
			t.Fatalf("create %s: %d %+v", kind, status, body)
		}
		return body["data"].(map[string]any)["id"].(string)
	}
	cat := create("food-categories", map[string]any{"name": "Temporary category"})
	status, body := f.call(t, "PUT", "/it4788/v1/admin/food-categories/"+cat, map[string]any{"name": "Changed category", "active": true}, token)
	if status != 200 {
		t.Fatal(body)
	}
	unit := create("units", map[string]any{"name": "Temporary weight", "dimension": "mass", "factorToBase": 2, "isBase": false})
	status, body = f.call(t, "PUT", "/it4788/v1/admin/units/"+unit, map[string]any{"name": "Changed weight", "dimension": "mass", "factorToBase": 3, "isBase": false}, token)
	if status != 200 {
		t.Fatal(body)
	}
	food := create("foods", map[string]any{"name": "Temporary food", "categoryId": cat, "defaultUnitId": unit, "shelfLives": map[string]int{"chill": 5}})
	status, body = f.call(t, "PUT", "/it4788/v1/admin/foods/"+food, map[string]any{"name": "Changed food", "categoryId": cat, "defaultUnitId": unit, "status": "active", "defaultLocation": "pantry_shelf", "shelfLives": map[string]int{"pantry_shelf": 10}}, token)
	if status != 200 {
		t.Fatal(body)
	}
	for _, kind := range []string{"food-categories", "units", "foods"} {
		status, body = f.call(t, "GET", "/it4788/v1/admin/"+kind, nil, token)
		if status != 200 || len(body["data"].(map[string]any)["items"].([]any)) == 0 {
			t.Fatal(body)
		}
	}
	status, body = f.call(t, "DELETE", "/it4788/v1/admin/foods/"+food, nil, token)
	if status != 200 {
		t.Fatal(body)
	}
	var state string
	if err := f.db.Pool.QueryRow(ctx, `SELECT status FROM catalog.foods WHERE id=$1`, food).Scan(&state); err != nil || state != "inactive" {
		t.Fatalf("food delete %s %v", state, err)
	}
	var private platform.FoodUnit
	if err := f.db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		private, err = f.s.CreatePrivateFood(ctx, tx, u["spaceId"].(string), "Promoted herb", "00000000-0000-7000-8000-000000000001", "00000000-0000-7000-8000-000000000011")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	status, body = f.call(t, "POST", "/it4788/v1/admin/foods/"+private.FoodID+"/review", map[string]any{"action": "approve"}, token)
	if status != 200 {
		t.Fatal(body)
	}
	var global bool
	if err := f.db.Pool.QueryRow(ctx, `SELECT scope='global' AND owner_space_id IS NULL FROM catalog.foods WHERE id=$1`, private.FoodID).Scan(&global); err != nil || !global {
		t.Fatalf("approve %v %v", global, err)
	}
	status, _ = f.call(t, "POST", "/it4788/v1/admin/foods/"+private.FoodID+"/review", map[string]any{"action": "approve"}, token)
	if status != 409 {
		t.Fatal(status)
	}
	for _, path := range []string{"configs", "audit-logs", "users?q=catalog-owner"} {
		status, body = f.call(t, "GET", "/it4788/v1/admin/"+path, nil, token)
		if status != 200 || len(body["data"].(map[string]any)["items"].([]any)) == 0 {
			t.Fatal(body)
		}
	}
	status, body = f.call(t, "GET", "/it4788/v1/admin/users?q=not-found", nil, token)
	if status != 200 || len(body["data"].(map[string]any)["items"].([]any)) != 0 {
		t.Fatal(body)
	}
}
