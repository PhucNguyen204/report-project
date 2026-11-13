//go:build integration

package accounts

import (
	"context"
	"dicho/internal/platform"
	"dicho/internal/testutil"
	"testing"
	"time"
)

func TestCredentialRetentionPreservesLiveResetsAndRollsBackOnFailure(t *testing.T) {
	db := testutil.NewDB(t)
	clock := &fixedClock{time.Date(2026, 11, 13, 6, 0, 0, 0, time.UTC)}
	db.Clock = clock
	s, ok := any(New(db, nil)).(interface{ Cleanup(context.Context) error })
	if !ok {
		t.Fatal("credential retention is missing")
	}
	ctx := context.Background()
	user := platform.ID()
	if _, err := db.Pool.Exec(ctx, `INSERT INTO identity.users(id,email,full_name,password_hash,status) VALUES($1,$2,'Retention','unused','active')`, user, user+"@test.local"); err != nil {
		t.Fatal(err)
	}
	for i, age := range []time.Duration{25 * time.Hour, 24 * time.Hour, 23 * time.Hour, 25 * time.Hour} {
		var reset any
		if i == 3 {
			reset = clock.at.Add(time.Minute)
		}
		if _, err := db.Pool.Exec(ctx, `INSERT INTO identity.otp_challenges(id,user_id,purpose,code_hash,expires_at,sent_at,reset_expires_at) VALUES($1,$2,'reset_password',$3,$4,$5,$6)`, platform.ID(), user, []byte{1}, clock.at.Add(-age), clock.at.Add(-age-time.Minute), reset); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 4 {
		expires := clock.at.Add(time.Hour)
		var revoked any
		if i == 0 {
			revoked = clock.at.Add(-31 * 24 * time.Hour)
		}
		if i == 1 || i == 2 {
			expires = clock.at.Add(-time.Duration(31-i) * 24 * time.Hour)
		}
		if _, err := db.Pool.Exec(ctx, `INSERT INTO identity.refresh_sessions(id,user_id,family_id,token_hash,device_id,platform,expires_at,revoked_at) VALUES($1,$2,$3,$4,$5,'android',$6,$7)`, platform.ID(), user, platform.ID(), []byte{byte(i)}, platform.ID(), expires, revoked); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Pool.Exec(ctx, `CREATE FUNCTION identity.reject_retention() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'retention unavailable'; END; $$; CREATE TRIGGER reject_retention BEFORE DELETE ON identity.refresh_sessions FOR EACH ROW EXECUTE FUNCTION identity.reject_retention()`); err != nil {
		t.Fatal(err)
	}
	if err := s.Cleanup(ctx); err == nil {
		t.Fatal("failed session purge hidden")
	}
	var count int
	if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM identity.otp_challenges").Scan(&count); err != nil || count != 4 {
		t.Fatalf("partial OTP purge count=%d err=%v", count, err)
	}
	if _, err := db.Pool.Exec(ctx, `DROP TRIGGER reject_retention ON identity.refresh_sessions; DROP FUNCTION identity.reject_retention()`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.Cleanup(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var otps, sessions int
	if err := db.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM identity.otp_challenges),(SELECT count(*) FROM identity.refresh_sessions)`).Scan(&otps, &sessions); err != nil || otps != 2 || sessions != 2 {
		t.Fatalf("retention OTP=%d sessions=%d err=%v", otps, sessions, err)
	}
}
