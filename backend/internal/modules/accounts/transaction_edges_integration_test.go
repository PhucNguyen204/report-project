//go:build integration

package accounts

import (
	"context"
	"dicho/internal/platform"
	"github.com/jackc/pgx/v5"
	"testing"
)

func TestActivationRollsBackAllStateWhenDeviceIdentifierIsInvalid(t *testing.T) {
	f := setupAuth(t)
	ctx := context.Background()
	email := "activation-rollback@example.com"
	status, body := f.call(t, "POST", "/it4788/v1/auth/register", map[string]any{"fullName": "Nguyễn An", "email": email, "password": "ValidPass1"}, "")
	if status != 201 {
		t.Fatal(body)
	}
	code := f.otp(t, email)
	status, _ = f.call(t, "POST", "/it4788/v1/auth/otp/verify", map[string]any{"email": email, "code": code, "deviceId": "invalid-uuid"}, "")
	if status != 422 {
		t.Fatal(status)
	}
	var state string
	var spaces int
	var used bool
	if err := f.db.Pool.QueryRow(ctx, `SELECT status FROM identity.users WHERE email=$1`, email).Scan(&state); err != nil || state != "pending" {
		t.Fatalf("activation partially committed %s %v", state, err)
	}
	if err := f.db.Pool.QueryRow(ctx, `SELECT count(*) FROM household.spaces`).Scan(&spaces); err != nil || spaces != 0 {
		t.Fatalf("orphan spaces %d %v", spaces, err)
	}
	if err := f.db.Pool.QueryRow(ctx, `SELECT used_at IS NOT NULL FROM identity.otp_challenges`).Scan(&used); err != nil || used {
		t.Fatalf("OTP consumed on failed activation %v %v", used, err)
	}
	status, body = f.call(t, "POST", "/it4788/v1/auth/otp/verify", map[string]any{"email": email, "code": code}, "")
	if status != 200 {
		t.Fatal(body)
	}
}

func TestCatalogLifecycleErrorsRollBackRatherThanDeletingReferencedFoods(t *testing.T) {
	f := setupAuth(t)
	ctx := context.Background()
	u := f.activate(t, "purge-order@example.com")
	sid := u["spaceId"].(string)
	port := any(f.s).(accountLifecycle)
	if err := f.db.Tx(ctx, func(tx pgx.Tx) error { _, err := port.ClonePrivateFoods(ctx, tx, sid, sid); return err }); err == nil {
		t.Fatal("same-space clone accepted")
	}
	var food platform.FoodUnit
	if err := f.db.Tx(ctx, func(tx pgx.Tx) error {
		var err error
		food, err = f.s.CreatePrivateFood(ctx, tx, sid, "Referenced private", "00000000-0000-7000-8000-000000000001", "00000000-0000-7000-8000-000000000011")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Tx(ctx, func(tx pgx.Tx) error { _, err := port.ClonePrivateFoods(ctx, tx, sid, platform.ID()); return err }); err == nil {
		t.Fatal("missing target accepted")
	}
	lot := platform.ID()
	if _, err := f.db.Pool.Exec(ctx, `INSERT INTO pantry.food_lots(id,space_id,food_id,unit_id,unit_factor,dimension,initial_qty,remaining_qty,base_remaining,purchased_on,expires_on,storage_location) VALUES($1,$2,$3,$4,1,'mass',1,1,1,'2026-11-09','2026-11-12','chill')`, lot, sid, food.FoodID, food.UnitID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool.Exec(ctx, `UPDATE household.spaces SET dissolved_at=now() WHERE id=$1`, sid); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Tx(ctx, func(tx pgx.Tx) error { return port.PurgeSpace(ctx, tx, sid) }); err == nil {
		t.Fatal("purge succeeded before inventory cleanup")
	}
	var foods, members int
	if err := f.db.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM catalog.foods WHERE owner_space_id=$1),(SELECT count(*) FROM household.space_members WHERE space_id=$1)`, sid).Scan(&foods, &members); err != nil || foods != 1 || members != 1 {
		t.Fatalf("failed purge partially committed foods%d members%d %v", foods, members, err)
	}
}

func TestInvalidUUIDsReturnClientErrorsAndLeaveCatalogUnchanged(t *testing.T) {
	f := setupAuth(t)
	u := f.activate(t, "invalid-id@example.com")
	token := u["accessToken"].(string)
	admin := makeAdmin(t, f, "invalid-id-admin@example.com")
	for _, route := range []struct {
		method, path string
		input        any
		token        string
	}{
		{"POST", "/me/devices", map[string]any{"deviceId": "bad", "fcmToken": "valid-token", "platform": "ios"}, token},
		{"PATCH", "/admin/users/bad", map[string]any{"status": "active"}, admin},
		{"PUT", "/admin/food-categories/bad", map[string]any{"name": "Valid name"}, admin},
		{"POST", "/admin/foods/bad/review", map[string]any{"action": "approve"}, admin},
		{"PATCH", "/spaces/bad", map[string]any{"name": "Valid group"}, token},
		{"POST", "/spaces/bad/invites", map[string]any{}, token},
		{"DELETE", "/spaces/bad/invites/current", nil, token},
		{"POST", "/spaces/bad/leave", map[string]any{}, token},
		{"DELETE", "/spaces/bad/members/" + u["userId"].(string) + "0", nil, token},
	} {
		status, body := f.call(t, route.method, "/it4788/v1"+route.path, route.input, route.token)
		if status != 422 {
			t.Fatalf("%s %s status%d %+v", route.method, route.path, status, body)
		}
	}
	var categories int
	if err := f.db.Pool.QueryRow(context.Background(), `SELECT count(*) FROM catalog.food_categories`).Scan(&categories); err != nil || categories != 2 {
		t.Fatalf("invalid writes changed catalog %d %v", categories, err)
	}
}
