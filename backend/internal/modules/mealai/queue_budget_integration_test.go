//go:build integration

package mealai

import (
	"context"
	"dicho/internal/modules/planning"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"sync/atomic"
	"testing"
	"time"

	"dicho/internal/platform"
	"dicho/internal/testutil"
	"github.com/riverqueue/river"
)

func TestAIQueueExhaustionDoesNotStrandPendingJob(t *testing.T) {
	var calls atomic.Int32
	s, db, user, sid, _ := setupAI(t, providerFunc(func(context.Context, Prompt) ([]Choice, error) {
		calls.Add(1)
		return nil, ErrUnavailable
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	client, err := platform.OpenQueue(ctx, db, s.Handle, key)
	if err != nil {
		t.Fatal(err)
	}
	job, err := s.Request(ctx, user, sid, aiInput(db))
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Pool.Exec(ctx, `
CREATE SEQUENCE review_claim_attempts;
CREATE FUNCTION review_claim_failure() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF nextval('review_claim_attempts')<=3 THEN
  RAISE EXCEPTION 'injected claim lock timeout' USING ERRCODE='55P03';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER review_claim_failure BEFORE UPDATE ON mealplan.ai_jobs FOR EACH ROW EXECUTE FUNCTION review_claim_failure();`)
	if err != nil {
		t.Fatal(err)
	}
	events, unsubscribe := client.Subscribe(river.EventKindJobFailed, river.EventKindJobCompleted)
	defer unsubscribe()
	if err = client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		stop, done := context.WithTimeout(context.Background(), 3*time.Second)
		defer done()
		if err := client.Stop(stop); err != nil {
			t.Error(err)
		}
	}()
	for i := 0; i < 2; i++ {
		select {
		case event := <-events:
			t.Logf("River attempt%d state%s", event.Job.Attempt, event.Job.State)
		case <-ctx.Done():
			t.Fatal("River did not exhaust attempts")
		}
	}
	var status, state string
	var attempts, riverAttempts int
	if err := db.Pool.QueryRow(ctx, "SELECT status,attempts,(SELECT state::text FROM river_job WHERE args->>'key'=$2),(SELECT attempt FROM river_job WHERE args->>'key'=$2) FROM mealplan.ai_jobs WHERE id=$1", job.ID, "ai.suggest:"+job.ID).Scan(&status, &attempts, &state, &riverAttempts); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || attempts != 1 || calls.Load() != 1 || riverAttempts != 2 {
		t.Fatalf("AI job stranded pending: durableAttempts%d providerCalls%d River%s/attempt%d", attempts, calls.Load(), state, riverAttempts)
	}
}

type atomicClock struct{ nanos atomic.Int64 }

func (c *atomicClock) Now() time.Time { return time.Unix(0, c.nanos.Load()).UTC() }

func TestAIExpiredWhileClaimWaitDoesNotCallProvider(t *testing.T) {
	var calls atomic.Int32
	s, db, user, sid, _ := setupAI(t, providerFunc(func(ctx context.Context, p Prompt) ([]Choice, error) { calls.Add(1); return defaultChoices(ctx, p) }))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	clock := &atomicClock{}
	now := db.Clock.Now()
	clock.nanos.Store(now.UnixNano())
	db.Clock = clock
	job, err := s.Request(ctx, user, sid, aiInput(db))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, "UPDATE mealplan.ai_jobs SET expires_at=$2 WHERE id=$1", job.ID, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	event := aiEvent(t, db, job.ID)
	holder, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback(context.Background())
	if _, err = holder.Exec(ctx, "SELECT id FROM identity.users WHERE id=$1 FOR UPDATE", user); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	premature := make(chan error, 1)
	go func() { err := s.Handle(ctx, event); done <- err; premature <- fmt.Errorf("worker returned %v", err) }()
	testutil.WaitForDatabaseLock(t, ctx, db, "SELECT system_role,token_version FROM identity.users", premature)
	clock.nanos.Store(now.Add(2 * time.Second).UnixNano())
	if err = holder.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("worker blocked")
	}
	var status, code string
	if err = db.Pool.QueryRow(ctx, "SELECT status,error_code FROM mealplan.ai_jobs WHERE id=$1", job.ID).Scan(&status, &code); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatalf("expired job called provider%d times; ended%s/%s", calls.Load(), status, code)
	}
}

