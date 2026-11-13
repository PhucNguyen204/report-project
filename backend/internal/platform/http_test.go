package platform

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeRejectsUnknownAndTrailingJSON(t *testing.T) {
	for _, body := range []string{`{"name":"ok","admin":true}`, `{"name":"ok"}{}`, `null {}`, `null`, `[]`, `false`, `1`, `"name"`} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		var input struct {
			Name string `json:"name"`
		}
		if err := Decode(r, &input); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}
func TestFailDoesNotLeakDatabaseDetails(t *testing.T) {
	w := httptest.NewRecorder()
	Fail(w, errors.New("password=secret DATABASE_URL"))
	if w.Code != 500 || strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}
func TestAuthRejectsMissingTokenAndPreservesPublicAuth(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(204) })
	h := Auth(nil, next)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/it4788/v1/me", nil))
	if w.Code != 401 || called {
		t.Fatalf("%d %v", w.Code, called)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/it4788/v1/auth/login", nil))
	if w.Code != 204 || !called {
		t.Fatal("public auth blocked")
	}
}

func TestPreviewDoesNotNeedIdempotencyKey(t *testing.T) {
	for _, path := range []string{"/it4788/v1/spaces/join", "/it4788/v1/spaces/abc/shopping-lists/generate", "/it4788/v1/spaces/abc/recipes/def/missing-to-list"} {
		r := httptest.NewRequest("POST", path, strings.NewReader(`{"dryRun":true}`))
		r = r.WithContext(WithPrincipal(r.Context(), Principal{UserID: "actor"}))
		w := httptest.NewRecorder()
		Idempotency(nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })).ServeHTTP(w, r)
		if w.Code != 204 {
			t.Fatalf("preview blocked %s %d", path, w.Code)
		}
	}
}

func TestAuthenticatedLogoutRequiresIdempotencyKey(t *testing.T) {
	r := httptest.NewRequest("POST", "/it4788/v1/auth/logout", strings.NewReader(`{}`))
	r = r.WithContext(WithPrincipal(r.Context(), Principal{UserID: "actor"}))
	w := httptest.NewRecorder()
	Idempotency(nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })).ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatalf("logout admitted without key: %d", w.Code)
	}
}
