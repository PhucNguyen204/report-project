package platform

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeRejectsUnknownAndTrailingJSON(t *testing.T) {
	for _, body := range []string{`{"name":"ok","admin":true}`, `{"name":"ok"}{}`, `null {}`} {
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
