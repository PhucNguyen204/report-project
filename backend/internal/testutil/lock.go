package testutil

import (
	"context"
	"dicho/internal/platform"
	"testing"
	"time"
)

func WaitForDatabaseLock(t *testing.T, ctx context.Context, db *platform.DB, fragment string, premature <-chan error) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-premature:
			t.Fatalf("operation completed before required lock wait: %v", err)
		default:
		}
		var waiting bool
		if err := db.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND strpos(query,$1)>0)", fragment).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("missing database lock wait: %s", fragment)
		case <-ticker.C:
		}
	}
}