type expiringPlanner struct {
	Planner
	clock *atomicClock
	after time.Time
}

func (p expiringPlanner) Suggest(ctx context.Context, user, sid string, in planning.SuggestionInput) (planning.SuggestionResult, error) {
	out, err := p.Planner.Suggest(ctx, user, sid, in)
	p.clock.nanos.Store(p.after.UnixNano())
	return out, err
}
func TestAIExpiredAfterPlannerDoesNotCallProvider(t *testing.T) {
	var calls atomic.Int32
	s, db, user, sid, _ := setupAI(t, providerFunc(func(ctx context.Context, p Prompt) ([]Choice, error) { calls.Add(1); return defaultChoices(ctx, p) }))
	ctx := context.Background()
	now := db.Clock.Now()
	clock := &atomicClock{}
	clock.nanos.Store(now.UnixNano())
	db.Clock = clock
	job, err := s.Request(ctx, user, sid, aiInput(db))
	if err != nil {
		t.Fatal(err)
	}
	s.planner = expiringPlanner{Planner: s.planner, clock: clock, after: now.Add(24 * time.Hour)}
	if err = s.Handle(ctx, aiEvent(t, db, job.ID)); err != nil {
		t.Fatal(err)
	}
	var status, code string
	if err = db.Pool.QueryRow(ctx, "SELECT status,error_code FROM mealplan.ai_jobs WHERE id=$1", job.ID).Scan(&status, &code); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 || status != "failed" || code != "expired" {
		t.Fatalf("expiry called provider %d status%s/%s", calls.Load(), status, code)
	}
}

func TestAIFinalQueueAttemptTerminalizesTransientPlannerAndPublishFailure(t *testing.T) {
	for _, failure := range []string{"planner", "publish"} {
		t.Run(failure, func(t *testing.T) {
			s, db, user, sid, _ := setupAI(t, providerFunc(defaultChoices))
			ctx := context.Background()
			job, err := s.Request(ctx, user, sid, aiInput(db))
			if err != nil {
				t.Fatal(err)
			}
			event := aiEvent(t, db, job.ID)
			event.Attempt = 2
			event.MaxAttempts = 2
			if failure == "planner" {
				s.planner = &transientPlanner{Planner: s.planner, fail: true}
			} else {
				db.EnqueueFunc = func(context.Context, pgx.Tx, string, string, any, time.Time) error {
					return errors.New("queue unavailable")
				}
			}
			if err = s.Handle(ctx, event); err != nil {
				t.Fatal(err)
			}
			var status, code string
			var attempts int
			if err = db.Pool.QueryRow(ctx, "SELECT status,error_code,attempts FROM mealplan.ai_jobs WHERE id=$1", job.ID).Scan(&status, &code, &attempts); err != nil {
				t.Fatal(err)
			}
			if status != "failed" || code != "provider_unavailable" || attempts != 1 {
				t.Fatalf("final attempt stranded%s/%s claims%d", status, code, attempts)
			}
		})
	}
}

func TestAIFinalClaimFailureDefersWithoutStrandingOrReplacingLease(t *testing.T) {
	s, db, user, sid, _ := setupAI(t, providerFunc(defaultChoices))
	ctx := context.Background()
	job, err := s.Request(ctx, user, sid, aiInput(db))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, `CREATE FUNCTION fail_ai_claim() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'lock timeout' USING ERRCODE='55P03'; END $$; CREATE TRIGGER fail_ai_claim BEFORE UPDATE ON mealplan.ai_jobs FOR EACH ROW EXECUTE FUNCTION fail_ai_claim();`); err != nil {
		t.Fatal(err)
	}
	event := aiEvent(t, db, job.ID)
	event.Attempt = 2
	event.MaxAttempts = 2
	err = s.Handle(ctx, event)
	var snooze *river.JobSnoozeError
	if !errors.As(err, &snooze) || snooze.Duration <= 0 {
		t.Fatalf("lastqueueclaimfailuremustdeferwithoutdiscard: %v", err)
	}
	if _, err = db.Pool.Exec(ctx, "DROP TRIGGER fail_ai_claim ON mealplan.ai_jobs"); err != nil {
		t.Fatal(err)
	}
	if err = s.Handle(ctx, event); err != nil {
		t.Fatal(err)
	}
	out, err := s.Get(ctx, user, sid, job.ID)
	if err != nil || out.Status != "ready" {
		t.Fatalf("deferredclaim %+v %v", out, err)
	}
}

