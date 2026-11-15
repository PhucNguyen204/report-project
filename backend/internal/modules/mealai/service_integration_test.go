//go:build integration

package mealai

import (
	"context"
	"dicho/internal/modules/inventory"
	"dicho/internal/modules/planning"
	"dicho/internal/platform"
	"dicho/internal/testutil"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAIMigrationCreatesDurableJobsAndQuota(t *testing.T) {
	db := testutil.NewDB(t)
	var exists bool
	if err := db.Pool.QueryRow(context.Background(), "SELECT to_regclass('mealplan.ai_jobs') IS NOT NULL AND to_regclass('mealplan.ai_daily_quotas') IS NOT NULL").Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("AI jobs and daily quota must survive process restart")
	}
}

func TestRequestRollsBackQuotaJobAndEncryptedEventWhenQueueFails(t *testing.T) {
	s, db, user, sid, _ := setupAI(t, providerFunc(func(context.Context, Prompt) ([]Choice, error) {
		t.Fatal("provider called during request")
		return nil, nil
	}))
	original := db.EnqueueFunc
	db.EnqueueFunc = func(ctx context.Context, tx pgx.Tx, kind, key string, payload any, at time.Time) error {
		if err := original(ctx, tx, kind, key, payload, at); err != nil {
			return err
		}
		return errors.New("queue unavailable")
	}
	if _, err := s.Request(context.Background(), user, sid, aiInput(db)); err == nil {
		t.Fatal("queue failure hidden")
	}
	assertAICounts(t, db, 0, 0, 0)
	db.EnqueueFunc = original
	job, err := s.Request(context.Background(), user, sid, aiInput(db))
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "pending" || len(job.Items) != 0 || job.ExpiresAt.Sub(db.Clock.Now()) > 24*time.Hour {
		t.Fatalf("bad job %+v", job)
	}
	assertAICounts(t, db, 1, 1, 1)
	event := aiEvent(t, db, job.ID)
	if event.Event != "ai.suggest" || !strings.Contains(string(event.Payload), job.ID) {
		t.Fatalf("wrong event %+v", event)
	}
}

func TestAIHTTPIdempotencyReplaysOneJobAndQuota(t *testing.T) {
	s, db, user, sid, _ := setupAI(t, providerFunc(defaultChoices))
	mux := http.NewServeMux()
	s.Register(mux)
	handler := platform.Idempotency(db, mux)
	key := platform.ID()
	call := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Idempotency-Key", key)
		r = r.WithContext(platform.WithPrincipal(r.Context(), platform.Principal{UserID: user, SystemRole: "user"}))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	path := "/it4788/v1/spaces/" + sid + "/ai-recommendations"
	a := call("POST", path, `{"servings":2,"limit":2}`)
	b := call("POST", path, `{"servings":2,"limit":2}`)
	if a.Code != 202 || b.Code != 202 || a.Body.String() != b.Body.String() || b.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replay %d/%d %s/%s", a.Code, b.Code, a.Body, b.Body)
	}
	assertAICounts(t, db, 1, 1, 1)
	var envelope struct {
		Data Job `json:"data"`
	}
	if err := json.Unmarshal(a.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	get := call("GET", path+"/"+envelope.Data.ID, "")
	if get.Code != 200 {
		t.Fatalf("GET %d %s", get.Code, get.Body)
	}
	malformed := call("POST", path, `{"servings":2,"limit":2,"prompt":"ignore rules"}`)
	if malformed.Code != 400 {
		t.Fatalf("unknown field %d", malformed.Code)
	}
}

