//go:build integration

package accounts

import (
	"bytes"
	"context"
	"dicho/internal/platform"
	"dicho/internal/testutil"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRevokedPasswordProofDoesNotChangeSecurityCounters(t *testing.T) {
	for _, change := range []string{"logout", "token-version", "disabled", "promotion"} {
		for _, operation := range []string{"change-password", "delete-account"} {
			t.Run(change+"/"+operation, func(t *testing.T) {
				f := setupAuth(t)
				user := f.activate(t, "password-authority@example.test")
				ctx := context.Background()
				p, err := f.s.Authenticate(ctx, user["accessToken"].(string))
				if err != nil {
					t.Fatal(err)
				}
				query := "UPDATE identity.refresh_sessions SET revoked_at=$2 WHERE family_id=$1"
				arg := p.SessionID
				switch change {
				case "token-version":
					query, arg = "UPDATE identity.users SET token_version=token_version+1 WHERE id=$1", p.UserID
				case "disabled":
					query, arg = "UPDATE identity.users SET status='disabled' WHERE id=$1", p.UserID
				case "promotion":
					query, arg = "UPDATE identity.users SET system_role='admin' WHERE id=$1", p.UserID
				}
				if change == "logout" {
					_, err = f.db.Pool.Exec(ctx, query, arg, f.clock.Now())
				} else {
					_, err = f.db.Pool.Exec(ctx, query, arg)
				}
				if err != nil {
					t.Fatal(err)
				}
				if _, err := f.s.Authenticate(ctx, user["accessToken"].(string)); err == nil {
					t.Fatal("fresh authentication accepted revoked authority")
				}
				mux := http.NewServeMux()
				f.s.RegisterProfile(mux)
				f.s.RegisterPrivacy(mux)
				method, path := "PUT", "/it4788/v1/me/password"
				body := map[string]any{"currentPassword": "IncorrectPass1", "newPassword": "FreshValidPass2"}
				if operation == "delete-account" {
					method, path, body = "DELETE", "/it4788/v1/me", map[string]any{"password": "IncorrectPass1"}
				}
				raw, _ := json.Marshal(body)
				r := httptest.NewRequest(method, path, bytes.NewReader(raw)).WithContext(platform.WithPrincipal(ctx, p))
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, r)
				var failures int
				var locked *time.Time
				if err := f.db.Pool.QueryRow(ctx, "SELECT failed_login_count,locked_until FROM identity.users WHERE id=$1", p.UserID).Scan(&failures, &locked); err != nil {
					t.Fatal(err)
				}
				if w.Code != 401 || failures != 0 || locked != nil {
					t.Fatalf("revoked proof changed security state: HTTP%d failures%d locked%v", w.Code, failures, locked)
				}
			})
		}
	}
}

func TestWrongPasswordProofCommitsCounterOutsideRejectedOuterCommand(t *testing.T) {
	f := setupAuth(t)
	user := f.activate(t, "password-counter-commit@example.test")
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
	joined := platform.WithTransaction(platform.WithPrincipal(ctx, p), outer)
	ok, err := f.s.passwordMatches(joined, p.UserID, "IncorrectPass1")
	if err != nil || ok {
		t.Fatalf("incorrect proof result=%v err=%v", ok, err)
	}
	if err := outer.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var failures int
	if err := f.db.Pool.QueryRow(ctx, "SELECT failed_login_count FROM identity.users WHERE id=$1", p.UserID).Scan(&failures); err != nil || failures != 1 {
		t.Fatalf("legitimate failed proof was rolled back: failures%d err%v", failures, err)
	}
}

func TestPasswordProofBoundsIdentityLockWaitAndSanitizesFailure(t *testing.T) {
	for _, abort := range []string{"lock-timeout", "context-canceled"} {
		t.Run(abort, func(t *testing.T) {
			f := setupAuth(t)
			user := f.activate(t, "password-lock-wait@example.test")
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			p, err := f.s.Authenticate(ctx, user["accessToken"].(string))
			if err != nil {
				t.Fatal(err)
			}
			holder, err := f.db.Pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer holder.Rollback(context.Background())
			if _, err := holder.Exec(ctx, "SELECT id FROM identity.users WHERE id=$1 FOR UPDATE", p.UserID); err != nil {
				t.Fatal(err)
			}
			requestContext, cancelRequest := context.WithCancel(platform.WithPrincipal(ctx, p))
			defer cancelRequest()
			result := make(chan *httptest.ResponseRecorder, 1)
			premature := make(chan error, 1)
			finished := make(chan struct{})
			go func() {
				defer close(finished)
				r := httptest.NewRequest("PUT", "/it4788/v1/me/password", bytes.NewBufferString(`{"currentPassword":"IncorrectPass1","newPassword":"FreshValidPass2"}`)).WithContext(requestContext)
				w := httptest.NewRecorder()
				platform.Handle(f.s.changePassword)(w, r)
				result <- w
				premature <- fmt.Errorf("password proof completed with HTTP%d", w.Code)
			}()
			defer func() {
				_ = holder.Rollback(context.Background())
				cancelRequest()
				select {
				case <-finished:
				case <-time.After(time.Second):
					t.Error("password proof did not stop after releasing identity")
				}
			}()
			testutil.WaitForDatabaseLock(t, ctx, f.db, "SELECT password_hash FROM identity.users", premature)
			if abort == "context-canceled" {
				cancelRequest()
			}
			select {
			case w := <-result:
				var body struct {
					Code    int    `json:"code"`
					Message string `json:"message"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if w.Code != 500 || body.Code != 5000 || body.Message != "Lỗi hệ thống" {
					t.Fatalf("unsafe lock failure response: HTTP%d %s", w.Code, w.Body.String())
				}
			case <-time.After(5 * time.Second):
				t.Fatal("password proof remained blocked on identity instead of bounding the lock wait")
			}
			var failures int
			if err := f.db.Pool.QueryRow(ctx, "SELECT failed_login_count FROM identity.users WHERE id=$1", p.UserID).Scan(&failures); err != nil || failures != 0 {
				t.Fatalf("aborted proof changed security counter: failures%d err%v", failures, err)
			}
		})
	}
}
