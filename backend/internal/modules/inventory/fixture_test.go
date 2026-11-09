//go:build integration

package inventory

import (
	"context"
	"dicho/internal/platform"
	"dicho/internal/testutil"
	"github.com/jackc/pgx/v5"
	"testing"
	"time"
)

type dbCatalog struct{ db *platform.DB }

func (c dbCatalog) ValidateFood(ctx context.Context, tx pgx.Tx, sid, fid, unit string) (platform.FoodUnit, error) {
	var out platform.FoodUnit
	out.FoodID = fid
	out.UnitID = unit
	err := tx.QueryRow(ctx, `SELECT f.name,u.dimension,u.factor_to_base::text FROM catalog.foods f JOIN catalog.units u ON u.id=$3 WHERE f.id=$2 AND f.status='active' AND (f.scope='global' OR f.owner_space_id=$1)`, sid, fid, unit).Scan(&out.Name, &out.Dimension, &out.Factor)
	return out, err
}

type fixedClock struct{ at time.Time }

func (c fixedClock) Now() time.Time { return c.at }

type fixture struct {
	db                          *platform.DB
	s                           *Service
	sid, uid, other, food, unit string
}

func setup(t *testing.T) fixture {
	t.Helper()
	db := testutil.NewDB(t)
	db.Clock = fixedClock{day("2026-11-09")}
	f := fixture{db: db, sid: platform.ID(), uid: platform.ID(), other: platform.ID(), food: platform.ID(), unit: "00000000-0000-7000-8000-000000000011"}
	ctx := context.Background()
	for _, id := range []string{f.uid, f.other} {
		if _, err := db.Pool.Exec(ctx, `INSERT INTO identity.users(id,email,full_name,password_hash,status) VALUES($1,$2,'Test Member','test-hash','active')`, id, id+"@test.example"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO household.spaces(id,kind,name,created_by) VALUES($1,'family','Family',$2);`, f.sid, f.uid); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO household.space_members(space_id,user_id,role,space_kind) VALUES($1,$2,'leader','family')`, f.sid, f.uid); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO catalog.foods(id,name,category_id,scope,default_unit_id,status) VALUES($1,'Carrot','00000000-0000-7000-8000-000000000001','global',$2,'active')`, f.food, f.unit); err != nil {
		t.Fatal(err)
	}
	f.s = New(db, dbCatalog{db})
	return f
}
func (f fixture) stock(t *testing.T, n string, expires string) Lot {
	t.Helper()
	lot, err := f.s.CreateLot(context.Background(), f.sid, f.uid, LotInput{FoodID: f.food, UnitID: f.unit, Quantity: qty(n), StorageLocation: "chill", PurchasedOn: "2026-11-09", ExpiresOn: expires})
	if err != nil {
		t.Fatal(err)
	}
	return lot
}
