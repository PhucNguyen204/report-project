//go:build integration

package platform_test

import (
	"bytes"
	"context"
	"dicho/internal/observability"
	"dicho/internal/platform"
	"dicho/internal/testutil"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type queueClock struct{ at time.Time }

func (c queueClock) Now() time.Time { return c.at }

func queueStatEvent(t *testing.T, db *platform.DB, kind, state string, at time.Time) {
	t.Helper()
	ctx := context.Background()
	key := platform.ID()
	if err := db.Tx(ctx, func(tx pgx.Tx) error {
		return db.Enqueue(ctx, tx, kind, key, map[string]string{"token": "PRIVATE-PAYLOAD", "user": "PRIVATE-TENANT"}, at)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, `UPDATE river_job SET state=$2::river_job_state,scheduled_at=$3,finalized_at=CASE WHEN $2 IN ('completed','cancelled','discarded') THEN $4::timestamptz ELSE NULL END WHERE args->>'key'=$1`, kind+":"+key, state, at, db.Clock.Now()); err != nil {
		t.Fatal(err)
	}
}

func queueStatScrape(t *testing.T, db *platform.DB) string {
	t.Helper()
	o := observability.New(observability.Options{QueueStats: db.QueueStats})
	w := httptest.NewRecorder()
	o.Metrics().ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	if w.Code != 200 {
		t.Fatalf("scrape HTTP%d", w.Code)
	}
	return w.Body.String()
}

func TestQueueStatsAggregateEncryptedEventsWithoutFutureOrTerminalAge(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	db.Clock = queueClock{time.Date(2026, 11, 15, 6, 0, 0, 0, time.UTC)}
	if _, err := platform.ConfigureQueue(ctx, db, nil, bytes.Repeat([]byte{7}, 32)); err != nil {
		t.Fatal(err)
	}
	now := db.Clock.Now()
	for _, entry := range []struct {
		kind, state string
		offset      time.Duration
	}{
		{"email", "available", -90 * time.Second}, {"email", "available", -10 * time.Second},
		{"email", "retryable", -120 * time.Second}, {"email", "scheduled", time.Hour},
		{"email", "running", -time.Hour}, {"email", "pending", -2 * time.Hour},
		{"notification", "scheduled", -30 * time.Second}, {"ai.suggest", "discarded", -time.Hour},
		{"PRIVATE-KIND-A", "available", -10 * time.Second}, {"PRIVATE-KIND-B", "available", -20 * time.Second},
		{"email", "completed", -3 * time.Hour}, {"email", "cancelled", -4 * time.Hour},
	} {
		queueStatEvent(t, db, entry.kind, entry.state, now.Add(entry.offset))
	}
	snapshot, err := db.QueueStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	groups := map[string]observability.QueueGroup{}
	for _, group := range snapshot.Groups {
		groups[group.Kind+":"+group.State] = group
	}
	for key, want := range map[string]observability.QueueGroup{
		"email:available": {Jobs: 2, OldestDueSeconds: 90},
		"email:retryable": {Jobs: 1, OldestDueSeconds: 120},
		"email:scheduled": {Jobs: 1}, "email:running": {Jobs: 1}, "email:pending": {Jobs: 1},
		"notification:scheduled": {Jobs: 1, OldestDueSeconds: 30}, "ai.suggest:discarded": {Jobs: 1},
		"other:available": {Jobs: 2, OldestDueSeconds: 20},
	} {
		got, ok := groups[key]
		if !ok || got.Jobs != want.Jobs || got.OldestDueSeconds != want.OldestDueSeconds {
			t.Fatalf("%s got%+v present%v want%+v", key, got, ok, want)
		}
	}
	if len(groups) != 8 {
		t.Fatalf("unexpected groups: %+v", groups)
	}
	raw := queueStatScrape(t, db)
	for _, line := range []string{`dicho_queue_stats_up 1`, `dicho_queue_jobs{kind="email",state="available"} 2`, `dicho_queue_oldest_due_seconds{kind="email"} 120`, `dicho_queue_oldest_due_seconds{kind="notification"} 30`, `dicho_queue_oldest_due_seconds{kind="other"} 20`} {
		if !strings.Contains(raw, line) {
			t.Fatalf("missing %s", line)
		}
	}
	if strings.Contains(raw, "PRIVATE") {
		t.Fatal("encrypted event metadata or payload leaked")
	}
	if _, err := db.Pool.Exec(ctx, "DELETE FROM river_job"); err != nil {
		t.Fatal(err)
	}
	raw = queueStatScrape(t, db)
	if !strings.Contains(raw, `dicho_queue_jobs{kind="email",state="available"} 0`) || !strings.Contains(raw, `dicho_queue_oldest_due_seconds{kind="email"} 0`) {
		t.Fatal("empty queue retained old count/age")
	}
}

func TestQueueStatsRespectCancellationAndUnavailableDatabase(t *testing.T) {
	db := testutil.NewDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := db.QueueStats(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled source result %v", err)
	}
	db.Pool.Close()
	if _, err := db.QueueStats(context.Background()); err == nil {
		t.Fatal("closed DB reported healthy")
	}
	raw := queueStatScrape(t, db)
	if !strings.Contains(raw, "dicho_queue_stats_up 0") || strings.Contains(raw, "dicho_queue_jobs{") || strings.Contains(raw, "closed pool") {
		t.Fatal("DB failure leaked or emitted healthy data")
	}
	var missing *platform.DB
	if _, err := missing.QueueStats(context.Background()); err == nil {
		t.Fatal("missing DB reported healthy")
	}
}

func TestQueueStatsScrapeCancelsRealBlockedDatabaseQuery(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	lock, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback(context.Background())
	if _, err = lock.Exec(ctx, "LOCK TABLE river_job IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	raw := queueStatScrape(t, db)
	if time.Since(started) > time.Second || !strings.Contains(raw, "dicho_queue_stats_up 0") || strings.Contains(raw, "dicho_queue_jobs{") {
		t.Fatal("blocked query was not canceled or emitted healthy data")
	}
}
