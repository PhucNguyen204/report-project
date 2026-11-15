//go:build integration

package accounts

import (
	"bytes"
	"context"
	"dicho/internal/platform"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func inviteFamily(t *testing.T, f *authFixture) (map[string]any, string, string) {
	t.Helper()
	leader := f.activate(t, "invite-leader@example.test")
	status, out := f.call(t, "POST", "/it4788/v1/spaces", map[string]any{"name": "Gia đình thử nghiệm"}, leader["accessToken"].(string))
	if status != 201 {
		t.Fatalf("create family%d %v", status, out)
	}
	sid := out["data"].(map[string]any)["id"].(string)
	return leader, sid, "/it4788/v1/spaces/" + sid
}
func issueInvite(t *testing.T, f *authFixture, base, token string) string {
	t.Helper()
	status, out := f.call(t, "POST", base+"/invites", map[string]any{}, token)
	if status != 201 {
		t.Fatalf("issue invite%d %v", status, out)
	}
	return out["data"].(map[string]any)["code"].(string)
}
func inviteJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestCurrentInviteReadPreservesActiveExpiredAndRevokedState(t *testing.T) {
	f := setupAuth(t)
	leader, sid, base := inviteFamily(t, f)
	token := leader["accessToken"].(string)
	status, out := f.call(t, "GET", base+"/invites/current", nil, token)
	if status != 200 {
		t.Fatalf("empty current invite HTTP%d %v", status, out)
	}
	data := out["data"].(map[string]any)
	if data["status"] != "none" || data["code"] != nil || data["expiresAt"] != nil || data["link"] != nil || data["memberCount"] != float64(1) || data["maxMembers"] != float64(10) {
		t.Fatalf("invalid empty state %v", data)
	}
	code := issueInvite(t, f, base, token)
	var original time.Time
	if err := f.db.Pool.QueryRow(context.Background(), "SELECT expires_at FROM household.invite_codes WHERE space_id=$1 AND revoked_at IS NULL", sid).Scan(&original); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		status, out = f.call(t, "GET", base+"/invites/current", nil, token)
		data = out["data"].(map[string]any)
		if status != 200 || data["status"] != "active" || data["code"] != code || data["link"] != "https://dicho.example/join/"+code {
			t.Fatalf("active state changed HTTP%d %v", status, data)
		}
	}
	var count int
	var expiry time.Time
	if err := f.db.Pool.QueryRow(context.Background(), "SELECT count(*),max(expires_at) FROM household.invite_codes WHERE space_id=$1", sid).Scan(&count, &expiry); err != nil || count != 1 || !expiry.Equal(original) {
		t.Fatalf("read rotated or extended invite count%d expiry%v %v", count, expiry, err)
	}
	if _, err := f.db.Pool.Exec(context.Background(), "UPDATE household.invite_codes SET expires_at=$2 WHERE space_id=$1", sid, f.clock.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	status, out = f.call(t, "GET", base+"/invites/current", nil, token)
	data = out["data"].(map[string]any)
	if status != 200 || data["status"] != "expired" || data["code"] != code {
		t.Fatalf("expired invite revived HTTP%d %v", status, data)
	}
	status, _ = f.call(t, "DELETE", base+"/invites/current", nil, token)
	if status != 200 {
		t.Fatalf("revoke HTTP%d", status)
	}
	status, out = f.call(t, "GET", base+"/invites/current", nil, token)
	data = out["data"].(map[string]any)
	if status != 200 || data["status"] != "none" || data["code"] != nil || data["expiresAt"] != nil || data["link"] != nil {
		t.Fatalf("revoked invite exposed HTTP%d %v", status, data)
	}
}
func TestFullGroupCannotReadOrRotateInviteAndCanStillRevoke(t *testing.T) {
	f := setupAuth(t)
	leader, sid, base := inviteFamily(t, f)
	token := leader["accessToken"].(string)
	member := f.activate(t, "invite-member@example.test")
	code := issueInvite(t, f, base, token)
	if _, err := f.db.Pool.Exec(context.Background(), "UPDATE admin.system_configs SET value='2' WHERE key='group.maxMembers'"); err != nil {
		t.Fatal(err)
	}
	status, out := f.call(t, "POST", "/it4788/v1/spaces/join", map[string]any{"code": code}, member["accessToken"].(string))
	if status != 200 {
		t.Fatalf("join%d %v", status, out)
	}
	for _, method := range []string{"POST", "GET"} {
		path := base + "/invites"
		if method == "GET" {
			path += "/current"
		}
		status, out = f.call(t, method, path, map[string]any{}, token)
		if status != 422 {
			t.Fatalf("full group %s HTTP%d %v", method, status, out)
		}
	}
	var n int
	var live string
	if err := f.db.Pool.QueryRow(context.Background(), "SELECT count(*),max(code) FILTER(WHERE revoked_at IS NULL) FROM household.invite_codes WHERE space_id=$1", sid).Scan(&n, &live); err != nil || n != 1 || live != code {
		t.Fatalf("capacity failure changed invite %d %s %v", n, live, err)
	}
	status, _ = f.call(t, "DELETE", base+"/invites/current", nil, token)
	if status != 200 {
		t.Fatalf("full group revoke HTTP%d", status)
	}
}
func TestCurrentInviteRequiresCurrentFamilyLeader(t *testing.T) {
	f := setupAuth(t)
	leader, _, base := inviteFamily(t, f)
	token := leader["accessToken"].(string)
	member := f.activate(t, "invite-member@example.test")
	outsider := f.activate(t, "invite-outsider@example.test")
	code := issueInvite(t, f, base, token)
	if status, out := f.call(t, "POST", "/it4788/v1/spaces/join", map[string]any{"code": code}, member["accessToken"].(string)); status != 200 {
		t.Fatalf("join%d %v", status, out)
	}
	for _, tc := range []struct {
		token string
		want  int
	}{{member["accessToken"].(string), 403}, {outsider["accessToken"].(string), 404}} {
		status, out := f.call(t, "GET", base+"/invites/current", nil, tc.token)
		if status != tc.want || strings.Contains(string(inviteJSON(t, out)), code) {
			t.Fatalf("unauthorized invite HTTP%d %v", status, out)
		}
	}
	if status, out := f.call(t, "POST", base+"/leader-transfer", map[string]any{"userId": member["userId"]}, token); status != 200 {
		t.Fatalf("transfer%d %v", status, out)
	}
	status, _ := f.call(t, "GET", base+"/invites/current", nil, token)
	if status != 403 {
		t.Fatalf("former leader HTTP%d", status)
	}
	status, out := f.call(t, "GET", base+"/invites/current", nil, member["accessToken"].(string))
	if status != 200 || out["data"].(map[string]any)["code"] != code {
		t.Fatalf("new leader read%d %v", status, out)
	}
}
func TestJoinPreviewNormalizesWhitespaceAndShowsLeaderAndCapacity(t *testing.T) {
	f := setupAuth(t)
	leader, sid, base := inviteFamily(t, f)
	member := f.activate(t, "join-preview@example.test")
	if _, err := f.db.Pool.Exec(context.Background(), "UPDATE identity.users SET full_name='Trưởng nhóm Lan' WHERE id=$1", leader["userId"]); err != nil {
		t.Fatal(err)
	}
	code := issueInvite(t, f, base, leader["accessToken"].(string))
	spaced := " \t" + strings.ToLower(code[:4]) + "\u2003" + strings.ToLower(code[4:]) + "\n"
	status, out := f.call(t, "POST", "/it4788/v1/spaces/join", map[string]any{"code": spaced, "dryRun": true}, member["accessToken"].(string))
	if status != 200 {
		t.Fatalf("normalized preview HTTP%d %v", status, out)
	}
	data := out["data"].(map[string]any)
	if data["id"] != sid || data["name"] != "Gia đình thử nghiệm" || data["leaderName"] != "Trưởng nhóm Lan" || data["memberCount"] != float64(1) || data["maxMembers"] != float64(10) || data["role"] != "" || data["joinedAt"] != nil {
		t.Fatalf("preview metadata %v", data)
	}
	var count int
	if err := f.db.Pool.QueryRow(context.Background(), "SELECT count(*) FROM household.space_members WHERE space_id=$1", sid).Scan(&count); err != nil || count != 1 {
		t.Fatalf("preview mutated membership%d %v", count, err)
	}
}
func TestJoinRejectsMalformedCodeAndStillCountsQuota(t *testing.T) {
	f := setupAuth(t)
	user := f.activate(t, "join-format@example.test")
	for _, code := range []string{"ABCDEFG", "ABCDEFGHJ", "ABCDOFGH", "ABCDIFGH", "ABCD0FGH", "ABCD1FGH", "ABCDÉFGH", "ABCD-FGH", ""} {
		status, out := f.call(t, "POST", "/it4788/v1/spaces/join", map[string]any{"code": code, "dryRun": true}, user["accessToken"].(string))
		if status != 400 {
			t.Fatalf("malformed %q HTTP%d %v", code, status, out)
		}
	}
	var hits int
	if err := f.db.Pool.QueryRow(context.Background(), "SELECT hits FROM household.join_attempts WHERE user_id=$1", user["userId"]).Scan(&hits); err != nil || hits != 9 {
		t.Fatalf("malformed attempts uncounted %d %v", hits, err)
	}
}
func TestJoinExistingMembershipReturnsStableDetailsWithoutMoreEffects(t *testing.T) {
	f := setupAuth(t)
	leader, sid, base := inviteFamily(t, f)
	member := f.activate(t, "join-existing@example.test")
	code := issueInvite(t, f, base, leader["accessToken"].(string))
	if _, err := f.db.Pool.Exec(context.Background(), "UPDATE admin.system_configs SET value='2' WHERE key='group.maxMembers'"); err != nil {
		t.Fatal(err)
	}
	status, out := f.call(t, "POST", "/it4788/v1/spaces/join", map[string]any{"code": code}, member["accessToken"].(string))
	if status != 200 {
		t.Fatalf("initial join%d %v", status, out)
	}
	first := out["data"].(map[string]any)
	if first["role"] != "member" || first["joinedAt"] == nil || first["leaderName"] != "Nguyễn An" || first["maxMembers"] != float64(2) {
		t.Fatalf("confirm metadata %v", first)
	}
	var activities, notices int
	if err := f.db.Pool.QueryRow(context.Background(), "SELECT (SELECT count(*) FROM platform.activities WHERE space_id=$1),(SELECT count(*) FROM river_job WHERE args->>'event'='notification')", sid).Scan(&activities, &notices); err != nil {
		t.Fatal(err)
	}
	for _, dry := range []bool{false, true} {
		status, out = f.call(t, "POST", "/it4788/v1/spaces/join", map[string]any{"code": code, "dryRun": dry}, member["accessToken"].(string))
		if status != 200 {
			t.Fatalf("existing request%d %v", status, out)
		}
		current := out["data"].(map[string]any)
		if current["role"] != "member" || current["joinedAt"] != first["joinedAt"] || current["memberCount"] != float64(2) {
			t.Fatalf("membership changed %v", current)
		}
	}
	var n, a, j int
	if err := f.db.Pool.QueryRow(context.Background(), "SELECT (SELECT count(*) FROM household.space_members WHERE space_id=$1),(SELECT count(*) FROM platform.activities WHERE space_id=$1),(SELECT count(*) FROM river_job WHERE args->>'event'='notification')", sid).Scan(&n, &a, &j); err != nil || n != 2 || a != activities || j != notices {
		t.Fatalf("duplicated effects members%d activities%d notices%d %v", n, a, j, err)
	}
}
func TestRevokedJoinAuthorityCannotChangeAttemptCounter(t *testing.T) {
	for _, change := range []string{"logout", "token-version", "disabled", "promotion"} {
		t.Run(change, func(t *testing.T) {
			f := setupAuth(t)
			user := f.activate(t, "join-authority@example.test")
			ctx := context.Background()
			p, err := f.s.Authenticate(ctx, user["accessToken"].(string))
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "logout":
				_, err = f.db.Pool.Exec(ctx, "UPDATE identity.refresh_sessions SET revoked_at=$2 WHERE family_id=$1", p.SessionID, f.clock.Now())
			case "token-version":
				_, err = f.db.Pool.Exec(ctx, "UPDATE identity.users SET token_version=token_version+1 WHERE id=$1", p.UserID)
			case "disabled":
				_, err = f.db.Pool.Exec(ctx, "UPDATE identity.users SET status='disabled' WHERE id=$1", p.UserID)
			case "promotion":
				_, err = f.db.Pool.Exec(ctx, "UPDATE identity.users SET system_role='admin' WHERE id=$1", p.UserID)
			}
			if err != nil {
				t.Fatal(err)
			}
			mux := http.NewServeMux()
			f.s.RegisterGroups(mux)
			r := httptest.NewRequest("POST", "/it4788/v1/spaces/join", bytes.NewReader(inviteJSON(t, map[string]any{"code": "ABCDEFGH", "dryRun": true}))).WithContext(platform.WithPrincipal(ctx, p))
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			var n int
			if err := f.db.Pool.QueryRow(ctx, "SELECT count(*) FROM household.join_attempts WHERE user_id=$1", p.UserID).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if w.Code != 401 || n != 0 {
				t.Fatalf("revoked request changed counter HTTP%d rows%d %s", w.Code, n, w.Body)
			}
		})
	}
}

