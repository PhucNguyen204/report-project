//go:build integration

package notification

import (
	"context"
	"dicho/internal/platform"
	"dicho/internal/testutil"
	"github.com/jackc/pgx/v5"
	"testing"
)

func TestPurgeUserRemovesOnlyOwnedInboxAndDeliveries(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	first, other := platform.ID(), platform.ID()
	for _, id := range []string{first, other} {
		_, err := db.Pool.Exec(ctx, "INSERT INTO identity.users(id,email,full_name,password_hash,status) VALUES($1,$2,'Notify User','unused','active')", id, id+"@example.test")
		if err != nil {
			t.Fatal(err)
		}
		nid := platform.ID()
		_, err = db.Pool.Exec(ctx, "INSERT INTO notification.notifications(id,user_id,dedupe_key,title,body,type) VALUES($1,$2,$3,'title','body','activity')", nid, id, nid)
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.Pool.Exec(ctx, "INSERT INTO notification.deliveries(notification_id,device_id) VALUES($1,$2)", nid, platform.ID())
		if err != nil {
			t.Fatal(err)
		}
	}
	s := New(db, nil, nil)
	for range 2 {
		if err := db.Tx(ctx, func(tx pgx.Tx) error { return s.PurgeUser(ctx, tx, first) }); err != nil {
			t.Fatal(err)
		}
	}
	var notices, deliveries int
	_ = db.Pool.QueryRow(ctx, "SELECT count(*) FROM notification.notifications").Scan(&notices)
	_ = db.Pool.QueryRow(ctx, "SELECT count(*) FROM notification.deliveries").Scan(&deliveries)
	if notices != 1 || deliveries != 1 {
		t.Fatalf("other user's data changed: %d %d", notices, deliveries)
	}
}
