//go:build integration

package accounts

import (
	"bytes"
	"context"
	"dicho/internal/platform"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func accountRequest(t *testing.T, f *authFixture, method, path string, payload any, token, match string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	if match != "" {
		r.Header.Set("If-Match", match)
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	return w
}
func TestVersionedAccountMutationsRequireIfMatch(t *testing.T) {
	f := setupAuth(t)
	u := f.activate(t, "ifmatch-user@example.com")
	token := u["accessToken"].(string)
	admin := makeAdmin(t, f, "ifmatch-admin@example.com")
	_, body := f.call(t, "POST", "/it4788/v1/spaces", map[string]any{"name": "Version family"}, token)
	sid := body["data"].(map[string]any)["id"].(string)
	cases := []struct {
		method, path, token string
		body                any
	}{
		{"PATCH", "/it4788/v1/me", token, map[string]any{"fullName": "New name"}},
		{"PATCH", "/it4788/v1/spaces/" + sid, token, map[string]any{"name": "New family"}},
		{"PATCH", "/it4788/v1/admin/users/" + u["userId"].(string), admin, map[string]any{"status": "active"}},
		{"PUT", "/it4788/v1/admin/foods/00000000-0000-7000-8000-000000000021", admin, map[string]any{"name": "Carrot", "categoryId": "00000000-0000-7000-8000-000000000001", "defaultUnitId": "00000000-0000-7000-8000-000000000011"}},
	}
	for _, tc := range cases {
		for _, match := range []string{"", "*", "W/\"1\"", "1,2", "0", "-1", "\"1", "+1"} {
			w := accountRequest(t, f, tc.method, tc.path, tc.body, tc.token, match)
			if w.Code != 400 {
				t.Fatalf("%s %s invalid match%q returned%d %s", tc.method, tc.path, match, w.Code, w.Body)
			}
		}
	}
	var name string
	if err := f.db.Pool.QueryRow(context.Background(), `SELECT full_name FROM identity.users WHERE id=$1`, u["userId"]).Scan(&name); err != nil || name != "Nguyễn An" {
		t.Fatalf("missing header changed profile %s %v", name, err)
	}
}
func TestProfileAndSpaceGETExposeVersionsAndETags(t *testing.T) {
	f := setupAuth(t)
	u := f.activate(t, "etag-entity@example.com")
	token := u["accessToken"].(string)
	for _, path := range []string{"/it4788/v1/me", "/it4788/v1/spaces/" + u["spaceId"].(string)} {
		w := accountRequest(t, f, "GET", path, nil, token, "")
		var body struct {
			Data struct {
				Version int64 `json:"version"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || body.Data.Version < 1 || w.Header().Get("ETag") == "" {
			t.Fatalf("GET version missing %s %d %s", path, w.Code, w.Body)
		}
	}
}

func fixtureMatch(f *authFixture, method, path, token string) string {
	if method != "PATCH" && method != "PUT" {
		return ""
	}
	table, id := "", ""
	segments := strings.Split(strings.TrimPrefix(path, "/it4788/v1/"), "/")
	if path == "/it4788/v1/me" && method == "PATCH" {
		table = "identity.users"
		p, err := f.s.Authenticate(context.Background(), token)
		if err == nil {
			id = p.UserID
		}
	}
	if len(segments) == 2 && segments[0] == "spaces" && method == "PATCH" {
		table, id = "household.spaces", segments[1]
	}
	if len(segments) == 3 && segments[0] == "admin" {
		if segments[1] == "users" && method == "PATCH" {
			table, id = "identity.users", segments[2]
		}
		if segments[1] == "foods" && method == "PUT" {
			table, id = "catalog.foods", segments[2]
		}
	}
	if table == "" {
		return ""
	}
	var revision int64
	if err := f.db.Pool.QueryRow(context.Background(), "SELECT version FROM "+table+" WHERE id=$1", id).Scan(&revision); err != nil {
		return `"1"`
	}
	return `"` + strconv.FormatInt(revision, 10) + `"`
}

func TestCatalogValidationLocksOriginalAndCanonicalRowsUntilWriterCommits(t *testing.T) {
	for _, merged := range []bool{false, true} {
		t.Run(strconv.FormatBool(merged), func(t *testing.T) {
			f := setupAuth(t)
			u := f.activate(t, "catalog-lock@example.com")
			ctx := context.Background()
			source := platform.ID()
			canonical := "00000000-0000-7000-8000-000000000021"
			if _, err := f.db.Pool.Exec(ctx, `INSERT INTO catalog.foods(id,name,category_id,scope,owner_space_id,default_unit_id,status) VALUES($1,'Private locked','00000000-0000-7000-8000-000000000001','space',$2,'00000000-0000-7000-8000-000000000011','pending_review')`, source, u["spaceId"]); err != nil {
				t.Fatal(err)
			}
			locked := source
			if merged {
				if _, err := f.db.Pool.Exec(ctx, `UPDATE catalog.foods SET status='inactive',merged_into=$2 WHERE id=$1`, source, canonical); err != nil {
					t.Fatal(err)
				}
				locked = canonical
			}
			writer, err := f.db.Pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer writer.Rollback(ctx)
			if _, err = f.s.ValidateFood(ctx, writer, u["spaceId"].(string), source, "00000000-0000-7000-8000-000000000011"); err != nil {
				t.Fatal(err)
			}
			updater, err := f.db.Pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer updater.Rollback(ctx)
			if _, err = updater.Exec(ctx, `SET LOCAL lock_timeout='100ms'`); err != nil {
				t.Fatal(err)
			}
			_, err = updater.Exec(ctx, `UPDATE catalog.foods SET status='inactive' WHERE id=$1`, locked)
			var pgerr *pgconn.PgError
			if !errors.As(err, &pgerr) || pgerr.Code != "55P03" {
				t.Fatalf("catalog changed before validated writer committed: %v", err)
			}
			if err = updater.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			if err = writer.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err = f.db.Pool.Exec(ctx, `UPDATE catalog.foods SET status='inactive' WHERE id=$1`, locked); err != nil {
				t.Fatalf("catalog remained locked after commit: %v", err)
			}
		})
	}
}

func accountMutationData(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("mutation%d %s", w.Code, w.Body)
	}
	var body struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Data
}
func assertOverwritten(t *testing.T, data map[string]any, want ...string) {
	t.Helper()
	raw, ok := data["overwrittenFields"].([]any)
	if !ok {
		t.Fatalf("missing overwrittenFields: %+v", data)
	}
	got := []string{}
	for _, v := range raw {
		got = append(got, v.(string))
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("overwritten=%v want%v", got, want)
	}
}
func TestProfileMetadataUsesFieldHistoryForPreciseStaleMerges(t *testing.T) {
	f := setupAuth(t)
	u := f.activate(t, "field-merge@example.com")
	token := u["accessToken"].(string)
	path := "/it4788/v1/me"
	data := accountMutationData(t, accountRequest(t, f, "PATCH", path, map[string]any{"fullName": "Server name"}, token, `"1"`))
	assertOverwritten(t, data)
	data = accountMutationData(t, accountRequest(t, f, "PATCH", path, map[string]any{"locale": "en"}, token, `"1"`))
	assertOverwritten(t, data)
	w := accountRequest(t, f, "GET", path, nil, token, "")
	var current struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &current); err != nil {
		t.Fatal(err)
	}
	if current.Data["fullName"] != "Server name" || current.Data["locale"] != "en" {
		t.Fatal(current)
	}
	data = accountMutationData(t, accountRequest(t, f, "PATCH", path, map[string]any{"fullName": "Client wins"}, token, `"1"`))
	assertOverwritten(t, data, "fullName")
	data = accountMutationData(t, accountRequest(t, f, "PATCH", path, map[string]any{"fullName": "Client wins"}, token, `"1"`))
	assertOverwritten(t, data)
	if w = accountRequest(t, f, "PATCH", path, map[string]any{"locale": "vi"}, token, `"999"`); w.Code != 409 {
		t.Fatalf("future version%d", w.Code)
	}
}
func TestGroupMetadataUsesTrackedFieldsAndCatalogPUTIsStrict(t *testing.T) {
	f := setupAuth(t)
	u := f.activate(t, "group-version@example.com")
	token := u["accessToken"].(string)
	admin := makeAdmin(t, f, "group-version-admin@example.com")
	_, body := f.call(t, "POST", "/it4788/v1/spaces", map[string]any{"name": "Original family"}, token)
	sid := body["data"].(map[string]any)["id"].(string)
	path := "/it4788/v1/spaces/" + sid
	assertOverwritten(t, accountMutationData(t, accountRequest(t, f, "PATCH", path, map[string]any{"name": "First family"}, token, `"1"`)))
	assertOverwritten(t, accountMutationData(t, accountRequest(t, f, "PATCH", path, map[string]any{"avatarObjectId": ""}, token, `"1"`)))
	assertOverwritten(t, accountMutationData(t, accountRequest(t, f, "PATCH", path, map[string]any{"name": "Second family"}, token, `"1"`)), "name")
	foodPath := "/it4788/v1/admin/foods/00000000-0000-7000-8000-000000000021"
	in := map[string]any{"name": "Updated carrot", "categoryId": "00000000-0000-7000-8000-000000000001", "defaultUnitId": "00000000-0000-7000-8000-000000000011"}
	accountMutationData(t, accountRequest(t, f, "PUT", foodPath, in, admin, `"1"`))
	if w := accountRequest(t, f, "PUT", foodPath, in, admin, `"1"`); w.Code != 409 {
		t.Fatalf("stale food replacement%d %s", w.Code, w.Body)
	}
}

func TestProfileFieldHistoryIncludesChangesMadeOutsideHTTP(t *testing.T) {
	f := setupAuth(t)
	u := f.activate(t, "background-version@example.com")
	token := u["accessToken"].(string)
	ctx := context.Background()
	var before, after int64
	if err := f.db.Pool.QueryRow(ctx, `SELECT version FROM identity.users WHERE id=$1`, u["userId"]).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool.Exec(ctx, `UPDATE identity.users SET full_name='Background name' WHERE id=$1`, u["userId"]); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Pool.QueryRow(ctx, `SELECT version FROM identity.users WHERE id=$1`, u["userId"]).Scan(&after); err != nil || after != before+1 {
		t.Fatalf("background change did not advance version%d→%d %v", before, after, err)
	}
	match := `"` + strconv.FormatInt(before, 10) + `"`
	assertOverwritten(t, accountMutationData(t, accountRequest(t, f, "PATCH", "/it4788/v1/me", map[string]any{"fullName": "Client name"}, token, match)), "fullName")
}
func TestAdminStatusMutationRejectsStaleSecurityDecision(t *testing.T) {
	f := setupAuth(t)
	u := f.activate(t, "stale-security@example.com")
	admin := makeAdmin(t, f, "stale-security-admin@example.com")
	path := "/it4788/v1/admin/users/" + u["userId"].(string)
	match := fixtureMatch(f, "PATCH", path, admin)
	accountMutationData(t, accountRequest(t, f, "PATCH", path, map[string]any{"status": "disabled", "lockReason": "Security review"}, admin, match))
	w := accountRequest(t, f, "PATCH", path, map[string]any{"status": "active"}, admin, match)
	if w.Code != 409 {
		t.Fatalf("stale security decision%d %s", w.Code, w.Body)
	}
	var status string
	if err := f.db.Pool.QueryRow(context.Background(), `SELECT status FROM identity.users WHERE id=$1`, u["userId"]).Scan(&status); err != nil || status != "disabled" {
		t.Fatalf("stale decision applied %s %v", status, err)
	}
}
