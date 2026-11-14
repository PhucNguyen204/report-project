//go:build integration

package app

import (
	"bytes"
	"context"
	"dicho/internal/platform"
	"dicho/internal/testutil"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type actorMutationState struct {
	lists, activities, keys, spaces, members, joinAttempts                  int
	reminderDays, devices, seenNotices, images, categories, recipes, audits int
	profileNames                                                            string
}

func actorState(t *testing.T, ctx context.Context, db *platform.DB) actorMutationState {
	t.Helper()
	var state actorMutationState
	if err := db.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM shopping.shopping_lists),(SELECT count(*) FROM platform.activities),(SELECT count(*) FROM platform.idempotency_keys),(SELECT count(*) FROM household.spaces),(SELECT count(*) FROM household.space_members),(SELECT count(*) FROM household.join_attempts),(SELECT coalesce(sum(reminder_days),0) FROM identity.user_settings),(SELECT count(*) FROM identity.devices),(SELECT count(*) FROM notification.notifications WHERE read_at IS NOT NULL),(SELECT count(*) FROM media.objects),(SELECT count(*) FROM catalog.food_categories),(SELECT count(*) FROM recipe.recipes),(SELECT count(*) FROM admin.audit_logs),(SELECT coalesce(string_agg(full_name,'|' ORDER BY id),'') FROM identity.users)`).Scan(&state.lists, &state.activities, &state.keys, &state.spaces, &state.members, &state.joinAttempts, &state.reminderDays, &state.devices, &state.seenNotices, &state.images, &state.categories, &state.recipes, &state.audits, &state.profileNames); err != nil {
		t.Fatal(err)
	}
	return state
}

func revokeCapturedActor(t *testing.T, ctx context.Context, db *platform.DB, p platform.Principal, change string) {
	t.Helper()
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var sql string
	switch change {
	case "promotion":
		sql = "UPDATE identity.users SET system_role='admin',token_version=token_version+1 WHERE id=$1"
	case "disable":
		sql = "UPDATE identity.users SET status='disabled',token_version=token_version+1 WHERE id=$1"
	case "delete":
		sql = "UPDATE identity.users SET deleted_at=now(),token_version=token_version+1 WHERE id=$1"
	case "token-version":
		sql = "UPDATE identity.users SET token_version=token_version+1 WHERE id=$1"
	case "logout-family":
	default:
		t.Fatalf("unknown actor transition %s", change)
	}
	if sql != "" {
		if _, err := tx.Exec(ctx, sql, p.UserID); err != nil {
			t.Fatal(err)
		}
	}
	if change != "token-version" {
		if _, err := tx.Exec(ctx, "UPDATE identity.refresh_sessions SET revoked_at=now(),revoke_reason='fixture' WHERE user_id=$1 AND family_id=$2", p.UserID, p.SessionID); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func pausedActorMutation(t *testing.T, h *apiHarness, ctx context.Context, token, method, path string, body any) (<-chan platform.Principal, func(), <-chan *httptest.ResponseRecorder) {
	t.Helper()
	entered, release := make(chan platform.Principal, 1), make(chan struct{})
	resume := sync.OnceFunc(func() { close(release) })
	t.Cleanup(resume)
	auth := func(ctx context.Context, bearer string) (platform.Principal, error) {
		p, err := h.system.Accounts.Authenticate(ctx, bearer)
		if err != nil {
			return p, err
		}
		entered <- p
		select {
		case <-release:
			return p, nil
		case <-ctx.Done():
			return platform.Principal{}, ctx.Err()
		}
	}
	handler := NewHandler(h.system.DB, auth, Config{}, func(m *http.ServeMux) { m.Handle("/", h.system.Mux) })
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(raw)).WithContext(ctx)
	r.Header.Set("X-App-Version", "1.0.0")
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", platform.ID())
	r.Header.Set("If-Match", "1")
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		done <- w
	}()
	return entered, resume, done
}

func capturedActor(t *testing.T, ctx context.Context, entered <-chan platform.Principal) platform.Principal {
	t.Helper()
	select {
	case p := <-entered:
		return p
	case <-ctx.Done():
		t.Fatal("real authentication did not reach pause")
		return platform.Principal{}
	}
}

func TestCapturedActorRevocationRejectsShoppingMutation(t *testing.T) {
	for _, change := range []string{"promotion", "disable", "delete", "logout-family", "token-version"} {
		t.Run(change, func(t *testing.T) {
			h := newAPIHarness(t)
			token, sid, _ := h.user(t, "actor@revocation.test")
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			before := actorState(t, ctx, h.system.DB)
			entered, resume, done := pausedActorMutation(t, h, ctx, token, "POST", "/it4788/v1/spaces/"+sid+"/shopping-lists", map[string]any{"name": "Stale mutation", "plannedDate": h.system.DB.Today().Format("2006-01-02")})
			p := capturedActor(t, ctx, entered)
			revokeCapturedActor(t, ctx, h.system.DB, p, change)
			if _, err := h.system.Accounts.Authenticate(ctx, token); err == nil {
				t.Fatal("native revocation did not reject a fresh authentication")
			}
			resume()
			select {
			case response := <-done:
				if response.Code < 400 {
					t.Errorf("captured actor mutated after %s: HTTP%d", change, response.Code)
				}
			case <-ctx.Done():
				t.Fatal("request did not finish after authentication resumed")
			}
			if after := actorState(t, ctx, h.system.DB); after != before {
				t.Errorf("revoked request persisted state: before%+v after%+v", before, after)
			}
		})
	}
}

func TestCapturedActorRevocationRejectsGroupCreateAndJoin(t *testing.T) {
	for _, change := range []string{"promotion", "disable", "delete"} {
		for _, operation := range []string{"create", "join"} {
			t.Run(change+"/"+operation, func(t *testing.T) {
				h := newAPIHarness(t)
				token, _, _ := h.user(t, "actor@revocation.test")
				path, body := "/it4788/v1/spaces", map[string]any{"name": "Stale family"}
				if operation == "join" {
					owner, _, _ := h.user(t, "owner@revocation.test")
					group := h.call(t, "POST", "/it4788/v1/spaces", map[string]any{"name": "Invite family"}, owner, "", 201)
					invite := h.call(t, "POST", "/it4788/v1/spaces/"+field(group, "id")+"/invites", nil, owner, "", 201)
					path, body = "/it4788/v1/spaces/join", map[string]any{"code": field(invite, "code")}
				}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				before := actorState(t, ctx, h.system.DB)
				entered, resume, done := pausedActorMutation(t, h, ctx, token, "POST", path, body)
				p := capturedActor(t, ctx, entered)
				revokeCapturedActor(t, ctx, h.system.DB, p, change)
				if _, err := h.system.Accounts.Authenticate(ctx, token); err == nil {
					t.Fatal("native revocation did not reject a fresh authentication")
				}
				resume()
				select {
				case response := <-done:
					if response.Code < 400 {
						t.Errorf("captured actor completed group %s after %s: HTTP%d", operation, change, response.Code)
					}
				case <-ctx.Done():
					t.Fatal("group request did not finish after authentication resumed")
				}
				if after := actorState(t, ctx, h.system.DB); after != before {
					t.Errorf("revoked group request persisted state: before%+v after%+v", before, after)
				}
			})
		}
	}
}

func TestAuthorizedShoppingTransactionSerializesCurrentFamilyLogout(t *testing.T) {
	h := newAPIHarness(t)
	token, sid, _ := h.user(t, "linearized@revocation.test")
	list := h.call(t, "POST", "/it4788/v1/spaces/"+sid+"/shopping-lists", map[string]any{"name": "Locked list", "plannedDate": h.system.DB.Today().Format("2006-01-02")}, token, "", 201)
	listID := field(list, "id")
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	blocker, err := h.system.DB.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(context.WithoutCancel(ctx))
	var locked string
	if err := blocker.QueryRow(ctx, "SELECT id::text FROM shopping.shopping_lists WHERE id=$1 FOR UPDATE", listID).Scan(&locked); err != nil {
		t.Fatal(err)
	}
	entered, resume, done := pausedActorMutation(t, h, ctx, token, "POST", "/it4788/v1/spaces/"+sid+"/shopping-lists/"+listID+"/items", map[string]any{"foodId": "00000000-0000-7000-8000-000000000021", "unitId": "00000000-0000-7000-8000-000000000012", "quantity": "1"})
	p := capturedActor(t, ctx, entered)
	resume()
	responseDone := make(chan *httptest.ResponseRecorder, 1)
	requestEarly := make(chan error, 1)
	go func() {
		select {
		case response := <-done:
			responseDone <- response
			requestEarly <- fmt.Errorf("request finished HTTP%d", response.Code)
		case <-ctx.Done():
		}
	}()
	testutil.WaitForDatabaseLock(t, ctx, h.system.DB, "shopping.shopping_lists", requestEarly)
	logoutDone := make(chan error, 1)
	go func() {
		tx, err := h.system.DB.Pool.Begin(ctx)
		if err != nil {
			logoutDone <- err
			return
		}
		defer tx.Rollback(context.WithoutCancel(ctx))
		if _, err = tx.Exec(ctx, "UPDATE identity.refresh_sessions SET revoked_at=now(),revoke_reason='logout' WHERE user_id=$1 AND family_id=$2", p.UserID, p.SessionID); err == nil {
			err = tx.Commit(ctx)
		}
		logoutDone <- err
	}()
	testutil.WaitForDatabaseLock(t, ctx, h.system.DB, "UPDATE identity.refresh_sessions SET revoked_at", logoutDone)
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case response := <-responseDone:
		if response.Code != 201 {
			t.Fatalf("authorized transaction failed before later logout: HTTP%d %s", response.Code, response.Body)
		}
	case <-ctx.Done():
		t.Fatal("authorized request did not finish after releasing list")
	}
	select {
	case err := <-logoutDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("logout did not finish after authorized transaction")
	}
	if _, err := h.system.Accounts.Authenticate(ctx, token); err == nil {
		t.Fatal("logout did not invalidate subsequent authentication")
	}
	var items int
	if err := h.system.DB.Pool.QueryRow(ctx, "SELECT count(*) FROM shopping.shopping_items WHERE list_id=$1", listID).Scan(&items); err != nil || items != 1 {
		t.Fatalf("authorized item not committed exactly once: items%d err%v", items, err)
	}
}

func TestCapturedLogoutRejectsOwnAndAdministrativeMutations(t *testing.T) {
	for _, name := range []string{"profile", "settings", "device", "notice-read", "avatar-upload", "admin-category", "admin-template"} {
		t.Run(name, func(t *testing.T) {
			h := newAPIHarness(t)
			token, _, user := h.user(t, "actor@revocation.test")
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			if name == "admin-category" || name == "admin-template" {
				if _, err := h.system.DB.Pool.Exec(ctx, "UPDATE identity.users SET system_role='admin',token_version=token_version+1 WHERE id=$1", user); err != nil {
					t.Fatal(err)
				}
				login := h.call(t, "POST", "/it4788/v1/auth/login", map[string]any{"email": "actor@revocation.test", "password": "Password123"}, "", "", 200)
				token = field(login, "accessToken")
			}
			method, path, body := "PATCH", "/it4788/v1/me", map[string]any{"fullName": "Revoked profile"}
			switch name {
			case "settings":
				method, path, body = "PUT", "/it4788/v1/me/settings", map[string]any{"reminderDays": 3}
			case "device":
				method, path, body = "POST", "/it4788/v1/me/devices", map[string]any{"fcmToken": "revocation-device-token", "platform": "android"}
			case "notice-read":
				notice := platform.ID()
				if _, err := h.system.DB.Pool.Exec(ctx, "INSERT INTO notification.notifications(id,user_id,dedupe_key,title,body,type) VALUES($1,$2,$3,'Fixture','Body','activity')", notice, user, "notice:"+notice); err != nil {
					t.Fatal(err)
				}
				method, path, body = "POST", "/it4788/v1/me/notifications/read", map[string]any{"ids": []string{notice}}
			case "avatar-upload":
				method, path, body = "POST", "/it4788/v1/media/upload-urls", map[string]any{"purpose": "avatar", "targetId": user, "contentType": "image/png", "size": 70}
			case "admin-category":
				method, path, body = "POST", "/it4788/v1/admin/food-categories", map[string]any{"name": "Revoked category"}
			case "admin-template":
				method, path, body = "POST", "/it4788/v1/admin/recipe-templates", map[string]any{"name": "Revoked template", "servings": 1, "status": "draft", "ingredients": []any{map[string]any{"foodId": "00000000-0000-7000-8000-000000000021", "unitId": "00000000-0000-7000-8000-000000000012", "quantity": "0.2"}}}
			}
			before := actorState(t, ctx, h.system.DB)
			entered, resume, done := pausedActorMutation(t, h, ctx, token, method, path, body)
			p := capturedActor(t, ctx, entered)
			revokeCapturedActor(t, ctx, h.system.DB, p, "logout-family")
			if _, err := h.system.Accounts.Authenticate(ctx, token); err == nil {
				t.Fatal("logout did not reject a fresh authentication")
			}
			resume()
			select {
			case response := <-done:
				if response.Code < 400 {
					t.Errorf("captured actor completed %s after logout: HTTP%d", name, response.Code)
				}
			case <-ctx.Done():
				t.Fatal("request did not finish after authentication resumed")
			}
			if after := actorState(t, ctx, h.system.DB); after != before {
				t.Errorf("logged-out request persisted state: before%+v after%+v", before, after)
			}
			login := h.call(t, "POST", "/it4788/v1/auth/login", map[string]any{"email": "actor@revocation.test", "password": "Password123"}, "", "", 200)
			status := 200
			if name == "avatar-upload" || name == "admin-category" || name == "admin-template" {
				status = 201
			}
			h.call(t, method, path, body, field(login, "accessToken"), "1", status)
		})
	}
}
