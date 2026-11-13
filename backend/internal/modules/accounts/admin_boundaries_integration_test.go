//go:build integration

package accounts

import (
	"context"
	"testing"
)

func TestAdminUserEditsValidateRolesProtectLastAdminAndRevokePromotedUser(t *testing.T) {
	f := setupAuth(t)
	u := f.activate(t, "user-edit@example.com")
	adminData := f.activate(t, "last-admin@example.com")
	token := promoteAdmin(t, f, adminData, "last-admin@example.com")
	path := "/it4788/v1/admin/users/" + u["userId"].(string)
	for _, in := range []map[string]any{{}, {"status": "pending"}, {"systemRole": "owner"}, {"status": "disabled"}} {
		status, _ := f.call(t, "PATCH", path, in, token)
		if status != 400 {
			t.Fatalf("invalid user edit %v got%d", in, status)
		}
	}
	status, _ := f.call(t, "PATCH", "/it4788/v1/admin/users/"+adminData["userId"].(string), map[string]any{"systemRole": "user"}, token)
	if status != 422 {
		t.Fatalf("last admin demoted %d", status)
	}
	status, body := f.call(t, "PATCH", path, map[string]any{"systemRole": "admin"}, token)
	if status != 200 {
		t.Fatal(body)
	}
	if _, err := f.s.Authenticate(context.Background(), u["accessToken"].(string)); err == nil {
		t.Fatal("role change kept old token")
	}
	status, body = f.call(t, "PATCH", path, map[string]any{"status": "active"}, token)
	if status != 200 {
		t.Fatal(body)
	}
	user := f.activate(t, "admin-forbidden@example.com")
	ut := user["accessToken"].(string)
	for _, route := range []struct {
		method, path string
		body         any
	}{{"GET", "/admin/configs", nil}, {"PUT", "/admin/configs", map[string]any{"values": map[string]int{"reminder.defaultDays": 4}}}, {"GET", "/admin/audit-logs", nil}, {"GET", "/admin/users", nil}, {"PATCH", "/admin/users/" + u["userId"].(string), map[string]any{"status": "active"}}, {"POST", "/admin/foods/00000000-0000-7000-8000-000000000021/review", map[string]any{"action": "approve"}}} {
		status, _ = f.call(t, route.method, "/it4788/v1"+route.path, route.body, ut)
		if status != 403 {
			t.Fatalf("normal user %s %d", route.path, status)
		}
	}
	for _, route := range []struct{ method, path string }{{"PUT", "/admin/configs"}, {"PATCH", "/admin/users/" + u["userId"].(string)}, {"POST", "/admin/foods"}, {"POST", "/admin/foods/00000000-0000-7000-8000-000000000021/review"}} {
		status, _ = f.call(t, route.method, "/it4788/v1"+route.path, []int{1}, token)
		if status != 400 {
			t.Fatalf("malformed %s %d", route.path, status)
		}
	}
}
