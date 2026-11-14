//go:build integration

package accounts

import (
	"context"
	"dicho/internal/testutil"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLogoutWaitsForIdentityBeforeChangingCurrentSession(t *testing.T) {
	f := setupAuth(t)
	user := f.activate(t, "logout-order@example.test")
	token := user["accessToken"].(string)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	holder, err := f.db.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback(context.Background())
	var uid string
	if err := holder.QueryRow(ctx, "SELECT id::text FROM identity.users WHERE id=$1 FOR SHARE", user["userId"]).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	result := make(chan int, 1)
	premature := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		r := httptest.NewRequest("POST", "/it4788/v1/auth/logout", nil).WithContext(ctx)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		f.router.ServeHTTP(w, r)
		result <- w.Code
		premature <- fmt.Errorf("logout completed with HTTP%d", w.Code)
	}()
	defer func() {
		_ = holder.Rollback(context.Background())
		cancel()
		select {
		case <-finished:
		case <-time.After(time.Second):
			t.Error("logout did not stop after releasing identity")
		}
	}()
	testutil.WaitForDatabaseLock(t, ctx, f.db, "SELECT id::text FROM identity.users", premature)
	if err := holder.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case status := <-result:
		if status != 200 {
			t.Fatalf("logout HTTP%d", status)
		}
	case <-ctx.Done():
		t.Fatal("logout did not complete after identity unlocked")
	}
	if _, err := f.s.Authenticate(ctx, token); err == nil {
		t.Fatal("logout did not invalidate access token")
	}
}
