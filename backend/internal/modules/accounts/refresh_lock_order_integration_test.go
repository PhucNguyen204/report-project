//go:build integration

package accounts

import (
	"bytes"
	"context"
	"dicho/internal/testutil"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRefreshWaitsForIdentityBeforeLockingCurrentSession(t *testing.T) {
	f := setupAuth(t)
	user := f.activate(t, "refresh-order@example.test")
	uid := user["userId"].(string)
	refreshToken := user["refreshToken"].(string)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var session, family string
	if err := f.db.Pool.QueryRow(ctx, "SELECT id::text,family_id::text FROM identity.refresh_sessions WHERE token_hash=$1", secretHash(refreshToken)).Scan(&session, &family); err != nil {
		t.Fatal(err)
	}
	holder, err := f.db.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback(context.Background())
	var locked string
	if err := holder.QueryRow(ctx, "SELECT id::text FROM identity.users WHERE id=$1 FOR SHARE", uid).Scan(&locked); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]string{"refreshToken": refreshToken, "deviceId": user["deviceId"].(string)})
	if err != nil {
		t.Fatal(err)
	}
	type response struct {
		status int
		body   []byte
	}
	result := make(chan response, 1)
	premature := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		r := httptest.NewRequest("POST", "/it4788/v1/auth/refresh", bytes.NewReader(body)).WithContext(ctx)
		w := httptest.NewRecorder()
		f.router.ServeHTTP(w, r)
		result <- response{w.Code, w.Body.Bytes()}
		premature <- fmt.Errorf("refresh completed with HTTP%d", w.Code)
	}()
	defer func() {
		_ = holder.Rollback(context.Background())
		cancel()
		select {
		case <-finished:
		case <-time.After(time.Second):
			t.Error("refresh request did not stop after releasing its blocker")
		}
	}()
	testutil.WaitForDatabaseLock(t, ctx, f.db, "FROM identity.users", premature)
	if err := holder.QueryRow(ctx, "SELECT id::text FROM identity.refresh_sessions WHERE id=$1 FOR SHARE NOWAIT", session).Scan(&locked); err != nil {
		t.Fatalf("refresh locked its session before acquiring identity: %v", err)
	}
	if err := holder.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case out := <-result:
		if out.status != 200 {
			t.Fatalf("refresh after releasing identity returned HTTP%d", out.status)
		}
		var decoded struct {
			Data struct {
				AccessToken string `json:"accessToken"`
			} `json:"data"`
		}
		if err := json.Unmarshal(out.body, &decoded); err != nil {
			t.Fatal(err)
		}
		principal, err := f.s.Authenticate(ctx, decoded.Data.AccessToken)
		if err != nil || principal.UserID != uid || principal.SessionID != family {
			t.Fatalf("rotated access token lost its valid identity/session family: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("refresh did not finish after identity was unlocked")
	}
}
