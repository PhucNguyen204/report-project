package testutil

import (
	"context"
	"dicho/internal/platform"
	"dicho/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/url"
	"os"
	"strings"
	"testing"
)

func NewDB(t *testing.T) *platform.DB {
	t.Helper()
	ctx := context.Background()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://dicho:dicho-local-test@127.0.0.1:55432/dicho_test?sslmode=disable"
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	base, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := "dicho_test_" + strings.ReplaceAll(platform.ID(), "-", "")
	if _, err = base.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		base.Close()
		t.Fatalf("PostgreSQL required for integration tests: %v", err)
	}
	u.Path = "/" + name
	pool, err := pgxpool.New(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = base.Exec(context.Background(), "DROP DATABASE "+name+" WITH (FORCE)")
		base.Close()
	})
	if err = platform.Migrate(ctx, pool, migrations.Files); err != nil {
		t.Fatal(err)
	}
	db := platform.NewDB(pool)
	if _, err = platform.ConfigureQueue(ctx, db, nil); err != nil {
		t.Fatal(err)
	}
	return db
}
