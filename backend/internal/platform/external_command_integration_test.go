//go:build integration

package platform_test

import (
	"dicho/internal/platform"
	"dicho/internal/testutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIdempotencyRejectsMalformedUUIDBeforeHandler(t *testing.T) {
	db := testutil.NewDB(t)
	called := false
	h := platform.Idempotency(db, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; platform.Write(w, 200, true) }))
	r := httptest.NewRequest("POST", "/it4788/v1/test", strings.NewReader(`{}`))
	r.Header.Set("Idempotency-Key", "not-a-uuid")
	r = r.WithContext(platform.WithPrincipal(r.Context(), platform.Principal{UserID: platform.ID()}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 400 || called {
		t.Fatalf("malformed key admitted: %d %v", w.Code, called)
	}
}
func TestMediaCompletionReauthorizesWithoutHoldingDBTransaction(t *testing.T) {
	db := testutil.NewDB(t)
	authorized := true
	h := platform.Idempotency(db, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if db.Pool.Stat().AcquiredConns() != 0 {
			platform.Fail(w, platform.Err(500, 5000, "external I/O held transaction"))
			return
		}
		if !authorized {
			platform.Fail(w, platform.NotFound())
			return
		}
		platform.Write(w, 200, map[string]any{"status": "ready"})
	}))
	user, key, object := platform.ID(), platform.ID(), platform.ID()
	seedMiddlewareActor(t, db, user)
	request := func(object string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/it4788/v1/media/"+object+"/complete", strings.NewReader(`{}`))
		r.Header.Set("Idempotency-Key", key)
		r = r.WithContext(platform.WithPrincipal(r.Context(), platform.Principal{UserID: user}))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := request(object); w.Code != 200 {
		t.Fatalf("initial%d %s", w.Code, w.Body)
	}
	authorized = false
	if w := request(object); w.Code != 404 {
		t.Fatalf("cached response bypassed ownership%d", w.Code)
	}
	if w := request(platform.ID()); w.Code != 400 {
		t.Fatalf("key reused for another object%d", w.Code)
	}
}
