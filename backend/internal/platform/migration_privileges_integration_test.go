//go:build integration

package platform_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"dicho/internal/app"
	"dicho/internal/modules/media"
	"dicho/internal/platform"
	"dicho/internal/testutil"
	"dicho/migrations"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestReadOnlyMigrationChecksRejectMissingChangedAndAheadLedger(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	if err := platform.CheckMigrations(ctx, db.Pool.Pool, migrations.Files); err != nil {
		t.Fatal(err)
	}
	var name, checksum string
	if err := db.Pool.QueryRow(ctx, "SELECT name,checksum FROM schema_migrations ORDER BY name LIMIT 1").Scan(&name, &checksum); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []string{
		"DELETE FROM schema_migrations WHERE name=$1",
		"UPDATE schema_migrations SET checksum='changed' WHERE name=$1",
	} {
		if _, err := db.Pool.Exec(ctx, mutation, name); err != nil {
			t.Fatal(err)
		}
		if err := platform.CheckMigrations(ctx, db.Pool.Pool, migrations.Files); err == nil {
			t.Fatal("invalid ledger was accepted")
		}
		if _, err := db.Pool.Exec(ctx, "INSERT INTO schema_migrations VALUES($1,$2) ON CONFLICT(name) DO UPDATE SET checksum=excluded.checksum", name, checksum); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Pool.Exec(ctx, "INSERT INTO schema_migrations VALUES('999_future.sql','ahead')"); err != nil {
		t.Fatal(err)
	}
	if err := platform.CheckMigrations(ctx, db.Pool.Pool, migrations.Files); err == nil {
		t.Fatal("old binary accepted newer database schema")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := platform.CheckMigrations(cancelled, db.Pool.Pool, migrations.Files); err == nil {
		t.Fatal("cancelled migration check succeeded")
	}
}

func TestOpenQueueRefusesPendingAndNewerSchemaWithoutRepair(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	var removed int
	if err := db.Pool.QueryRow(ctx, "DELETE FROM river_migration WHERE version=(SELECT max(version) FROM river_migration WHERE line='main') AND line='main' RETURNING version").Scan(&removed); err != nil {
		t.Fatal(err)
	}
	if _, err := platform.OpenQueue(ctx, db, nil); err == nil {
		t.Fatal("pending River migration silently applied")
	}
	if _, err := db.Pool.Exec(ctx, "INSERT INTO river_migration(line,version) VALUES('main',$1)", removed); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, "INSERT INTO river_migration(line,version) VALUES('main',999999)"); err != nil {
		t.Fatal(err)
	}
	if _, err := platform.OpenQueue(ctx, db, nil); err == nil {
		t.Fatal("old worker accepted newer River schema")
	}
}

func TestQueueStartsAndCompletesWithRoleWithoutDDLPrivileges(t *testing.T) {
	db := testutil.NewDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	role := "dicho_runtime_" + strings.ReplaceAll(platform.ID(), "-", "")
	password := platform.ID()
	quoted := pgx.Identifier{role}.Sanitize()
	if _, err := db.Pool.Exec(ctx, "CREATE ROLE "+quoted+" LOGIN PASSWORD '"+password+"'"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.Pool.Exec(context.Background(), "DROP OWNED BY "+quoted); err != nil {
			t.Error(err)
		}
		if _, err := db.Pool.Exec(context.Background(), "DROP ROLE "+quoted); err != nil {
			t.Error(err)
		}
	})
	rows, err := db.Pool.Query(ctx, "SELECT nspname FROM pg_namespace WHERE nspname NOT IN ('pg_catalog','information_schema') AND nspname NOT LIKE 'pg_toast%'")
	if err != nil {
		t.Fatal(err)
	}
	schemas := []string{}
	for rows.Next() {
		var schema string
		if err = rows.Scan(&schema); err != nil {
			t.Fatal(err)
		}
		schemas = append(schemas, pgx.Identifier{schema}.Sanitize())
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, schema := range schemas {
		for _, grant := range []string{
			"GRANT USAGE ON SCHEMA " + schema + " TO " + quoted,
			"GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA " + schema + " TO " + quoted,
			"GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA " + schema + " TO " + quoted,
		} {
			if _, err = db.Pool.Exec(ctx, grant); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err = db.Pool.Exec(ctx, "REVOKE INSERT,UPDATE,DELETE ON schema_migrations FROM "+quoted); err != nil {
		t.Fatal(err)
	}
	cfg := db.Pool.Pool.Config()
	cfg.ConnConfig.User, cfg.ConnConfig.Password = role, password
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, "CREATE TABLE public.must_not_be_created(id int)"); err == nil {
		t.Fatal("runtime role has DDL privileges")
	}
	if _, err = pool.Exec(ctx, "DELETE FROM schema_migrations"); err == nil {
		t.Fatal("runtime can mutate migration ledger")
	}
	if err = platform.CheckMigrations(ctx, pool, migrations.Files); err != nil {
		t.Fatal(err)
	}
	runtime := platform.NewDB(pool)
	done := make(chan platform.Event, 1)
	client, err := platform.OpenQueue(ctx, runtime, func(_ context.Context, event platform.Event) error {
		done <- event
		return nil
	}, bytes.Repeat([]byte{5}, 32))
	if err != nil {
		t.Fatal(err)
	}
	if err = client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		stop, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		if err := client.Stop(stop); err != nil {
			t.Error(err)
		}
	}()
	if err = runtime.Tx(ctx, func(tx pgx.Tx) error {
		return runtime.Enqueue(ctx, tx, "runtime.check", platform.ID(), map[string]string{"purpose": "limited-role"}, time.Now())
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-done:
		if event.Event != "runtime.check" || !bytes.Contains(event.Payload, []byte("limited-role")) {
			t.Fatal("encrypted event payload changed")
		}
	case <-ctx.Done():
		t.Fatal("runtime worker could not execute job")
	}
	var complete int
	for complete == 0 {
		if err = pool.QueryRow(ctx, "SELECT count(*) FROM river_job WHERE state='completed'").Scan(&complete); err != nil {
			t.Fatal(err)
		}
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if complete != 1 {
		t.Fatalf("completed jobs%d", complete)
	}
	if err := client.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	_, signing, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	store := media.NewS3(s3.New(s3.Options{Region: "us-east-1"}), "media")
	system, err := app.NewSystem(ctx, runtime, app.Options{SigningKey: signing, QueueKey: bytes.Repeat([]byte{5}, 32), MediaStore: store, ExportStore: store})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "/it4788/v1/auth/register", strings.NewReader(`{"email":"runtime-role@example.test","fullName":"Runtime Member","password":"Password123"}`))
	request.Header.Set("X-App-Version", "1.0.0")
	request.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	system.Handler.ServeHTTP(w, request)
	if w.Code != 201 {
		t.Fatalf("limited-role API registration%d %s", w.Code, w.Body)
	}
	var users int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM identity.users WHERE email='runtime-role@example.test'").Scan(&users); err != nil || users != 1 {
		t.Fatalf("registration readback%d error%v", users, err)
	}
	w = httptest.NewRecorder()
	system.Handler.ServeHTTP(w, httptest.NewRequest("GET", "/readyz", nil))
	if w.Code != 200 {
		t.Fatalf("limited-role ready check%d %s", w.Code, w.Body)
	}
}
