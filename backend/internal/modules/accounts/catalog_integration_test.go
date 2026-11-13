//go:build integration

package accounts

import (
	"context"
	"dicho/internal/platform"
	"github.com/jackc/pgx/v5"
	"testing"
)

func makeAdmin(t *testing.T, f *authFixture, email string) string {
	t.Helper()
	data := f.activate(t, email)
	_, err := f.db.Pool.Exec(context.Background(), "UPDATE identity.users SET system_role='admin',token_version=token_version+1 WHERE id=$1", data["userId"])
	if err != nil {
		t.Fatal(err)
	}
	status, body := f.call(t, "POST", "/it4788/v1/auth/login", map[string]any{"email": email, "password": "ValidPass1", "deviceId": platform.ID(), "platform": "android"}, "")
	if status != 200 {
		t.Fatalf("admin login %d %+v", status, body)
	}
	return body["data"].(map[string]any)["accessToken"].(string)
}
func TestCatalogScopesPrivateFoodsAndRejectsForeignUnits(t *testing.T) {
	f := setupAuth(t)
	a := f.activate(t, "a@example.com")
	b := f.activate(t, "b@example.com")
	var food platform.FoodUnit
	err := f.db.Tx(context.Background(), func(tx pgx.Tx) error {
		var err error
		food, err = f.s.CreatePrivateFood(context.Background(), tx, a["spaceId"].(string), "Rau riêng", "00000000-0000-7000-8000-000000000001", "00000000-0000-7000-8000-000000000011")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	err = f.db.Tx(context.Background(), func(tx pgx.Tx) error {
		_, err := f.s.ValidateFood(context.Background(), tx, b["spaceId"].(string), food.FoodID, food.UnitID)
		return err
	})
	if err == nil {
		t.Fatal("private food leaked to other space")
	}
	err = f.db.Tx(context.Background(), func(tx pgx.Tx) error {
		unit, err := f.s.ValidateFood(context.Background(), tx, a["spaceId"].(string), food.FoodID, food.UnitID)
		if err == nil && (unit.Dimension != "mass" || unit.Factor.String() != "1") {
			t.Fatalf("wrong unit %+v", unit)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	status, body := f.call(t, "GET", "/it4788/v1/catalog", nil, a["accessToken"].(string))
	if status != 200 || len(body["data"].(map[string]any)["units"].([]any)) != 6 {
		t.Fatalf("catalog %d %+v", status, body)
	}
}
func TestCatalogAdminRestrictionsAndConfigAtomicity(t *testing.T) {
	f := setupAuth(t)
	user := f.activate(t, "a@example.com")
	admin := makeAdmin(t, f, "admin@example.com")
	status, _ := f.call(t, "POST", "/it4788/v1/admin/food-categories", map[string]any{"name": "Test"}, user["accessToken"].(string))
	if status != 403 {
		t.Fatalf("user admin API %d", status)
	}
	status, body := f.call(t, "POST", "/it4788/v1/admin/food-categories", map[string]any{"name": "Gia vị"}, admin)
	if status != 201 {
		t.Fatalf("create category %d %+v", status, body)
	}
	status, body = f.call(t, "PUT", "/it4788/v1/admin/configs", map[string]any{"values": map[string]any{"reminder.defaultDays": 4, "suggest.weights": map[string]any{"r": 0.9, "e": 0.3, "f": 0.1}}}, admin)
	if status != 400 {
		t.Fatalf("invalid config %d %+v", status, body)
	}
	var days int
	f.db.Pool.QueryRow(context.Background(), "SELECT (value #>> '{}')::int FROM admin.system_configs WHERE key='reminder.defaultDays'").Scan(&days)
	if days != 3 {
		t.Fatal("partial config write")
	}
	status, body = f.call(t, "PUT", "/it4788/v1/admin/configs", map[string]any{"values": map[string]any{"reminder.defaultDays": 4}}, admin)
	if status != 200 {
		t.Fatalf("config %d %+v", status, body)
	}
	var count int
	f.db.Pool.QueryRow(context.Background(), "SELECT count(*) FROM admin.audit_logs WHERE action='config.update'").Scan(&count)
	if count != 1 {
		t.Fatalf("audit %d", count)
	}
	status, _ = f.call(t, "DELETE", "/it4788/v1/admin/food-categories/00000000-0000-7000-8000-000000000001", nil, admin)
	if status != 422 {
		t.Fatalf("deleted referenced category: %d", status)
	}
}
