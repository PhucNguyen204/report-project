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
