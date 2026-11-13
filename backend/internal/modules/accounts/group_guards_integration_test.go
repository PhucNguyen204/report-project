//go:build integration

package accounts

import (
	"context"
	"dicho/internal/platform"
	"github.com/jackc/pgx/v5"
	"testing"
)

func TestGroupMutationGuardsAndFailedMemberCleanupRollBackMembership(t *testing.T) {
	f := setupAuth(t)
	ctx := context.Background()
	a := f.activate(t, "guard-leader@example.com")
	b := f.activate(t, "guard-member@example.com")
	at := a["accessToken"].(string)
	bt := b["accessToken"].(string)
	status, body := f.call(t, "POST", "/it4788/v1/spaces", map[string]any{"name": "Guard family"}, at)
	if status != 201 {
		t.Fatal(body)
	}
	sid := body["data"].(map[string]any)["id"].(string)
	base := "/it4788/v1/spaces/" + sid
	status, _ = f.call(t, "POST", "/it4788/v1/spaces", map[string]any{"name": "Second family"}, at)
	if status != 422 {
		t.Fatal(status)
	}
	status, _ = f.call(t, "PATCH", base, map[string]any{"name": "x"}, at)
	if status != 400 {
		t.Fatal(status)
	}
	avatar := platform.ID()
	status, _ = f.call(t, "PATCH", base, map[string]any{"avatarObjectId": avatar}, at)
	if status != 422 {
		t.Fatal(status)
	}
	f.s.ValidateMedia = func(ctx context.Context, tx pgx.Tx, user, purpose, owner, obj string) error {
		if user != a["userId"] || purpose != "space" || owner != sid || obj != avatar {
			return platform.Forbidden()
		}
		return nil
	}
	status, body = f.call(t, "PATCH", base, map[string]any{"avatarObjectId": avatar}, at)
	if status != 200 {
		t.Fatal(body)
	}
	status, body = f.call(t, "POST", base+"/invites", map[string]any{}, at)
	if status != 201 {
		t.Fatal(body)
	}
	code := body["data"].(map[string]any)["code"]
	status, _ = f.call(t, "POST", "/it4788/v1/spaces/join", map[string]any{"code": code}, at)
	if status != 422 {
		t.Fatal(status)
	}
	status, body = f.call(t, "POST", "/it4788/v1/spaces/join", map[string]any{"code": code}, bt)
	if status != 200 {
		t.Fatal(body)
	}
	for _, target := range []string{"", a["userId"].(string), platform.ID()} {
		status, _ = f.call(t, "POST", base+"/leader-transfer", map[string]any{"userId": target}, at)
		if status != 400 && status != 404 {
			t.Fatalf("bad leader target %q status%d", target, status)
		}
	}
	status, _ = f.call(t, "POST", base+"/leader-transfer", map[string]any{"userId": a["userId"]}, bt)
	if status != 403 {
		t.Fatal(status)
	}
	f.db.MemberLeave = func(context.Context, pgx.Tx, string, string) error { return platform.Rule("cleanup rejected") }
	status, _ = f.call(t, "DELETE", base+"/members/"+b["userId"].(string), nil, at)
	if status != 422 {
		t.Fatal(status)
	}
	status, _ = f.call(t, "POST", base+"/leave", map[string]any{}, bt)
	if status != 422 {
		t.Fatal(status)
	}
	var members int
	var stored *string
	if err := f.db.Pool.QueryRow(ctx, `SELECT count(*) FROM household.space_members WHERE space_id=$1`, sid).Scan(&members); err != nil || members != 2 {
		t.Fatalf("failed cleanup lost membership %d %v", members, err)
	}
	if err := f.db.Pool.QueryRow(ctx, `SELECT avatar_object_id::text FROM household.spaces WHERE id=$1`, sid).Scan(&stored); err != nil || stored == nil || *stored != avatar {
		t.Fatalf("group avatar %v %v", stored, err)
	}
}

func TestCatalogUnitDimensionAndPrivateFoodValidationProtectExistingReferences(t *testing.T) {
	f := setupAuth(t)
	ctx := context.Background()
	u := f.activate(t, "private-validation@example.com")
	admin := makeAdmin(t, f, "unit-validation-admin@example.com")
	status, _ := f.call(t, "PUT", "/it4788/v1/admin/units/00000000-0000-7000-8000-000000000011", map[string]any{"name": "g", "dimension": "volume", "factorToBase": 1}, admin)
	if status != 422 {
		t.Fatal(status)
	}
	status, body := f.call(t, "POST", "/it4788/v1/admin/units", map[string]any{"name": "unused weight", "dimension": "mass", "factorToBase": 5}, admin)
	if status != 201 {
		t.Fatal(body)
	}
	id := body["data"].(map[string]any)["id"].(string)
	status, body = f.call(t, "PUT", "/it4788/v1/admin/units/"+id, map[string]any{"name": "unused count", "dimension": "count", "factorToBase": 5}, admin)
	if status != 200 {
		t.Fatal(body)
	}
	status, body = f.call(t, "DELETE", "/it4788/v1/admin/units/"+id, nil, admin)
	if status != 200 {
		t.Fatal(body)
	}
	status, body = f.call(t, "POST", "/it4788/v1/admin/food-categories", map[string]any{"name": "Inactive category", "active": false}, admin)
	if status != 201 {
		t.Fatal(body)
	}
	cat := body["data"].(map[string]any)["id"].(string)
	for _, in := range []struct{ name, cat string }{{"", "00000000-0000-7000-8000-000000000001"}, {"unknown category", platform.ID()}, {"inactive", cat}} {
		if err := f.db.Tx(ctx, func(tx pgx.Tx) error {
			_, err := f.s.CreatePrivateFood(ctx, tx, u["spaceId"].(string), in.name, in.cat, "00000000-0000-7000-8000-000000000011")
			return err
		}); err == nil {
			t.Fatalf("private food accepted %+v", in)
		}
	}
	status, body = f.call(t, "DELETE", "/it4788/v1/admin/food-categories/"+cat, nil, admin)
	if status != 200 {
		t.Fatal(body)
	}
}

func TestAdminCannotUseFamilyMutationAPIsEvenWithFormerMembership(t *testing.T) {
	f := setupAuth(t)
	u := f.activate(t, "former-member-admin@example.com")
	token := u["accessToken"].(string)
	status, body := f.call(t, "POST", "/it4788/v1/spaces", map[string]any{"name": "Former family"}, token)
	if status != 201 {
		t.Fatal(body)
	}
	sid := body["data"].(map[string]any)["id"].(string)
	admin := promoteAdmin(t, f, u, "former-member-admin@example.com")
	base := "/spaces/" + sid
	for _, route := range []struct{ method, path string }{
		{"POST", "/spaces"}, {"PATCH", base}, {"POST", base + "/invites"}, {"DELETE", base + "/invites/current"}, {"POST", "/spaces/join"}, {"POST", base + "/leave"}, {"DELETE", base + "/members/" + platform.ID()}, {"POST", base + "/leader-transfer"},
	} {
		status, _ = f.call(t, route.method, "/it4788/v1"+route.path, map[string]any{}, admin)
		if status != 404 {
			t.Fatalf("admin family mutation %s %s got%d", route.method, route.path, status)
		}
	}
	var members int
	if err := f.db.Pool.QueryRow(context.Background(), `SELECT count(*) FROM household.space_members WHERE space_id=$1`, sid).Scan(&members); err != nil || members != 1 {
		t.Fatalf("denied admin mutations changed family %d %v", members, err)
	}
}
