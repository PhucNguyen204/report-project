//go:build integration

package notification

import (
	"context"
	"dicho/internal/platform"
	"dicho/internal/testutil"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func notifyUser(t *testing.T, db *platform.DB) string {
	t.Helper()
	user := platform.ID()
	if _, err := db.Pool.Exec(context.Background(), "INSERT INTO identity.users(id,email,full_name,password_hash,status) VALUES($1,$2,'Notify User','unused','active')", user, user+"@test.local"); err != nil {
		t.Fatal(err)
	}
	return user
}
func inboxCall(t *testing.T, mux *http.ServeMux, user, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r = r.WithContext(platform.WithPrincipal(context.Background(), platform.Principal{UserID: user}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}
func inboxNotice(t *testing.T, db *platform.DB, user string, at time.Time) string {
	t.Helper()
	id := platform.ID()
	if _, err := db.Pool.Exec(context.Background(), "INSERT INTO notification.notifications(id,user_id,dedupe_key,title,body,type,created_at) VALUES($1::uuid,$2,$1::text,'Thông báo','Nội dung','activity',$3)", id, user, at); err != nil {
		t.Fatal(err)
	}
	return id
}

// Replacing keyset pagination with OFFSET or an id-only cursor would repeat/omit equal-time entries after a new notification arrives.
func TestInboxPaginationUsesOwnedStableCursorAndDefault20(t *testing.T) {
	db := testutil.NewDB(t)
	user, other := notifyUser(t, db), notifyUser(t, db)
	at := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	ids := map[string]bool{}
	for i := 0; i < 24; i++ {
		created := at
		if i >= 21 {
			created = at.Add(-time.Hour)
		}
		ids[inboxNotice(t, db, user, created)] = true
	}
	foreign := inboxNotice(t, db, other, at.Add(time.Hour))
	s := New(db, nil, nil)
	mux := http.NewServeMux()
	s.Register(mux)
	decode := func(w *httptest.ResponseRecorder) ([]map[string]any, string) {
		t.Helper()
		if w.Code != 200 {
			t.Fatalf("list %d %s", w.Code, w.Body)
		}
		var out struct {
			Data struct {
				Items []map[string]any `json:"items"`
			} `json:"data"`
			Meta struct {
				Next string `json:"nextCursor"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("paginated inbox response %s: %v", w.Body, err)
		}
		return out.Data.Items, out.Meta.Next
	}
	first, cursor := decode(inboxCall(t, mux, user, "GET", "/it4788/v1/me/notifications", ""))
	if len(first) != 20 || cursor == "" {
		t.Fatalf("default page %d cursor %q", len(first), cursor)
	}
	newer := inboxNotice(t, db, user, at.Add(time.Minute))
	second, next := decode(inboxCall(t, mux, user, "GET", "/it4788/v1/me/notifications?cursor="+url.QueryEscape(cursor), ""))
	if len(second) != 4 || next != "" {
		t.Fatalf("second page %d %q", len(second), next)
	}
	seen := map[string]bool{}
	for _, page := range [][]map[string]any{first, second} {
		for _, item := range page {
			id := item["id"].(string)
			if !ids[id] || seen[id] || id == foreign || id == newer {
				t.Fatalf("scope/repeated entry %s", id)
			}
			seen[id] = true
		}
	}
	if len(seen) != 24 {
		t.Fatal("cursor omitted tied entries")
	}
	for _, query := range []string{"limit=0", "limit=101", "limit=-1", "limit=bad", "cursor=garbage", "cursor=eyJhdCI6ImJhZCIsImlkIjoiYmFkIn0"} {
		w := inboxCall(t, mux, user, "GET", "/it4788/v1/me/notifications?"+query, "")
		if w.Code != 400 {
			t.Fatalf("query %s: %d %s", query, w.Code, w.Body)
		}
	}
	all, _ := decode(inboxCall(t, mux, user, "GET", "/it4788/v1/me/notifications?limit=100", ""))
	if len(all) != 25 {
		t.Fatalf("maximum page %d", len(all))
	}
	if w := inboxCall(t, mux, "", "GET", "/it4788/v1/me/notifications", ""); w.Code != 401 {
		t.Fatalf("anonymous read %d", w.Code)
	}
}

// Marking foreign or replayed IDs must never change another user's read state or overwrite the first read timestamp.
func TestMarkReadAffectsOnlyOwnedNotificationsAndValidatesIDs(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	user, other := notifyUser(t, db), notifyUser(t, db)
	own, foreign := inboxNotice(t, db, user, db.Clock.Now()), inboxNotice(t, db, other, db.Clock.Now())
	s := New(db, nil, nil)
	mux := http.NewServeMux()
	s.Register(mux)
	body := `{"ids":["` + own + `","` + foreign + `"]}`
	for i := 0; i < 2; i++ {
		w := inboxCall(t, mux, user, "POST", "/it4788/v1/me/notifications/read", body)
		if w.Code != 200 {
			t.Fatalf("mark %d %s", w.Code, w.Body)
		}
	}
	var ownRead, otherRead *time.Time
	if err := db.Pool.QueryRow(ctx, "SELECT (SELECT read_at FROM notification.notifications WHERE id=$1),(SELECT read_at FROM notification.notifications WHERE id=$2)", own, foreign).Scan(&ownRead, &otherRead); err != nil {
		t.Fatal(err)
	}
	if ownRead == nil || otherRead != nil {
		t.Fatal("foreign read state changed")
	}
	for _, body := range []string{`{`, `{"ids":[]}`, `{"ids":["not-a-uuid"]}`, `{"unknown":true}`} {
		w := inboxCall(t, mux, user, "POST", "/it4788/v1/me/notifications/read", body)
		if w.Code != 400 {
			t.Fatalf("invalid mark %s %d %s", body, w.Code, w.Body)
		}
	}
	if w := inboxCall(t, mux, "", "POST", "/it4788/v1/me/notifications/read", body); w.Code != 401 {
		t.Fatalf("anonymous mark %d", w.Code)
	}
}