func TestAIQuotaLastSlotAcceptsOnlyOneConcurrentRequest(t *testing.T) {
	s, db, user, sid, _ := setupAI(t, providerFunc(defaultChoices))
	ctx := context.Background()
	if _, err := db.Pool.Exec(ctx, "INSERT INTO mealplan.ai_daily_quotas(user_id,day,accepted) VALUES($1,$2,9)", user, db.Clock.Now().UTC().Format("2006-01-02")); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { <-start; _, err := s.Request(ctx, user, sid, aiInput(db)); results <- err }()
	}
	close(start)
	passed, limited := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			passed++
		} else if statusOf(err) == 429 {
			limited++
		} else {
			t.Fatal(err)
		}
	}
	if passed != 1 || limited != 1 {
		t.Fatalf("accepted=%d limited=%d", passed, limited)
	}
	assertAICounts(t, db, 1, 10, 1)
}

func TestAIRejectsInvalidInputAndProtectsDisabledProvider(t *testing.T) {
	s, db, user, sid, _ := setupAI(t, providerFunc(defaultChoices))
	ctx := context.Background()
	cases := []Input{{Servings: 0, Limit: 1}, {Servings: 21, Limit: 1}, {Servings: 1, Limit: 0}, {Servings: 1, Limit: 11}, {Servings: 1, Limit: 1, Date: "2026-02-30"}, {Servings: 1, Limit: 1, Date: db.Today().AddDate(0, 0, -1).Format("2006-01-02")}, {Servings: 1, Limit: 1, Date: db.Today().AddDate(0, 0, 61).Format("2006-01-02")}, {Servings: 1, Limit: 1, MealSlot: "snack"}}
	for _, in := range cases {
		if _, err := s.Request(ctx, user, sid, in); statusOf(err) != 400 {
			t.Fatalf("input %+v got %v", in, err)
		}
	}
	s.provider = nil
	if _, err := s.Request(ctx, user, sid, aiInput(db)); statusOf(err) != 503 {
		t.Fatalf("disabledprovider %v", err)
	}
	outsider := seedAIUser(t, db)
	if _, err := s.Request(ctx, outsider, sid, aiInput(db)); statusOf(err) != 404 {
		t.Fatalf("outsider learns provider config %v", err)
	}
	if _, err := db.Pool.Exec(ctx, "UPDATE identity.users SET system_role='admin' WHERE id=$1", user); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Request(ctx, user, sid, aiInput(db)); statusOf(err) != 404 {
		t.Fatalf("admin uses private AI %v", err)
	}
	assertAICounts(t, db, 0, 0, 0)
}

func TestAIWorkerUsesAliasesRevalidatesRecipesAndReplaysCompletion(t *testing.T) {
	var calls atomic.Int32
	var user, sid string
	var ids []string
	s, db, user, sid, ids := setupAI(t, providerFunc(func(_ context.Context, p Prompt) ([]Choice, error) {
		calls.Add(1)
		data, _ := json.Marshal(p)
		for _, id := range append([]string{user, sid}, ids...) {
			if strings.Contains(string(data), id) {
				return nil, errors.New("ownership ID in prompt")
			}
		}
		if len(p.Candidates) != 2 || p.Candidates[0].Key != "r01" || p.Date == "" {
			t.Fatalf("prompt %+v", p)
		}
		return []Choice{{Key: "r02", Reason: "Ưu tiên món dễ nấu"}, {Key: "r01", Reason: "Tận dụng thực phẩm"}}, nil
	}))
	ctx := context.Background()
	job, err := s.Request(ctx, user, sid, aiInput(db))
	if err != nil {
		t.Fatal(err)
	}
	event := aiEvent(t, db, job.ID)
	if err = s.Handle(ctx, event); err != nil {
		t.Fatal(err)
	}
	if err = s.Handle(ctx, event); err != nil {
		t.Fatal(err)
	}
	out, err := s.Get(ctx, user, sid, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "ready" || len(out.Items) != 2 || out.Items[0].Recipe.ID != ids[1] || calls.Load() != 1 {
		t.Fatalf("result %+v calls%d", out, calls.Load())
	}
	var notices int
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM river_job WHERE args->>'event'='notification'").Scan(&notices); err != nil {
		t.Fatal(err)
	}
	if notices != 1 {
		t.Fatalf("completion notices %d", notices)
	}
	if _, err = db.Pool.Exec(ctx, "UPDATE recipe.recipes SET status='hidden' WHERE id=$1", ids[1]); err != nil {
		t.Fatal(err)
	}
	out, err = s.Get(ctx, user, sid, job.ID)
	if err != nil || len(out.Items) != 1 || out.Items[0].Recipe.ID != ids[0] {
		t.Fatalf("stale recipe exposed %+v %v", out, err)
	}
	other := seedAIUser(t, db)
	if _, err = db.Pool.Exec(ctx, "INSERT INTO household.space_members(space_id,user_id,role,space_kind) VALUES($1,$2,'member','personal')", sid, other); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(ctx, other, sid, job.ID); statusOf(err) != 404 {
		t.Fatalf("member read other's result %v", err)
	}
}

