package planning

import (
	"dicho/internal/platform"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPlanningPaginationRejectsBadParamsBeforeDatabaseAccess(t *testing.T) {
	s := New(nil, nil, nil, nil)
	mux := http.NewServeMux()
	s.Register(mux)
	for _, path := range []string{"/it4788/v1/spaces/00000000-0000-7000-8000-000000000001/recipes", "/it4788/v1/spaces/00000000-0000-7000-8000-000000000001/meals", "/it4788/v1/admin/recipe-templates"} {
		for _, query := range []string{"limit=101", "limit=1&limit=2", "cursor=bad-cursor"} {
			t.Run(path+"?"+query, func(t *testing.T) {
				r := httptest.NewRequest("GET", path+"?"+query, nil)
				r = r.WithContext(platform.WithPrincipal(r.Context(), platform.Principal{UserID: "00000000-0000-7000-8000-000000000002", SystemRole: "admin"}))
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, r)
				if w.Code != 400 {
					t.Fatalf("invalid pagination must fail before accessing nil DB: status%d %s", w.Code, w.Body)
				}
			})
		}
	}
}