func TestAIRiverFinalClaimFailureSnoozesThenRecoversWithSameAttemptBudget(t *testing.T) {
	var calls atomic.Int32
	s, db, user, sid, _ := setupAI(t, providerFunc(func(ctx context.Context, p Prompt) ([]Choice, error) { calls.Add(1); return defaultChoices(ctx, p) }))
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	client, err := platform.OpenQueue(ctx, db, s.Handle, key)
	if err != nil {
		t.Fatal(err)
	}
	job, err := s.Request(ctx, user, sid, aiInput(db))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, `CREATE SEQUENCE ai_claim_failures; CREATE FUNCTION ai_claim_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF nextval('ai_claim_failures')<=6 THEN RAISE EXCEPTION 'lock timeout' USING ERRCODE='55P03'; END IF; RETURN NEW; END $$; CREATE TRIGGER ai_claim_fail BEFORE UPDATE ON mealplan.ai_jobs FOR EACH ROW EXECUTE FUNCTION ai_claim_fail();`); err != nil {
		t.Fatal(err)
	}
	events, unsubscribe := client.Subscribe(river.EventKindJobFailed, river.EventKindJobSnoozed, river.EventKindJobCompleted)
	defer unsubscribe()
	if err = client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	stop := func() {
		stopCtx, done := context.WithTimeout(context.Background(), 3*time.Second)
		defer done()
		if err := client.Stop(stopCtx); err != nil {
			t.Error(err)
		}
	}
	defer stop()
	for i := 0; i < 2; i++ {
		select {
		case event := <-events:
			if i == 0 && event.Kind != river.EventKindJobFailed {
				t.Fatalf("first event %s", event.Kind)
			}
			if i == 1 && event.Kind != river.EventKindJobSnoozed {
				t.Fatalf("final claim did not snooze %s", event.Kind)
			}
		case <-ctx.Done():
			t.Fatal("River did not reach final claim")
		}
	}
	stop()
	var attempt, claims int
	var state, status string
	if err = db.Pool.QueryRow(ctx, "SELECT status,attempts,(SELECT state::text FROM river_job WHERE args->>'key'=$2),(SELECT attempt FROM river_job WHERE args->>'key'=$2) FROM mealplan.ai_jobs WHERE id=$1", job.ID, "ai.suggest:"+job.ID).Scan(&status, &claims, &state, &attempt); err != nil {
		t.Fatal(err)
	}
	if state != "scheduled" || attempt != 1 || status != "pending" || claims != 0 || calls.Load() != 0 {
		t.Fatalf("snooze state%s attempt%d status%s claims%d calls%d", state, attempt, status, claims, calls.Load())
	}
	if _, err = db.Pool.Exec(ctx, "UPDATE river_job SET state='available',scheduled_at=now() WHERE args->>'key'=$1", "ai.suggest:"+job.ID); err != nil {
		t.Fatal(err)
	}
	events, cancelRecovered := client.Subscribe(river.EventKindJobFailed, river.EventKindJobCompleted)
	defer cancelRecovered()
	if err = client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-events:
		if event.Kind != river.EventKindJobCompleted {
			t.Fatalf("deferred completion %s", event.Kind)
		}
	case <-ctx.Done():
		t.Fatal("deferred job not recovered")
	}
	out, err := s.Get(ctx, user, sid, job.ID)
	if err != nil || out.Status != "ready" || calls.Load() != 1 {
		t.Fatalf("deferred %+v calls%d err%v", out, calls.Load(), err)
	}
}
