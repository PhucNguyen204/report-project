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

func TestSchedulerStopsAfterCleanupFailureAndLeavesSlotRetryable(t *testing.T) {
	h := newAPIHarness(t)
	waitCtx, deadlineCancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer deadlineCancel()
	ctx, cancel := context.WithCancel(waitCtx)
	defer cancel()
	if _, err := h.system.DB.Pool.Exec(ctx, `CREATE FUNCTION platform.scheduler_fixture_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'scheduler fixture failure'; RETURN NULL; END; $$;
 CREATE TRIGGER cleanup_failure BEFORE DELETE ON identity.otp_challenges FOR EACH STATEMENT EXECUTE FUNCTION platform.scheduler_fixture_failure();`); err != nil {
		t.Fatal(err)
	}
	reported := make(chan error, 1)
	done := make(chan struct{})
	go func() { h.system.RunScheduler(ctx, func(err error) { reported <- err; cancel() }); close(done) }()
	select {
	case err := <-reported:
		if err == nil {
			t.Fatal("cleanup failure was not reported")
		}
	case <-waitCtx.Done():
		t.Fatal("scheduler did not report cleanup failure")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop after cancellation")
	}
	var slots int
	if err := h.system.DB.Pool.QueryRow(context.Background(), "SELECT count(*) FROM platform.scheduler_runs").Scan(&slots); err != nil || slots != 0 {
		t.Fatalf("failed tick committed a durable slot%d error%v", slots, err)
	}
	if _, err := h.system.DB.Pool.Exec(context.Background(), "DROP TRIGGER cleanup_failure ON identity.otp_challenges"); err != nil {
		t.Fatal(err)
	}
	if err := h.system.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := h.system.DB.Pool.QueryRow(context.Background(), "SELECT count(*) FROM platform.scheduler_runs").Scan(&slots); err != nil || slots != 1 {
		t.Fatalf("next tick failed to recover slot%d error%v", slots, err)
	}
}

func TestSchedulerRunsInitialTickBeforeWaitingAndCancelsCleanly(t *testing.T) {
	h := newAPIHarness(t)
	waitCtx, deadlineCancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer deadlineCancel()
	ctx, cancel := context.WithCancel(waitCtx)
	defer cancel()
	done := make(chan struct{})
	errors := make(chan error, 1)
	go func() { h.system.RunScheduler(ctx, func(err error) { errors <- err; cancel() }); close(done) }()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var slots int
		if err := h.system.DB.Pool.QueryRow(waitCtx, "SELECT count(*) FROM platform.scheduler_runs").Scan(&slots); err != nil {
			t.Fatal(err)
		}
		if slots == 1 {
			break
		}
		select {
		case err := <-errors:
			t.Fatalf("initial tick failed: %v", err)
		case <-waitCtx.Done():
			t.Fatal("initial tick waited for minute timer")
		case <-ticker.C:
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not stop after successful tick")
	}
	select {
	case err := <-errors:
		t.Fatalf("successful cancellation reported as failure: %v", err)
	default:
	}
}
