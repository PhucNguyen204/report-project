//go:build integration

package app

import (
	"context"
	"dicho/internal/platform"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func activityRetentionFixture(t *testing.T) (*System, string, []string, time.Time) {
	t.Helper()
	h := newAPIHarness(t)
	now := time.Date(2026, 11, 13, 19, 25, 0, 0, time.UTC)
	h.system.DB.Clock = &lifecycleClock{now: now}
	user := platform.ID()
	ctx := context.Background()
	if _, err := h.system.DB.Pool.Exec(ctx, `INSERT INTO identity.users(id,email,full_name,password_hash,status) VALUES($1,$2,'Retention Member','unused','active')`, user, user+"@example.test"); err != nil {
		t.Fatal(err)
	}
	spaces := []string{platform.ID(), platform.ID()}
	for _, sid := range spaces {
		if _, err := h.system.DB.Pool.Exec(ctx, "INSERT INTO household.spaces(id,kind,name,created_by) VALUES($1,'family','Retention family',$2)", sid, user); err != nil {
			t.Fatal(err)
		}
		for i, at := range []time.Time{now.Add(-90*24*time.Hour - time.Microsecond), now.Add(-90 * 24 * time.Hour), now.Add(-time.Hour), now.Add(time.Hour)} {
			if err := h.system.DB.Tx(ctx, func(tx pgx.Tx) error {
				if err := h.system.DB.Activity(ctx, tx, sid, user, "fixture", platform.ID(), map[string]any{}); err != nil {
					return err
				}
				_, err := tx.Exec(ctx, "UPDATE platform.activities SET created_at=$3 WHERE space_id=$1 AND seq=$2", sid, i+1, at)
				return err
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := h.system.DB.Pool.Exec(ctx, "INSERT INTO admin.audit_logs(id,actor_id,action,created_at) VALUES($1,$2,'fixture',$3)", platform.ID(), user, now.Add(-180*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	return h.system, user, spaces, now
}

func TestActivityRetentionPreservesBoundarySequencesAndAdministrationHistory(t *testing.T) {
	s, user, spaces, _ := activityRetentionFixture(t)
	ctx := context.Background()
	if err := s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	for _, sid := range spaces {
		var sequences []int64
		rows, err := s.DB.Pool.Query(ctx, "SELECT seq FROM platform.activities WHERE space_id=$1 ORDER BY seq", sid)
		if err != nil {
			t.Fatal(err)
		}
		sequences, err = pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil || len(sequences) != 3 || sequences[0] != 2 || sequences[1] != 3 || sequences[2] != 4 {
			t.Fatalf("retained sequences=%v err=%v", sequences, err)
		}
		if err := s.DB.Tx(ctx, func(tx pgx.Tx) error {
			return s.DB.Activity(ctx, tx, sid, user, "after.cleanup", platform.ID(), map[string]any{})
		}); err != nil {
			t.Fatal(err)
		}
		var counter, latest int64
		if err := s.DB.Pool.QueryRow(ctx, "SELECT change_seq,(SELECT max(seq) FROM platform.activities WHERE space_id=$1) FROM household.spaces WHERE id=$1", sid).Scan(&counter, &latest); err != nil || counter != 5 || latest != 5 {
			t.Fatalf("sequence reused or reset: counter=%d latest=%d err=%v", counter, latest, err)
		}
	}
	var audits int
	if err := s.DB.Pool.QueryRow(ctx, "SELECT count(*) FROM admin.audit_logs WHERE actor_id=$1", user).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("administration history lost: count=%d err=%v", audits, err)
	}
}

func TestActivityRetentionFailureRollsBackEarlierCleanupAndCanRetry(t *testing.T) {
	s, user, _, now := activityRetentionFixture(t)
	ctx := context.Background()
	if _, err := s.DB.Pool.Exec(ctx, "INSERT INTO notification.notifications(id,user_id,dedupe_key,title,body,type,created_at) VALUES($1,$2,$3,'Fixture','Body','activity',$4)", platform.ID(), user, platform.ID(), now.Add(-91*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Pool.Exec(ctx, `CREATE FUNCTION platform.retention_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture retention failure'; RETURN NULL; END; $$;
CREATE TRIGGER retention_failure BEFORE DELETE ON platform.activities FOR EACH STATEMENT EXECUTE FUNCTION platform.retention_failure()`); err != nil {
		t.Fatal(err)
	}
	if err := s.Tick(ctx); err == nil {
		t.Fatal("activity cleanup failure was ignored")
	}
	check := func(notices, logs, slots int) {
		t.Helper()
		var gotNotices, gotLogs, gotSlots int
		if err := s.DB.Pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM notification.notifications),(SELECT count(*) FROM platform.activities),(SELECT count(*) FROM platform.scheduler_runs)").Scan(&gotNotices, &gotLogs, &gotSlots); err != nil || gotNotices != notices || gotLogs != logs || gotSlots != slots {
			t.Fatalf("retention atomicity: notices=%d logs=%d slots=%d err=%v", gotNotices, gotLogs, gotSlots, err)
		}
	}
	check(1, 8, 0)
	if _, err := s.DB.Pool.Exec(ctx, "DROP TRIGGER retention_failure ON platform.activities"); err != nil {
		t.Fatal(err)
	}
	if err := s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	check(0, 6, 1)
}
