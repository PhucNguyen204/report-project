//go:build integration

package notification

import (
	"context"
	"dicho/internal/platform"
	"dicho/internal/testutil"
	"testing"
	"time"
)

// Inbox90days and delivery30days cleanup must commit together, preserve younger records, and remain safe to replay.
func TestCleanupRetainsInbox90DaysAndDelivery30DaysAtomically(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	db.Clock = &fixedClock{time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)}
	user := notifyUser(t, db)
	now := db.Clock.Now()
	old := inboxNotice(t, db, user, now.Add(-91*24*time.Hour))
	pending := inboxNotice(t, db, user, now.Add(-31*24*time.Hour))
	sent := inboxNotice(t, db, user, now.Add(-31*24*time.Hour))
	fresh := inboxNotice(t, db, user, now.Add(-29*24*time.Hour))
	for _, tc := range []struct {
		id, state string
		at        any
	}{{old, "sent", now.Add(-time.Hour)}, {pending, "pending", nil}, {sent, "sent", now.Add(-31 * 24 * time.Hour)}, {fresh, "sent", now.Add(-29 * 24 * time.Hour)}} {
		if _, err := db.Pool.Exec(ctx, "INSERT INTO notification.deliveries(notification_id,device_id,status,sent_at) VALUES($1,$2,$3,$4)", tc.id, platform.ID(), tc.state, tc.at); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Pool.Exec(ctx, `CREATE FUNCTION notification.reject_cleanup() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'cleanup unavailable'; END; $$; CREATE TRIGGER reject_cleanup BEFORE DELETE ON notification.notifications FOR EACH ROW EXECUTE FUNCTION notification.reject_cleanup()`); err != nil {
		t.Fatal(err)
	}
	s := New(db, nil, nil)
	if err := s.Cleanup(ctx); err == nil {
		t.Fatal("retention failure hidden")
	}
	var count int
	if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM notification.deliveries").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 4 {
		t.Fatal("delivery purge survived rolled-back inbox cleanup")
	}
	if _, err := db.Pool.Exec(ctx, "DROP TRIGGER reject_cleanup ON notification.notifications; DROP FUNCTION notification.reject_cleanup()"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := s.Cleanup(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var notices, deliveries int
	if err := db.Pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM notification.notifications),(SELECT count(*) FROM notification.deliveries)").Scan(&notices, &deliveries); err != nil {
		t.Fatal(err)
	}
	if notices != 3 || deliveries != 1 {
		t.Fatalf("retention %d notices %d deliveries", notices, deliveries)
	}
	var remaining string
	if err := db.Pool.QueryRow(ctx, "SELECT notification_id::text FROM notification.deliveries").Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != fresh {
		t.Fatal("new delivery removed")
	}
}