func TestJoinCounterCommitsOutsideRejectedOuterCommand(t *testing.T) {
	f := setupAuth(t)
	user := f.activate(t, "join-counter-commit@example.test")
	ctx := context.Background()
	p, err := f.s.Authenticate(ctx, user["accessToken"].(string))
	if err != nil {
		t.Fatal(err)
	}
	outer, err := f.db.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer outer.Rollback(context.Background())
	if err := f.s.joinAttempt(platform.WithTransaction(platform.WithPrincipal(ctx, p), outer), p.UserID); err != nil {
		t.Fatal(err)
	}
	if err := outer.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var hits int
	if err := f.db.Pool.QueryRow(ctx, "SELECT hits FROM household.join_attempts WHERE user_id=$1", p.UserID).Scan(&hits); err != nil || hits != 1 {
		t.Fatalf("legitimate failure counter rolled back%d %v", hits, err)
	}
}

func TestInviteWaitRechecksLeaderSessionAndCapacity(t *testing.T) {
	for _, change := range []string{"capacity", "logout", "leader-transfer"} {
		t.Run(change, func(t *testing.T) {
			f := setupAuth(t)
			leader, sid, base := inviteFamily(t, f)
			member := f.activate(t, "invite-wait-member@example.test")
			code := issueInvite(t, f, base, leader["accessToken"].(string))
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			p, err := f.s.Authenticate(ctx, leader["accessToken"].(string))
			if err != nil {
				t.Fatal(err)
			}
			lock, err := f.db.Pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Rollback(context.Background())
			if _, err := lock.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", sid); err != nil {
				t.Fatal(err)
			}
			mux := http.NewServeMux()
			f.s.RegisterGroups(mux)
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				method, path := "GET", base+"/invites/current"
				if change == "capacity" {
					method, path = "POST", base+"/invites"
				}
				r := httptest.NewRequest(method, path, strings.NewReader(`{}`)).WithContext(platform.WithPrincipal(ctx, p))
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, r)
				done <- w
			}()
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for {
				var waiting bool
				if err := f.db.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND strpos(query,'pg_advisory_xact_lock')>0)").Scan(&waiting); err != nil {
					t.Fatal(err)
				}
				if waiting {
					break
				}
				select {
				case w := <-done:
					t.Fatalf("request finished before required advisorywait HTTP%d", w.Code)
				case <-ctx.Done():
					t.Fatal("missing advisorywait")
				case <-ticker.C:
				}
			}
			want := 403
			switch change {
			case "capacity":
				want = 422
				if _, err := lock.Exec(ctx, "UPDATE admin.system_configs SET value='2' WHERE key='group.maxMembers'"); err != nil {
					t.Fatal(err)
				}
				if _, err := lock.Exec(ctx, "INSERT INTO household.space_members(space_id,user_id,role,space_kind) VALUES($1,$2,'member','family')", sid, member["userId"]); err != nil {
					t.Fatal(err)
				}
			case "logout":
				want = 401
				if _, err := lock.Exec(ctx, "UPDATE identity.refresh_sessions SET revoked_at=$2 WHERE family_id=$1", p.SessionID, f.clock.Now()); err != nil {
					t.Fatal(err)
				}
			case "leader-transfer":
				if _, err := lock.Exec(ctx, "UPDATE household.space_members SET role='member' WHERE space_id=$1 AND user_id=$2", sid, p.UserID); err != nil {
					t.Fatal(err)
				}
				if _, err := lock.Exec(ctx, "INSERT INTO household.space_members(space_id,user_id,role,space_kind) VALUES($1,$2,'leader','family')", sid, member["userId"]); err != nil {
					t.Fatal(err)
				}
			}
			if err := lock.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case w := <-done:
				if w.Code != want || strings.Contains(w.Body.String(), code) {
					t.Fatalf("authority/capacity changed while waiting HTTP%d want%d %s", w.Code, want, w.Body)
				}
			case <-ctx.Done():
				t.Fatal("request did not finish after lock release")
			}
			var live string
			var total int
			if err := f.db.Pool.QueryRow(ctx, "SELECT max(code) FILTER(WHERE revoked_at IS NULL),count(*) FROM household.invite_codes WHERE space_id=$1", sid).Scan(&live, &total); err != nil || live != code || total != 1 {
				t.Fatalf("rejected request rotated invite %d %s %v", total, live, err)
			}
		})
	}
}

func TestCurrentInviteRejectsAdministratorAndPersonalSpace(t *testing.T) {
	f := setupAuth(t)
	leader, _, base := inviteFamily(t, f)
	admin := makeAdmin(t, f, "invite-admin@example.test")
	if status, out := f.call(t, "GET", base+"/invites/current", nil, admin); status != 404 {
		t.Fatalf("admin read invite%d %v", status, out)
	}
	personal := "/it4788/v1/spaces/" + leader["spaceId"].(string) + "/invites/current"
	if status, out := f.call(t, "GET", personal, nil, leader["accessToken"].(string)); status != 422 {
		t.Fatalf("personal invite%d %v", status, out)
	}
}
