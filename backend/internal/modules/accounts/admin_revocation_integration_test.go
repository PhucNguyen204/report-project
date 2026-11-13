//go:build integration

package accounts

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"dicho/internal/platform"
	"dicho/internal/testutil"
)

func revocationUser(t *testing.T, db *platform.DB, role string) string {
	t.Helper()
	id := platform.ID()
	if _, err := db.Pool.Exec(context.Background(), "INSERT INTO identity.users(id,email,full_name,password_hash,status,system_role) VALUES($1,$2,'Review admin','unused','active',$3)", id, id+"@review.test", role); err != nil {
		t.Fatal(err)
	}
	return id
}
func adminCapturedRequest(mux *http.ServeMux, ctx context.Context, actor, method, path, body, version string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("If-Match", version)
	r = r.WithContext(platform.WithPrincipal(ctx, platform.Principal{UserID: actor, SystemRole: "admin"}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func TestDemotedAdministratorCannotRegrantOwnRoleAfterWaiting(t *testing.T) {
	db := testutil.NewDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	actor := revocationUser(t, db, "admin")
	revocationUser(t, db, "admin")
	blocker, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(context.Background())
	if _, err = blocker.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('admin_roles'))"); err != nil {
		t.Fatal(err)
	}
	s := New(db, nil)
	mux := http.NewServeMux()
	s.RegisterAdmin(mux)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- adminCapturedRequest(mux, ctx, actor, "PATCH", "/it4788/v1/admin/users/"+actor, `{"systemRole":"admin"}`, "2")
	}()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		if err = db.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%admin_roles%')").Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("handler did not reach role lock barrier")
		case <-ticker.C:
		}
	}
	if _, err = blocker.Exec(ctx, "UPDATE identity.users SET system_role='user',version=2,token_version=token_version+1 WHERE id=$1", actor); err != nil {
		t.Fatal(err)
	}
	if err = blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	w := <-done
	var role string
	var version int
	if err = db.Pool.QueryRow(ctx, "SELECT system_role,version FROM identity.users WHERE id=$1", actor).Scan(&role, &version); err != nil {
		t.Fatal(err)
	}
	if w.Code != 403 || role != "user" || version != 2 {
		t.Fatalf("demoted actor regranted own role: HTTP%d role=%s version=%d body=%s", w.Code, role, version, w.Body)
	}
}

