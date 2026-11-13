//go:build integration

package accounts

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func promoteAdmin(t *testing.T, f *authFixture, u map[string]any, email string) string {
	t.Helper()
	_, err := f.db.Pool.Exec(context.Background(), "UPDATE identity.users SET system_role='admin' WHERE id=$1", u["userId"])
	if err != nil {
		t.Fatal(err)
	}
	status, body := f.call(t, "POST", "/it4788/v1/auth/login", map[string]any{"email": email, "password": "ValidPass1"}, "")
	if status != 200 {
		t.Fatal(body)
	}
	return body["data"].(map[string]any)["accessToken"].(string)
}
func TestAdminCannotDisableSelfAndLockSendsEmail(t *testing.T) {
	f := setupAuth(t)
	a := f.activate(t, "admin-a@example.com")
	b := f.activate(t, "admin-b@example.com")
	u := f.activate(t, "disabled@example.com")
	token := promoteAdmin(t, f, a, "admin-a@example.com")
	promoteAdmin(t, f, b, "admin-b@example.com")
	status, body := f.call(t, "PATCH", "/it4788/v1/admin/users/"+a["userId"].(string), map[string]any{"status": "disabled", "lockReason": "Test lock"}, token)
	if status != 422 {
		t.Fatalf("self disabled %d %+v", status, body)
	}
	status, body = f.call(t, "PATCH", "/it4788/v1/admin/users/"+u["userId"].(string), map[string]any{"status": "disabled", "lockReason": "Vi phạm điều khoản"}, token)
	if status != 200 {
		t.Fatalf("disable user %d %+v", status, body)
	}
	var n int
	err := f.db.Pool.QueryRow(context.Background(), "SELECT count(*) FROM river_job WHERE args->>'event'='email' AND args->'payload'->>'subject'='Tài khoản bị khóa' AND args->'payload'->>'to'='disabled@example.com'").Scan(&n)
	if err != nil || n != 1 {
		t.Fatalf("notification email %d %v", n, err)
	}
	if _, err = f.s.Authenticate(context.Background(), u["accessToken"].(string)); err == nil {
		t.Fatal("disabled token kept access")
	}
}
func TestCatalogETagChangesAfterAdminMutation(t *testing.T) {
	f := setupAuth(t)
	a := f.activate(t, "etag@example.com")
	token := promoteAdmin(t, f, a, "etag@example.com")
	get := func(tag string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/it4788/v1/catalog", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("If-None-Match", tag)
		w := httptest.NewRecorder()
		f.router.ServeHTTP(w, r)
		return w
	}
	first := get("")
	if first.Code != 200 {
		t.Fatal(first.Body.String())
	}
	tag := first.Header().Get("ETag")
	if tag == "" || get(tag).Code != 304 {
		t.Fatal("conditional catalog unavailable")
	}
	status, body := f.call(t, "POST", "/it4788/v1/admin/food-categories", map[string]any{"name": "Thịt cá"}, token)
	if status != 201 {
		t.Fatal(body)
	}
	after := get(tag)
	if after.Code != 200 || after.Header().Get("ETag") == tag {
		t.Fatalf("stale catalog ETag %d %s", after.Code, after.Header().Get("ETag"))
	}
	var document map[string]any
	if err := json.NewDecoder(bytes.NewReader(after.Body.Bytes())).Decode(&document); err != nil {
		t.Fatal(err)
	}
	items := document["data"].(map[string]any)["categories"].([]any)
	if len(items) != 3 {
		t.Fatal(items)
	}
}
