//go:build integration

package app

import (
	"context"
	"dicho/internal/platform"
	"dicho/internal/testutil"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFavoriteSpaceRecipeRejectsCurrentAdministratorWithFormerMembership(t *testing.T) {
	h := newAPIHarness(t)
	token, sid, user := h.user(t, "favorite-role@example.test")
	ctx := context.Background()
	rid := platform.ID()
	if _, err := h.system.DB.Pool.Exec(ctx, "INSERT INTO recipe.recipes(id,space_id,created_by,name,servings,kind,status) VALUES($1,$2,$3,'Private recipe',1,'space','published')", rid, sid, user); err != nil {
		t.Fatal(err)
	}
	h.call(t, "PUT", "/it4788/v1/me/favorite-recipes/"+rid, nil, token, "", 200)
	if _, err := h.system.DB.Pool.Exec(ctx, "UPDATE identity.users SET system_role='admin',token_version=token_version+1 WHERE id=$1", user); err != nil {
		t.Fatal(err)
	}
	login := h.call(t, "POST", "/it4788/v1/auth/login", map[string]string{"email": "favorite-role@example.test", "password": "Password123"}, "", "", 200)
	adminToken := field(login, "accessToken")
	for _, method := range []string{"PUT", "DELETE"} {
		h.call(t, method, "/it4788/v1/me/favorite-recipes/"+rid, nil, adminToken, "", 404)
	}
	var count int
	if err := h.system.DB.Pool.QueryRow(ctx, "SELECT count(*) FROM recipe.favorite_recipes WHERE user_id=$1 AND recipe_id=$2", user, rid).Scan(&count); err != nil || count != 1 {
		t.Fatalf("administrator changed private favorite: count%d err%v", count, err)
	}
	template := platform.ID()
	if _, err := h.system.DB.Pool.Exec(ctx, "INSERT INTO recipe.recipes(id,created_by,name,servings,kind,status) VALUES($1,$2,'Global template',1,'template','published')", template, user); err != nil {
		t.Fatal(err)
	}
	h.call(t, "PUT", "/it4788/v1/me/favorite-recipes/"+template, nil, adminToken, "", 200)
}

func TestFavoriteHoldsCurrentFamilyUntilAuthorizedMutationCommits(t *testing.T) {
	h := newAPIHarness(t)
	token, _, user := h.user(t, "favorite-logout@example.test")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	p, err := h.system.Accounts.Authenticate(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	rid := platform.ID()
	if _, err := h.system.DB.Pool.Exec(ctx, "INSERT INTO recipe.recipes(id,created_by,name,servings,kind,status) VALUES($1,$2,'Global template',1,'template','published')", rid, user); err != nil {
		t.Fatal(err)
	}
	blocker, err := h.system.DB.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := blocker.Exec(ctx, "SELECT id FROM recipe.recipes WHERE id=$1 FOR UPDATE", rid); err != nil {
		blocker.Rollback(context.Background())
		t.Fatal(err)
	}
	r := httptest.NewRequest("PUT", "/it4788/v1/me/favorite-recipes/"+rid, nil).WithContext(ctx)
	r.Header.Set("X-App-Version", "1.0.0")
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Idempotency-Key", platform.ID())
	w := httptest.NewRecorder()
	done := make(chan error, 1)
	finished := make(chan struct{})
	go func() { h.system.Handler.ServeHTTP(w, r); done <- nil; close(finished) }()
	defer func() {
		_ = blocker.Rollback(context.Background())
		select {
		case <-finished:
		case <-ctx.Done():
		}
	}()
	testutil.WaitForDatabaseLock(t, ctx, h.system.DB, "recipe", done)
	revoked := make(chan error, 1)
	go func() {
		_, err := h.system.DB.Pool.Exec(ctx, "UPDATE identity.refresh_sessions SET revoked_at=$3,revoke_reason='logout' WHERE user_id=$1 AND family_id=$2", user, p.SessionID, h.system.DB.Clock.Now())
		revoked <- err
	}()
	testutil.WaitForDatabaseLock(t, ctx, h.system.DB, "UPDATE identity.refresh_sessions", revoked)
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	case <-ctx.Done():
		t.Fatal("favorite did not complete")
	}
	select {
	case err := <-revoked:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("logout did not complete")
	}
	var count int
	if err := h.system.DB.Pool.QueryRow(ctx, "SELECT count(*) FROM recipe.favorite_recipes WHERE user_id=$1 AND recipe_id=$2", user, rid).Scan(&count); err != nil || count != 1 || w.Code != 200 {
		t.Fatalf("authorized favorite lost: status%d rows%d err%v", w.Code, count, err)
	}
	if _, err := h.system.Accounts.Authenticate(ctx, token); err == nil {
		t.Fatal("logout failed to revoke token")
	}
}