// A captured admin principal cannot authorize private reads or catalog/config/review mutations after its database role or active state changes.
func TestAdminEndpointsRecheckCapturedRoleAndActiveState(t *testing.T) {
	for _, state := range []string{"demoted", "disabled", "deleted"} {
		t.Run(state, func(t *testing.T) {
			db := testutil.NewDB(t)
			ctx := context.Background()
			actor := revocationUser(t, db, "admin")
			target := revocationUser(t, db, "user")
			sid, category, food := platform.ID(), platform.ID(), platform.ID()
			for _, q := range []struct {
				sql  string
				args []any
			}{{"INSERT INTO household.spaces(id,kind,name,created_by) VALUES($1,'personal','Review',$2)", []any{sid, actor}}, {"INSERT INTO catalog.food_categories(id,name) VALUES($1,'Review category')", []any{category}}, {"INSERT INTO catalog.foods(id,name,category_id,scope,owner_space_id,default_unit_id,status) VALUES($1,'Review food','00000000-0000-7000-8000-000000000001','space',$2,'00000000-0000-7000-8000-000000000011','pending_review')", []any{food, sid}}} {
				if _, err := db.Pool.Exec(ctx, q.sql, q.args...); err != nil {
					t.Fatal(err)
				}
			}
			sql := "UPDATE identity.users SET system_role='user' WHERE id=$1"
			if state == "disabled" {
				sql = "UPDATE identity.users SET status='disabled' WHERE id=$1"
			}
			if state == "deleted" {
				sql = "UPDATE identity.users SET deleted_at=now() WHERE id=$1"
			}
			if _, err := db.Pool.Exec(ctx, sql, actor); err != nil {
				t.Fatal(err)
			}
			s := New(db, nil)
			mux := http.NewServeMux()
			s.RegisterAdmin(mux)
			s.RegisterCatalog(mux)
			for _, route := range []struct{ method, path, body, version string }{
				{"GET", "/it4788/v1/admin/configs", "", ""}, {"GET", "/it4788/v1/admin/users", "", ""}, {"GET", "/it4788/v1/admin/audit-logs", "", ""},
				{"GET", "/it4788/v1/admin/foods", "", ""}, {"GET", "/it4788/v1/admin/food-categories", "", ""}, {"GET", "/it4788/v1/admin/units", "", ""},
				{"PATCH", "/it4788/v1/admin/users/" + target, `{"systemRole":"admin"}`, "1"},
				{"PUT", "/it4788/v1/admin/configs", `{"values":{"reminder.defaultDays":4}}`, ""},
				{"POST", "/it4788/v1/admin/food-categories", `{"name":"Unauthorized category"}`, ""},
				{"PUT", "/it4788/v1/admin/food-categories/" + category, `{"name":"Unauthorized rename"}`, ""},
				{"DELETE", "/it4788/v1/admin/food-categories/" + category, "", ""},
				{"POST", "/it4788/v1/admin/foods/" + food + "/review", `{"action":"approve"}`, ""},
			} {
				w := adminCapturedRequest(mux, ctx, actor, route.method, route.path, route.body, route.version)
				if w.Code != 403 {
					t.Errorf("%s %s stale%s status%d %s", route.method, route.path, state, w.Code, w.Body)
				}
			}
			var role, status, name string
			var audits int
			if err := db.Pool.QueryRow(ctx, "SELECT (SELECT system_role FROM identity.users WHERE id=$1),(SELECT status FROM catalog.foods WHERE id=$2),coalesce((SELECT name FROM catalog.food_categories WHERE id=$3),''),(SELECT count(*) FROM admin.audit_logs)", target, food, category).Scan(&role, &status, &name, &audits); err != nil {
				t.Fatal(err)
			}
			if role != "user" || status != "pending_review" || name != "Review category" || audits != 0 {
				t.Fatal("unauthorized administrator changed protected state")
			}
		})
	}
}

func TestAdministratorAuthorityIsHeldThroughCatalogMutation(t *testing.T) {
	db := testutil.NewDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	actor := revocationUser(t, db, "admin")
	category := platform.ID()
	if _, err := db.Pool.Exec(ctx, "INSERT INTO catalog.food_categories(id,name) VALUES($1,'Before authorized update')", category); err != nil {
		t.Fatal(err)
	}
	blocker, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(context.Background())
	if _, err = blocker.Exec(ctx, "SELECT id FROM catalog.food_categories WHERE id=$1 FOR UPDATE", category); err != nil {
		t.Fatal(err)
	}
	s := New(db, nil)
	mux := http.NewServeMux()
	s.RegisterCatalog(mux)
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- adminCapturedRequest(mux, ctx, actor, "PUT", "/it4788/v1/admin/food-categories/"+category, `{"name":"Authorized update"}`, "")
	}()
	waitFor := func(fragment string) {
		t.Helper()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			var waiting bool
			if err := db.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND strpos(query,$1)>0)", fragment).Scan(&waiting); err != nil {
				t.Fatal(err)
			}
			if waiting {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatalf("expected lock wait for %s", fragment)
			case <-ticker.C:
			}
		}
	}
	waitFor("SELECT to_jsonb(x) FROM catalog.food_categories")
	demoted := make(chan error, 1)
	go func() {
		_, err := db.Pool.Exec(ctx, "UPDATE identity.users SET system_role='user' WHERE id=$1", actor)
		demoted <- err
	}()
	waitFor("UPDATE identity.users SET system_role='user'")
	if err = blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	w := <-done
	if err = <-demoted; err != nil {
		t.Fatal(err)
	}
	var role, name string
	if err = db.Pool.QueryRow(ctx, "SELECT (SELECT system_role FROM identity.users WHERE id=$1),(SELECT name FROM catalog.food_categories WHERE id=$2)", actor, category).Scan(&role, &name); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || role != "user" || name != "Authorized update" {
		t.Fatalf("authorization ordering HTTP%d role%s name%s", w.Code, role, name)
	}
}
