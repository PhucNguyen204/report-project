//go:build integration

package accounts

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"
)

type adminPage struct {
	Data struct {
		Items []map[string]any `json:"items"`
	} `json:"data"`
	Meta struct {
		NextCursor string    `json:"nextCursor"`
		ServerTime time.Time `json:"serverTime"`
	} `json:"meta"`
}

func adminCollection(t *testing.T, f *authFixture, path, token string) adminPage {
	t.Helper()
	w := accountRequest(t, f, "GET", path, nil, token, "")
	if w.Code != 200 {
		t.Fatalf("GET%s status%d %s", path, w.Code, w.Body)
	}
	var page adminPage
	if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if page.Meta.ServerTime.IsZero() || !page.Meta.ServerTime.Equal(f.clock.Now()) {
		t.Fatalf("serverTime %v want %v", page.Meta.ServerTime, f.clock.Now())
	}
	for _, item := range page.Data.Items {
		for field := range item {
			if field == "field_versions" || field == "password_hash" || strings.HasPrefix(field, "__") {
				t.Fatalf("private field%s leaked: %+v", field, item)
			}
		}
	}
	return page
}
func pageUser(t *testing.T, f *authFixture, id, email string, at time.Time) {
	t.Helper()
	if _, err := f.db.Pool.Exec(context.Background(), `INSERT INTO identity.users(id,email,full_name,password_hash,status,created_at) VALUES($1,$2,'Pagination user','never-expose','active',$3)`, id, email, at); err != nil {
		t.Fatal(err)
	}
}
func TestAdminUserKeysetUsesCreationTimeAndIDWithoutRepeatingNewerInsert(t *testing.T) {
	f := setupAuth(t)
	token := makeAdmin(t, f, "pagination-admin@example.com")
	at := f.clock.Now()
	for i := 1; i <= 21; i++ {
		pageUser(t, f, fmt.Sprintf("10000000-0000-7000-8000-%012d", i), fmt.Sprintf("page-user-%02d@example.com", i), at)
	}
	pageUser(t, f, "10000000-0000-7000-8000-000000000100", "page-user-deleted@example.com", at)
	if _, err := f.db.Pool.Exec(context.Background(), `UPDATE identity.users SET deleted_at=$1 WHERE id='10000000-0000-7000-8000-000000000100'`, at); err != nil {
		t.Fatal(err)
	}
	base := "/it4788/v1/admin/users?q=page-user-"
	first := adminCollection(t, f, base, token)
	if len(first.Data.Items) != 20 || first.Meta.NextCursor == "" {
		t.Fatalf("default page count%d cursor%q", len(first.Data.Items), first.Meta.NextCursor)
	}
	if first.Data.Items[0]["id"] != "10000000-0000-7000-8000-000000000021" || first.Data.Items[19]["id"] != "10000000-0000-7000-8000-000000000002" {
		t.Fatalf("descending ID tie order %+v", first.Data.Items)
	}
	pageUser(t, f, "10000000-0000-7000-8000-000000000099", "page-user-new@example.com", at.Add(time.Hour))
	second := adminCollection(t, f, base+"&limit=7&cursor="+url.QueryEscape(first.Meta.NextCursor), token)
	if len(second.Data.Items) != 1 || second.Data.Items[0]["id"] != "10000000-0000-7000-8000-000000000001" || second.Meta.NextCursor != "" {
		t.Fatalf("keyset after insert %+v cursor%q", second.Data.Items, second.Meta.NextCursor)
	}
	all := adminCollection(t, f, base+"&limit=100", token)
	if len(all.Data.Items) != 22 || all.Meta.NextCursor != "" {
		t.Fatalf("max page count%d cursor%q", len(all.Data.Items), all.Meta.NextCursor)
	}
	for _, path := range []string{base + "&limit=101", base + "&limit=0", base + "&cursor=not-base64", "/it4788/v1/admin/users?q=changed&cursor=" + url.QueryEscape(first.Meta.NextCursor)} {
		w := accountRequest(t, f, "GET", path, nil, token, "")
		if w.Code != 400 {
			t.Fatalf("invalid page%s got%d %s", path, w.Code, w.Body)
		}
	}
	other := makeAdmin(t, f, "pagination-other-admin@example.com")
	w := accountRequest(t, f, "GET", base+"&cursor="+url.QueryEscape(first.Meta.NextCursor), nil, other, "")
	if w.Code != 400 {
		t.Fatalf("another admin reused cursor%d %s", w.Code, w.Body)
	}
}
func TestAuditKeysetReturnsAllTiesAndHidesInternalFieldHistory(t *testing.T) {
	f := setupAuth(t)
	token := makeAdmin(t, f, "audit-pagination-admin@example.com")
	ctx := context.Background()
	var actor string
	if err := f.db.Pool.QueryRow(ctx, `SELECT id::text FROM identity.users WHERE email='audit-pagination-admin@example.com'`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	at := f.clock.Now()
	insert := func(i int, stamp time.Time) {
		t.Helper()
		if _, err := f.db.Pool.Exec(ctx, `INSERT INTO admin.audit_logs(id,actor_id,action,entity_id,old_value,new_value,created_at) VALUES($1,$2,'page.action','entity','{"name":"Old","field_versions":{"name":2}}','{"name":"New"}',$3)`, fmt.Sprintf("20000000-0000-7000-8000-%012d", i), actor, stamp); err != nil {
			t.Fatal(err)
		}
	}
	for i := 1; i <= 21; i++ {
		insert(i, at)
	}
	base := "/it4788/v1/admin/audit-logs"
	first := adminCollection(t, f, base, token)
	if len(first.Data.Items) != 20 || first.Meta.NextCursor == "" {
		t.Fatalf("default audit page count%d cursor%q", len(first.Data.Items), first.Meta.NextCursor)
	}
	if first.Data.Items[0]["id"] != "20000000-0000-7000-8000-000000000021" {
		t.Fatalf("audit tie order %+v", first.Data.Items[0])
	}
	old := first.Data.Items[0]["oldValue"].(map[string]any)
	if old["name"] != "Old" || old["field_versions"] != nil {
		t.Fatalf("audit public shape %+v", old)
	}
	insert(99, at.Add(time.Hour))
	second := adminCollection(t, f, base+"?limit=100&cursor="+url.QueryEscape(first.Meta.NextCursor), token)
	if len(second.Data.Items) != 1 || second.Data.Items[0]["id"] != "20000000-0000-7000-8000-000000000001" || second.Meta.NextCursor != "" {
		t.Fatalf("audit after insertion %+v", second)
	}
	if w := accountRequest(t, f, "GET", "/it4788/v1/admin/users?cursor="+url.QueryEscape(first.Meta.NextCursor), nil, token, ""); w.Code != 400 {
		t.Fatalf("cross-route cursor%d %s", w.Code, w.Body)
	}
}
func TestCatalogAdminKeysetsUseNameAndIDAndKeepItemsEnvelope(t *testing.T) {
	for _, kind := range []string{"food-categories", "units", "foods"} {
		t.Run(kind, func(t *testing.T) {
			f := setupAuth(t)
			token := makeAdmin(t, f, "catalog-page-admin@example.com")
			ctx := context.Background()
			table := map[string]string{"food-categories": "food_categories", "units": "units", "foods": "foods"}[kind]
			if _, err := f.db.Pool.Exec(ctx, `UPDATE catalog.`+table+` SET name='Base '||id::text`); err != nil {
				t.Fatal(err)
			}
			var initial int
			if err := f.db.Pool.QueryRow(ctx, `SELECT count(*) FROM catalog.`+table).Scan(&initial); err != nil {
				t.Fatal(err)
			}
			insert := func(id, name, category string) {
				t.Helper()
				var err error
				switch kind {
				case "food-categories":
					_, err = f.db.Pool.Exec(ctx, `INSERT INTO catalog.food_categories(id,name) VALUES($1,$2)`, id, name)
				case "units":
					_, err = f.db.Pool.Exec(ctx, `INSERT INTO catalog.units(id,name,dimension,factor_to_base) VALUES($1,$2,'mass',2)`, id, name)
				case "foods":
					_, err = f.db.Pool.Exec(ctx, `INSERT INTO catalog.foods(id,name,category_id,default_unit_id) VALUES($1,$2,$3,'00000000-0000-7000-8000-000000000011')`, id, name, category)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			for i := 1; i <= 21; i++ {
				insert(fmt.Sprintf("30000000-0000-7000-8000-%012d", i), fmt.Sprintf("Page catalog %02d", i), "00000000-0000-7000-8000-000000000001")
			}
			expected := initial + 21
			if kind == "foods" {
				insert("30000000-0000-7000-8000-000000000091", "Page catalog Tie", "00000000-0000-7000-8000-000000000001")
				insert("30000000-0000-7000-8000-000000000092", "Page catalog Tie", "00000000-0000-7000-8000-000000000002")
				expected += 2
			}
			base := "/it4788/v1/admin/" + kind
			page := adminCollection(t, f, base, token)
			if len(page.Data.Items) != 20 || page.Meta.NextCursor == "" {
				t.Fatalf("catalog default count%d cursor%q", len(page.Data.Items), page.Meta.NextCursor)
			}
			insert("30000000-0000-7000-8000-000000000099", "AA inserted before cursor", "00000000-0000-7000-8000-000000000001")
			seen := map[string]bool{}
			ties := []string{}
			pages := 0
			for {
				pages++
				if pages > 1 && len(page.Data.Items) > 3 {
					t.Fatalf("catalog ignored resized limit: %d", len(page.Data.Items))
				}
				if pages > 10 {
					t.Fatal("cursor did not terminate")
				}
				for _, item := range page.Data.Items {
					id := item["id"].(string)
					if seen[id] {
						t.Fatalf("repeated catalog item%s", id)
					}
					seen[id] = true
					if item["name"] == "Page catalog Tie" {
						ties = append(ties, id)
					}
				}
				if page.Meta.NextCursor == "" {
					break
				}
				page = adminCollection(t, f, base+"?limit=3&cursor="+url.QueryEscape(page.Meta.NextCursor), token)
			}
			if len(seen) != expected {
				t.Fatalf("catalog pagination count%d want%d", len(seen), expected)
			}
			if kind == "foods" && strings.Join(ties, ",") != "30000000-0000-7000-8000-000000000091,30000000-0000-7000-8000-000000000092" {
				t.Fatalf("food name tie order%v", ties)
			}
			for _, query := range []string{"limit=101", "limit=20&limit=30", "cursor=invalid"} {
				if w := accountRequest(t, f, "GET", base+"?"+query, nil, token, ""); w.Code != 400 {
					t.Fatalf("catalog invalid %s got%d %s", query, w.Code, w.Body)
				}
			}
		})
	}
}
func TestAdminPaginationStillRejectsNonAdministrators(t *testing.T) {
	f := setupAuth(t)
	u := f.activate(t, "page-nonadmin@example.com")
	for _, path := range []string{"users", "audit-logs", "foods", "food-categories", "units"} {
		w := accountRequest(t, f, "GET", "/it4788/v1/admin/"+path+"?limit=1", nil, u["accessToken"].(string), "")
		if w.Code != 403 {
			t.Fatalf("nonadmin%s got%d %s", path, w.Code, w.Body)
		}
	}
}
