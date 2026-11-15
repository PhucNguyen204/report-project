//go:build integration

package platform_test

import (
	"bytes"
	"context"
	"dicho/internal/platform"
	"dicho/internal/testutil"
	"github.com/jackc/pgx/v5"
	"strings"
	"testing"
	"time"
)

func TestEncryptedQueuePersistsNoOTPOrRecipient(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	if _, err := platform.ConfigureQueue(ctx, db, nil, bytes.Repeat([]byte{7}, 32)); err != nil {
		t.Fatal(err)
	}
	if err := db.Tx(ctx, func(tx pgx.Tx) error {
		return db.Enqueue(ctx, tx, "email", "otp", map[string]string{"code": "123456", "to": "owner@example.test"}, time.Now())
	}); err != nil {
		t.Fatal(err)
	}
	var raw string
	var encrypted bool
	if err := db.Pool.QueryRow(ctx, "SELECT args::text,(args->>'encrypted')::boolean FROM river_job").Scan(&raw, &encrypted); err != nil {
		t.Fatal(err)
	}
	if !encrypted || strings.Contains(raw, "123456") || strings.Contains(raw, "owner@example.test") {
		t.Fatal("outbound secret leaked in database")
	}
}

func TestAIQueueBoundsProviderRetriesWithoutReducingMailRetries(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	if _, err := platform.ConfigureQueue(ctx, db, nil, bytes.Repeat([]byte{7}, 32)); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"ai.suggest", "email"} {
		if err := db.Tx(ctx, func(tx pgx.Tx) error {
			return db.Enqueue(ctx, tx, kind, platform.ID(), map[string]string{"jobId": platform.ID()}, time.Now())
		}); err != nil {
			t.Fatal(err)
		}
	}
	for kind, want := range map[string]int{"ai.suggest": 2, "email": 4} {
		var maxAttempts int
		var encrypted bool
		if err := db.Pool.QueryRow(ctx, "SELECT max_attempts,(args->>'encrypted')::boolean FROM river_job WHERE args->>'event'=$1", kind).Scan(&maxAttempts, &encrypted); err != nil {
			t.Fatal(err)
		}
		if maxAttempts != want || !encrypted {
			t.Fatalf("%s retry budget %d encrypted%v, want%d", kind, maxAttempts, encrypted, want)
		}
	}
}

func TestQueueMakesDueEventsAvailableAndKeepsFutureEventsScheduled(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	for _, item := range []struct {
		key   string
		at    time.Time
		state string
	}{
		{"due", db.Clock.Now(), "available"}, {"future", db.Clock.Now().Add(time.Hour), "scheduled"},
	} {
		if err := db.Tx(ctx, func(tx pgx.Tx) error {
			return db.Enqueue(ctx, tx, "notification", item.key, map[string]string{"purpose": "test"}, item.at)
		}); err != nil {
			t.Fatal(err)
		}
		var state string
		if err := db.Pool.QueryRow(ctx, "SELECT state::text FROM river_job WHERE args->>'key'=$1", "notification:"+item.key).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state != item.state {
			t.Fatalf("%s event state%s, want%s", item.key, state, item.state)
		}
	}
}