func TestAIWorkerBoundsProviderRetriesAndRejectsFabricatedChoices(t *testing.T) {
	cases := []struct {
		name        string
		providerErr error
		choices     []Choice
		attempts    int
		code        string
	}{{"temporary", ErrUnavailable, nil, 2, "provider_unavailable"}, {"rejected", ErrRejected, nil, 1, "provider_rejected"}, {"unknown", nil, []Choice{{Key: "r99", Reason: "Invented"}}, 1, "invalid_response"}, {"duplicate", nil, []Choice{{Key: "r01", Reason: "A"}, {Key: "r01", Reason: "B"}}, 1, "invalid_response"}, {"empty", nil, []Choice{}, 1, "invalid_response"}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			s, db, user, sid, _ := setupAI(t, providerFunc(func(context.Context, Prompt) ([]Choice, error) { calls.Add(1); return tc.choices, tc.providerErr }))
			ctx := context.Background()
			job, err := s.Request(ctx, user, sid, aiInput(db))
			if err != nil {
				t.Fatal(err)
			}
			event := aiEvent(t, db, job.ID)
			first := s.Handle(ctx, event)
			if tc.attempts == 2 && !errors.Is(first, ErrUnavailable) {
				t.Fatalf("temporary not retryable: %v", first)
			}
			if tc.attempts == 1 && first != nil {
				t.Fatal(first)
			}
			if err = s.Handle(ctx, event); err != nil {
				t.Fatal(err)
			}
			if err = s.Handle(ctx, event); err != nil {
				t.Fatal(err)
			}
			out, err := s.Get(ctx, user, sid, job.ID)
			if err != nil || out.Status != "failed" || out.ErrorCode != tc.code || calls.Load() != int32(tc.attempts) {
				t.Fatalf("result %+v calls%d err%v", out, calls.Load(), err)
			}
		})
	}
}

func TestAIProviderWaitDoesNotHoldAuthorityAndRevocationPreventsPublish(t *testing.T) {
	for _, mutation := range []string{"remove", "disable", "promote"} {
		t.Run(mutation, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			s, db, user, sid, _ := setupAI(t, providerFunc(func(ctx context.Context, p Prompt) ([]Choice, error) {
				close(entered)
				select {
				case <-release:
					return defaultChoices(ctx, p)
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}))
			ctx := context.Background()
			job, err := s.Request(ctx, user, sid, aiInput(db))
			if err != nil {
				t.Fatal(err)
			}
			event := aiEvent(t, db, job.ID)
			done := make(chan error, 1)
			go func() { done <- s.Handle(ctx, event) }()
			select {
			case <-entered:
			case err := <-done:
				t.Fatalf("worker did not reach provider: %v", err)
			case <-time.After(3 * time.Second):
				t.Fatal("provider was not called")
			}
			updateCtx, cancel := context.WithTimeout(ctx, time.Second)
			defer cancel()
			switch mutation {
			case "remove":
				_, err = db.Pool.Exec(updateCtx, "DELETE FROM household.space_members WHERE space_id=$1 AND user_id=$2", sid, user)
			case "disable":
				_, err = db.Pool.Exec(updateCtx, "UPDATE identity.users SET status='disabled' WHERE id=$1", user)
			case "promote":
				_, err = db.Pool.Exec(updateCtx, "UPDATE identity.users SET system_role='admin' WHERE id=$1", user)
			}
			close(release)
			if err != nil {
				t.Fatalf("provider held authority locks %v", err)
			}
			if err = <-done; err != nil {
				t.Fatal(err)
			}
			var status, code string
			if err = db.Pool.QueryRow(ctx, "SELECT status,error_code FROM mealplan.ai_jobs WHERE id=$1", job.ID).Scan(&status, &code); err != nil {
				t.Fatal(err)
			}
			if status != "failed" || code != "access_revoked" {
				t.Fatalf("revoked published %s/%s", status, code)
			}
			if _, err = s.Get(ctx, user, sid, job.ID); statusOf(err) != 404 {
				t.Fatalf("revoked read %v", err)
			}
		})
	}
}

