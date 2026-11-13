//go:build integration

package main

import (
	"context"
	"dicho/internal/platform"
	"dicho/internal/testutil"
	"dicho/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestMigratorBootstrapsEmptyDatabaseUsingOnlyDatabaseURL(t *testing.T) {
	base := testutil.NewDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	name := "dicho_migrate_" + strings.ReplaceAll(platform.ID(), "-", "")
	if _, err := base.Pool.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := base.Pool.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Error(err)
		}
	})
	cfg := base.Pool.Pool.Config()
	cfg.ConnConfig.Database = name
	connection, err := url.Parse(cfg.ConnConfig.ConnString())
	if err != nil {
		t.Fatal("invalid test connection URL")
	}
	connection.Path = "/" + name
	dsn := connection.String()
	for range 2 {
		if err := run(ctx, func(key string) string {
			if key != "DATABASE_URL" {
				t.Errorf("migrator requested unrelated configuration %s", key)
			}
			return dsn
		}); err != nil {
			t.Fatal(err)
		}
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = platform.CheckMigrations(ctx, pool, migrations.Files); err != nil {
		t.Fatal(err)
	}
	if _, err = platform.OpenQueue(ctx, platform.NewDB(pool), nil); err != nil {
		t.Fatal(err)
	}
	var units int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM catalog.units").Scan(&units); err != nil || units == 0 {
		t.Fatalf("seed missing: units%d error%v", units, err)
	}
}
