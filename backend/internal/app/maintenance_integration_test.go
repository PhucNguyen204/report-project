//go:build integration

package app

import (
	"context"
	"dicho/internal/platform"
	"testing"
	"time"
)

func TestSchedulerRunsNotificationRetentionAndReplaysOncePerMinute(t *testing.T) {
	h := newAPIHarness(t)
	db := h.system.DB
	ctx := context.Background()
	user, old, fresh := platform.ID(), platform.ID(), platform.ID()
	if _, err := db.Pool.Exec(ctx, `INSERT INTO identity.users(id,email,full_name,password_hash,status) VALUES($1,$2,'Maintenance','unused','active')`, user, user+"@test.local"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `INSERT INTO identity.otp_challenges(id,user_id,purpose,code_hash,expires_at,sent_at) VALUES($1,$2,'register',$3,$4,$4)`, platform.ID(), user, []byte{1}, db.Clock.Now().Add(-25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	for id, age := range map[string]time.Duration{old: 91 * 24 * time.Hour, fresh: time.Hour} {
		if _, err := db.Pool.Exec(ctx, `INSERT INTO notification.notifications(id,user_id,dedupe_key,title,body,type,created_at) VALUES($1,$2,$4,'Title','Body','activity',$3)`, id, user, db.Clock.Now().Add(-age), id); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if err := h.system.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var notices, slots, otps int
	if err := db.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM notification.notifications),(SELECT count(*) FROM platform.scheduler_runs),(SELECT count(*) FROM identity.otp_challenges)`).Scan(&notices, &slots, &otps); err != nil {
		t.Fatal(err)
	}
	if notices != 1 || slots != 1 || otps != 0 {
		t.Fatalf("retention/replay: notices=%d slots=%d OTP=%d", notices, slots, otps)
	}
}