func TestAICleanupExpiresJobsAndPreservesCurrentDayQuota(t *testing.T) {
	var calls atomic.Int32
	s, db, user, sid, _ := setupAI(t, providerFunc(func(ctx context.Context, p Prompt) ([]Choice, error) { calls.Add(1); return defaultChoices(ctx, p) }))
	ctx := context.Background()
	job, err := s.Request(ctx, user, sid, aiInput(db))
	if err != nil {
		t.Fatal(err)
	}
	event := aiEvent(t, db, job.ID)
	clock := &aiClock{at: db.Clock.Now().Add(24 * time.Hour)}
	db.Clock = clock
	if _, err = s.Get(ctx, user, sid, job.ID); statusOf(err) != 404 {
		t.Fatalf("expired visible %v", err)
	}
	if err = s.Handle(ctx, event); err != nil || calls.Load() != 0 {
		t.Fatalf("expired called provider %v", err)
	}
	if err = s.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	assertAICounts(t, db, 0, 0, 1)
	if err = s.Handle(ctx, event); err != nil {
		t.Fatal(err)
	}
}

type providerFunc func(context.Context, Prompt) ([]Choice, error)

func (f providerFunc) Recommend(ctx context.Context, p Prompt) ([]Choice, error) { return f(ctx, p) }
func defaultChoices(_ context.Context, p Prompt) ([]Choice, error) {
	return []Choice{{Key: p.Candidates[0].Key, Reason: "Tận dụng nguyên liệu sẵn có"}}, nil
}

type aiClock struct{ at time.Time }

