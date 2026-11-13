//go:build integration

package notification

import (
	"context"
	"dicho/internal/platform"
	"dicho/internal/testutil"
	"testing"
	"time"
)

type fixedClock struct{ at time.Time }

func (c *fixedClock) Now() time.Time { return c.at }
func TestReminderWindowAggregatesSpacesAndDedupes(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	loc, _ := time.LoadLocation("Asia/Ho_Chi_Minh")
	clock := &fixedClock{time.Date(2026, 11, 9, 7, 0, 0, 0, loc)}
	db.Clock = clock
	user := platform.ID()
	_, err := db.Pool.Exec(ctx, "INSERT INTO identity.users(id,email,full_name,password_hash,status) VALUES($1,'reminder@example.test','Notify User','unused','active')", user)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Pool.Exec(ctx, "INSERT INTO identity.user_settings(user_id,reminder_days) VALUES($1,3)", user)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"personal", "family"} {
		sid := platform.ID()
		_, err = db.Pool.Exec(ctx, "INSERT INTO household.spaces(id,kind,name,created_by) VALUES($1,$2,'Test space',$3)", sid, kind, user)
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.Pool.Exec(ctx, "INSERT INTO household.space_members(space_id,user_id,role,space_kind) VALUES($1,$2,'leader',$3)", sid, user, kind)
		if err != nil {
			t.Fatal(err)
		}
		for _, expires := range []string{"2026-11-08", "2026-11-12", "2026-11-13", "2026-11-07"} {
			_, err = db.Pool.Exec(ctx, `INSERT INTO pantry.food_lots(id,space_id,food_id,unit_id,unit_factor,dimension,initial_qty,remaining_qty,base_remaining,purchased_on,expires_on,storage_location) VALUES($1,$2,'00000000-0000-7000-8000-000000000021','00000000-0000-7000-8000-000000000011',1,'mass',1,1,1,'2026-11-01',$3,'chill')`, platform.ID(), sid, expires)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	s := New(db, testMail{}, &testPush{calls: map[string]int{}})
	if err = s.DispatchReminders(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = db.Pool.QueryRow(ctx, "SELECT count(*) FROM river_job").Scan(&n)
	if n != 0 {
		t.Fatal("reminded before configured hour")
	}
	clock.at = clock.at.Add(2 * time.Hour)
	for range 2 {
		if err = s.DispatchReminders(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var body string
	err = db.Pool.QueryRow(ctx, "SELECT args->'payload'->>'body' FROM river_job WHERE args->>'event'='notification'").Scan(&body)
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Pool.QueryRow(ctx, "SELECT count(*) FROM river_job").Scan(&n)
	if n != 1 {
		t.Fatalf("duplicate reminders %d", n)
	}
	if body != "Có 4 lô thực phẩm sắp hoặc vừa hết hạn: Cà rốt, Cà rốt, Cà rốt, Cà rốt" {
		t.Fatalf("wrong window/space aggregation: %s", body)
	}
	_, _ = db.Pool.Exec(ctx, "UPDATE identity.user_settings SET expiry_notify_on=false WHERE user_id=$1", user)
	clock.at = clock.at.Add(24 * time.Hour)
	if err = s.DispatchReminders(ctx); err != nil {
		t.Fatal(err)
	}
	_ = db.Pool.QueryRow(ctx, "SELECT count(*) FROM river_job").Scan(&n)
	if n != 1 {
		t.Fatal("notified disabled user")
	}
}
