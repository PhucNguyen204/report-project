//go:build integration

package app

import (
	"context"
	"dicho/internal/platform"
	"dicho/internal/testutil"
	"dicho/migrations"
	"testing"
)

func TestCookMigrationAddsNullableActorWithoutRequiringSyntheticHistory(t *testing.T) {
	db := testutil.NewDB(t)
	var present bool
	if err := db.Pool.QueryRow(context.Background(), "SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='mealplan' AND table_name='meal_entries' AND column_name='cooked_by' AND data_type='uuid' AND is_nullable='YES')").Scan(&present); err != nil {
		t.Fatal(err)
	}
	if !present {
		t.Fatal("cooked actor cannot survive commit/replay while preserving unknown legacy actors")
	}
}

func TestCookMigrationBackfillsOnlyKnownActorAndKeepsLegacyUnknownNull(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	if _, err := db.Pool.Exec(ctx, "ALTER TABLE mealplan.meal_entries DROP COLUMN cooked_by; DELETE FROM schema_migrations WHERE name='050_meal_cooked_actor.sql'"); err != nil {
		t.Fatal(err)
	}
	u, sid, known, unknown := platform.ID(), platform.ID(), platform.ID(), platform.ID()
	if _, err := db.Pool.Exec(ctx, "INSERT INTO identity.users(id,email,full_name,password_hash,status) VALUES($1,$2,'Legacy Actor','unused','active')", u, u+"@example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, "INSERT INTO household.spaces(id,kind,name,created_by) VALUES($1,'personal','Legacy',$2)", sid, u); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{known, unknown} {
		if _, err := db.Pool.Exec(ctx, "INSERT INTO mealplan.meal_entries(id,space_id,name,meal_date,meal_slot,servings,status,cooked_at,created_by) VALUES($1,$2,'Legacy meal',CURRENT_DATE,'lunch',2,'cooked',now(),$3)", id, sid, u); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Pool.Exec(ctx, "INSERT INTO platform.activities(space_id,seq,actor_id,kind,entity_id,payload) VALUES($1,1,$2,'meal.cooked',$3,'{}')", sid, u, known); err != nil {
		t.Fatal(err)
	}
	if err := platform.Migrate(ctx, db.Pool.Pool, migrations.Files); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{known: u, unknown: ""} {
		var actor *string
		if err := db.Pool.QueryRow(ctx, "SELECT cooked_by::text FROM mealplan.meal_entries WHERE id=$1", id).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		if (want == "" && actor != nil) || (want != "" && (actor == nil || *actor != want)) {
			t.Fatalf("legacy actor mismatch known%v actor%v", want != "", actor)
		}
	}
	if err := platform.Migrate(ctx, db.Pool.Pool, migrations.Files); err != nil {
		t.Fatal("migration replay failed", err)
	}
}