func (c *aiClock) Now() time.Time { return c.at }
func seedAIUser(t *testing.T, db *platform.DB) string {
	t.Helper()
	user := platform.ID()
	if _, err := db.Pool.Exec(context.Background(), "INSERT INTO identity.users(id,email,full_name,password_hash,status) VALUES($1,$2,'Người dùng','unused','active')", user, user+"@test.local"); err != nil {
		t.Fatal(err)
	}
	return user
}
func setupAI(t *testing.T, provider Provider) (*Service, *platform.DB, string, string, []string) {
	t.Helper()
	db := testutil.NewDB(t)
	ctx := context.Background()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	if _, err := platform.OpenQueue(ctx, db, nil, key); err != nil {
		t.Fatal(err)
	}
	user := seedAIUser(t, db)
	sid := platform.ID()
	if _, err := db.Pool.Exec(ctx, "INSERT INTO household.spaces(id,kind,name,created_by) VALUES($1,'personal','Nhà',$2)", sid, user); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, "INSERT INTO household.space_members(space_id,user_id,role,space_kind) VALUES($1,$2,'leader','personal')", sid, user); err != nil {
		t.Fatal(err)
	}
	ids := []string{platform.ID(), platform.ID()}
	for i, id := range ids {
		if _, err := db.Pool.Exec(ctx, "INSERT INTO recipe.recipes(id,created_by,name,servings,kind,status) VALUES($1,$2,$3,2,'template','published')", id, user, []string{"Canh rau", "Đậu hấp"}[i]); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Pool.Exec(ctx, "INSERT INTO recipe.recipe_ingredients(id,recipe_id,food_id,unit_id,unit_factor,dimension,quantity) VALUES($1,$2,'00000000-0000-7000-8000-000000000021','00000000-0000-7000-8000-000000000011',1,'mass',100)", platform.ID(), id); err != nil {
			t.Fatal(err)
		}
	}
	return New(db, planning.New(db, inventory.New(db, nil), nil, nil), provider), db, user, sid, ids
}
func aiInput(db *platform.DB) Input {
	return Input{Date: db.Today().Format("2006-01-02"), Servings: 2, Limit: 2}
}
func statusOf(err error) int {
	var e *platform.Error
	if errors.As(err, &e) {
		return e.Status
	}
	return 0
}
func assertAICounts(t *testing.T, db *platform.DB, jobs, quota, events int) {
	t.Helper()
	var a, b, c int
	if err := db.Pool.QueryRow(context.Background(), "SELECT (SELECT count(*) FROM mealplan.ai_jobs),(SELECT coalesce(sum(accepted),0) FROM mealplan.ai_daily_quotas),(SELECT count(*) FROM river_job WHERE args->>'event'='ai.suggest')").Scan(&a, &b, &c); err != nil {
		t.Fatal(err)
	}
	if a != jobs || b != quota || c != events {
		t.Fatalf("jobs/quota/events=%d/%d/%d want%d/%d/%d", a, b, c, jobs, quota, events)
	}
}
func aiEvent(t *testing.T, db *platform.DB, id string) platform.Event {
	t.Helper()
	var raw []byte
	if err := db.Pool.QueryRow(context.Background(), "SELECT args FROM river_job WHERE args->>'event'='ai.suggest' AND args->>'key'=$1", "ai.suggest:"+id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var e platform.Event
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatal(err)
	}
	if !e.Encrypted || strings.Contains(string(raw), "jobId") {
		t.Fatalf("event plaintext %s", raw)
	}
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	cipher, err := platform.NewEventCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	e, err = cipher.Open(e)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestAIConcurrentWorkerDoesNotCallProviderTwice(t *testing.T) {
	var calls atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	s, db, user, sid, _ := setupAI(t, providerFunc(func(ctx context.Context, p Prompt) ([]Choice, error) {
		calls.Add(1)
		close(entered)
		select {
		case <-release:
			return defaultChoices(ctx, p)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}))
	ctx := context.Background()
	job, err := s.Request(ctx, user, sid, aiInput(db))
	if err != nil {
		t.Fatal(err)
	}
	event := aiEvent(t, db, job.ID)
	done := make(chan error, 1)
	go func() { done <- s.Handle(ctx, event) }()
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("worker returned early %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("provider not reached")
	}
	second := s.Handle(ctx, event)
	close(release)
	var snooze *river.JobSnoozeError
	if !errors.As(second, &snooze) || snooze.Duration <= 0 || snooze.Duration > time.Minute {
		t.Fatalf("active lease must defer without consuming Riverattempt: %v", second)
	}
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	var attempts int
	if err = db.Pool.QueryRow(ctx, "SELECT attempts FROM mealplan.ai_jobs WHERE id=$1", job.ID).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || attempts != 1 {
		t.Fatalf("calls%d attempts%d", calls.Load(), attempts)
	}
}

func TestAIBackgroundJobContinuesAfterLogoutButCapturedSessionCannotRequestOrRead(t *testing.T) {
	s, db, user, sid, _ := setupAI(t, providerFunc(defaultChoices))
	ctx := context.Background()
	family := platform.ID()
	if _, err := db.Pool.Exec(ctx, "INSERT INTO identity.refresh_sessions(id,user_id,family_id,token_hash,device_id,platform,expires_at) VALUES($1,$2,$3,$4,$5,'android',$6)", platform.ID(), user, family, []byte("synthetic-session-hash"), platform.ID(), db.Clock.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	captured := platform.WithPrincipal(ctx, platform.Principal{UserID: user, SessionID: family, SystemRole: "user"})
	job, err := s.Request(captured, user, sid, aiInput(db))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, "UPDATE identity.refresh_sessions SET revoked_at=$2 WHERE family_id=$1", family, db.Clock.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Request(captured, user, sid, aiInput(db)); statusOf(err) != 401 {
		t.Fatalf("revoked request %v", err)
	}
	if _, err = s.Get(captured, user, sid, job.ID); statusOf(err) != 401 {
		t.Fatalf("revoked read %v", err)
	}
	if err = s.Handle(ctx, aiEvent(t, db, job.ID)); err != nil {
		t.Fatal(err)
	}
	out, err := s.Get(ctx, user, sid, job.ID)
	if err != nil || out.Status != "ready" {
		t.Fatalf("logout cancelled authorized background job %+v %v", out, err)
	}
	assertAICounts(t, db, 1, 1, 1)
}

func TestAICleanupPreservesTodaysQuotaAndPurgeRespectsOwnerAndSpace(t *testing.T) {
	s, db, user, sid, _ := setupAI(t, providerFunc(defaultChoices))
	ctx := context.Background()
	first, err := s.Request(ctx, user, sid, aiInput(db))
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Request(ctx, user, sid, aiInput(db))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, "UPDATE mealplan.ai_jobs SET created_at=$2,expires_at=$3 WHERE id=$1", first.ID, db.Clock.Now().Add(-time.Hour), db.Clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err = s.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	assertAICounts(t, db, 1, 2, 2)
	if _, err = s.Get(ctx, user, sid, first.ID); statusOf(err) != 404 {
		t.Fatalf("expired job survived %v", err)
	}
	other := seedAIUser(t, db)
	otherSpace := platform.ID()
	if _, err = db.Pool.Exec(ctx, "INSERT INTO household.spaces(id,kind,name,created_by) VALUES($1,'personal','Nhà khác',$2)", otherSpace, other); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, "INSERT INTO household.space_members(space_id,user_id,role,space_kind) VALUES($1,$2,'leader','personal')", otherSpace, other); err != nil {
		t.Fatal(err)
	}
	otherJob, err := s.Request(ctx, other, otherSpace, aiInput(db))
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Tx(ctx, func(tx pgx.Tx) error { return s.PurgeSpace(ctx, tx, sid) }); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Get(ctx, user, sid, second.ID); statusOf(err) != 404 {
		t.Fatalf("space purge did not remove job %v", err)
	}
	if _, err = s.Get(ctx, other, otherSpace, otherJob.ID); err != nil {
		t.Fatalf("space purge deleted foreign job %v", err)
	}
	assertAICounts(t, db, 1, 3, 3)
	if err = db.Tx(ctx, func(tx pgx.Tx) error { return s.PurgeUser(ctx, tx, other) }); err != nil {
		t.Fatal(err)
	}
	assertAICounts(t, db, 0, 2, 3)
}

func TestAIEmptyCandidateSetDoesNotCallProviderOrInventRecipe(t *testing.T) {
	s, db, user, sid, ids := setupAI(t, providerFunc(func(context.Context, Prompt) ([]Choice, error) {
		t.Fatal("no candidates must not call provider")
		return nil, nil
	}))
	ctx := context.Background()
	for _, id := range ids {
		if _, err := db.Pool.Exec(ctx, "UPDATE recipe.recipes SET status='hidden' WHERE id=$1", id); err != nil {
			t.Fatal(err)
		}
	}
	job, err := s.Request(ctx, user, sid, aiInput(db))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Handle(ctx, aiEvent(t, db, job.ID)); err != nil {
		t.Fatal(err)
	}
	out, err := s.Get(ctx, user, sid, job.ID)
	if err != nil || out.Status != "ready" || len(out.Items) != 0 {
		t.Fatalf("empty result %+v %v", out, err)
	}
}

func TestAICompletionQueueFailureCanRetryWithoutWaitingForLease(t *testing.T) {
	var calls atomic.Int32
	s, db, user, sid, _ := setupAI(t, providerFunc(func(ctx context.Context, p Prompt) ([]Choice, error) { calls.Add(1); return defaultChoices(ctx, p) }))
	ctx := context.Background()
	job, err := s.Request(ctx, user, sid, aiInput(db))
	if err != nil {
		t.Fatal(err)
	}
	event := aiEvent(t, db, job.ID)
	original := db.EnqueueFunc
	db.EnqueueFunc = func(ctx context.Context, tx pgx.Tx, kind, key string, payload any, at time.Time) error {
		if err := original(ctx, tx, kind, key, payload, at); err != nil {
			return err
		}
		if kind == "notification" {
			return errors.New("queue unavailable")
		}
		return nil
	}
	if err = s.Handle(ctx, event); err == nil {
		t.Fatal("completion queue failure hidden")
	}
	var status string
	var notices int
	if err = db.Pool.QueryRow(ctx, "SELECT status,(SELECT count(*) FROM river_job WHERE args->>'event'='notification') FROM mealplan.ai_jobs WHERE id=$1", job.ID).Scan(&status, &notices); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || notices != 0 {
		t.Fatalf("partial publish %s notices%d", status, notices)
	}
	db.EnqueueFunc = original
	if err = s.Handle(ctx, event); err != nil {
		t.Fatalf("retry blocked by abandoned lease: %v", err)
	}
	out, err := s.Get(ctx, user, sid, job.ID)
	if err != nil || out.Status != "ready" || calls.Load() != 2 {
		t.Fatalf("retry result %+v calls%d err%v", out, calls.Load(), err)
	}
}

func TestAIMalformedEventsAndIDsAreRejectedBeforeDatabaseWork(t *testing.T) {
	s, db, user, sid, _ := setupAI(t, providerFunc(defaultChoices))
	ctx := context.Background()
	for _, event := range []platform.Event{{Event: "email", Payload: []byte(`{}`)}, {Event: "ai.suggest", Payload: []byte(`{`)}, {Event: "ai.suggest", Payload: []byte(`{"jobId":"bad"}`)}} {
		if err := s.Handle(ctx, event); statusOf(err) != 400 {
			t.Fatalf("malformed event %+v %v", event, err)
		}
	}
	for _, id := range []string{"bad", "zzzzzzzz-zzzz-zzzz-zzzz-zzzzzzzzzzzz"} {
		if _, err := s.Get(ctx, user, sid, id); statusOf(err) != 404 {
			t.Fatalf("invalid ID %v", err)
		}
	}
	if _, err := s.Request(ctx, user, "bad", aiInput(db)); statusOf(err) != 404 {
		t.Fatalf("invalid spaceID %v", err)
	}
	if err := s.Handle(ctx, platform.Event{Event: "ai.suggest", Payload: []byte(`{"jobId":"` + platform.ID() + `"}`)}); err != nil {
		t.Fatalf("deleted job replay %v", err)
	}
}

func TestAIExhaustedLeaseAndUnavailableProviderDoNotExceedDurableAttemptBound(t *testing.T) {
	s, db, user, sid, _ := setupAI(t, providerFunc(func(context.Context, Prompt) ([]Choice, error) {
		t.Fatal("exhausted attempt called provider")
		return nil, nil
	}))
	ctx := context.Background()
	job, err := s.Request(ctx, user, sid, aiInput(db))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, "UPDATE mealplan.ai_jobs SET attempts=2,attempt_token=$2,lease_until=$3 WHERE id=$1", job.ID, platform.ID(), db.Clock.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err = s.Handle(ctx, aiEvent(t, db, job.ID)); err != nil {
		t.Fatal(err)
	}
	out, err := s.Get(ctx, user, sid, job.ID)
	if err != nil || out.Status != "failed" || out.ErrorCode != "provider_unavailable" {
		t.Fatalf("exhausted %+v %v", out, err)
	}
	second, err := s.Request(ctx, user, sid, aiInput(db))
	if err != nil {
		t.Fatal(err)
	}
	s.provider = nil
	event := aiEvent(t, db, second.ID)
	if err = s.Handle(ctx, event); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing runtime provider %v", err)
	}
	if err = s.Handle(ctx, event); err != nil {
		t.Fatal(err)
	}
}

func TestAITransientFailureRecoversOnSecondAttemptWithoutExtraQuotaOrBusinessWrites(t *testing.T) {
	var calls atomic.Int32
	s, db, user, sid, _ := setupAI(t, providerFunc(func(ctx context.Context, p Prompt) ([]Choice, error) {
		if calls.Add(1) == 1 {
			return nil, errors.New("opaque provider credential detail")
		}
		return defaultChoices(ctx, p)
	}))
	ctx := context.Background()
	job, err := s.Request(ctx, user, sid, aiInput(db))
	if err != nil {
		t.Fatal(err)
	}
	event := aiEvent(t, db, job.ID)
	if err = s.Handle(ctx, event); !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "credential") {
		t.Fatalf("unredacted or terminal failure %v", err)
	}
	pending, err := s.Get(ctx, user, sid, job.ID)
	if err != nil || pending.Status != "pending" || pending.ErrorCode != "provider_unavailable" {
		t.Fatalf("pending %+v %v", pending, err)
	}
	if err = s.Handle(ctx, event); err != nil {
		t.Fatal(err)
	}
	ready, err := s.Get(ctx, user, sid, job.ID)
	if err != nil || ready.Status != "ready" || ready.ErrorCode != "" || calls.Load() != 2 {
		t.Fatalf("recovery %+v %v", ready, err)
	}
	assertAICounts(t, db, 1, 1, 1)
	var writes int
	if err = db.Pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM pantry.stock_movements)+(SELECT count(*) FROM mealplan.meal_entries)+(SELECT count(*) FROM shopping.shopping_lists)").Scan(&writes); err != nil {
		t.Fatal(err)
	}
	if writes != 0 {
		t.Fatalf("AI mutated business resources %d", writes)
	}
}

type transientPlanner struct {
	Planner
	fail bool
}

func (p *transientPlanner) Suggest(ctx context.Context, user, sid string, in planning.SuggestionInput) (planning.SuggestionResult, error) {
	if p.fail {
		p.fail = false
		return planning.SuggestionResult{}, platform.Err(503, 5003, "temporary database contention")
	}
	return p.Planner.Suggest(ctx, user, sid, in)
}
func TestAITransientPlannerFailureRemainsRetryable(t *testing.T) {
	s, db, user, sid, _ := setupAI(t, providerFunc(defaultChoices))
	ctx := context.Background()
	job, err := s.Request(ctx, user, sid, aiInput(db))
	if err != nil {
		t.Fatal(err)
	}
	s.planner = &transientPlanner{Planner: s.planner, fail: true}
	event := aiEvent(t, db, job.ID)
	if err = s.Handle(ctx, event); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("temporary planning failure became terminal: %v", err)
	}
	if err = s.Handle(ctx, event); err != nil {
		t.Fatal(err)
	}
	out, err := s.Get(ctx, user, sid, job.ID)
	if err != nil || out.Status != "ready" {
		t.Fatalf("retry %+v %v", out, err)
	}
}
