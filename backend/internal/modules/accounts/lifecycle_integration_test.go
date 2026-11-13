//go:build integration

package accounts

import (
	"context"
	"dicho/internal/platform"
	"github.com/jackc/pgx/v5"
	"testing"
)

type accountLifecycle interface {
	ClonePrivateFoods(context.Context, pgx.Tx, string, string) (map[string]string, error)
	PurgeSpace(context.Context, pgx.Tx, string) error
}

func TestClonePrivateFoodsPreservesShelfLifeAndPurgesOnlyDissolvedSpace(t *testing.T) {
	f := setupAuth(t)
	ctx := context.Background()
	u := f.activate(t, "copy-food@example.com")
	source := u["spaceId"].(string)
	status, body := f.call(t, "POST", "/it4788/v1/spaces", map[string]any{"name": "Copy target"}, u["accessToken"].(string))
	if status != 201 {
		t.Fatal(body)
	}
	target := body["data"].(map[string]any)["id"].(string)
	port, ok := any(f.s).(accountLifecycle)
	if !ok {
		t.Fatal("account lifecycle ports missing")
	}
	food := platform.ID()
	if _, err := f.db.Pool.Exec(ctx, `INSERT INTO catalog.foods(id,name,category_id,scope,owner_space_id,default_unit_id,status,default_location) VALUES($1,'Private herb','00000000-0000-7000-8000-000000000001','space',$2,'00000000-0000-7000-8000-000000000011','pending_review','crisper')`, food, source); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool.Exec(ctx, `INSERT INTO catalog.food_shelf_lives VALUES($1,'crisper',9)`, food); err != nil {
		t.Fatal(err)
	}
	var mapped string
	if err := f.db.Tx(ctx, func(tx pgx.Tx) error {
		m, err := port.ClonePrivateFoods(ctx, tx, source, target)
		mapped = m[food]
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if mapped == "" || mapped == food {
		t.Fatalf("private food ownership was not cloned: %q", mapped)
	}
	var days int
	var owner, loc, statusName string
	if err := f.db.Pool.QueryRow(ctx, `SELECT owner_space_id::text,default_location,status,days FROM catalog.foods JOIN catalog.food_shelf_lives ON food_id=id WHERE id=$1`, mapped).Scan(&owner, &loc, &statusName, &days); err != nil || owner != target || loc != "crisper" || statusName != "pending_review" || days != 9 {
		t.Fatalf("copy %s %s %s %d %v", owner, loc, statusName, days, err)
	}
	if err := f.db.Tx(ctx, func(tx pgx.Tx) error { return port.PurgeSpace(ctx, tx, target) }); err == nil {
		t.Fatal("active space purged")
	}
	if _, err := f.db.Pool.Exec(ctx, `UPDATE household.spaces SET dissolved_at=now() WHERE id=$1`, target); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := f.db.Tx(ctx, func(tx pgx.Tx) error { return port.PurgeSpace(ctx, tx, target) }); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err := f.db.Pool.QueryRow(ctx, `SELECT count(*) FROM catalog.foods WHERE id=$1`, food).Scan(&n); err != nil || n != 1 {
		t.Fatalf("source food lost %d %v", n, err)
	}
}
