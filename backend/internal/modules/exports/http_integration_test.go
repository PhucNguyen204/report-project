//go:build integration

package exports

import (
	"context"
	"dicho/internal/platform"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Losing request scope or async status in the HTTP boundary exposes another member's export or starts an unqueued download.
func TestExportHTTPAsyncContractAndInputValidation(t *testing.T) {
	s, db, _, user, sid := setupExport(t)
	mux := http.NewServeMux()
	s.Register(mux)
	call := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req = req.WithContext(platform.WithPrincipal(context.Background(), platform.Principal{UserID: user}))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}
	base := "/it4788/v1/spaces/" + sid + "/reports/exports"
	b, _ := json.Marshal(exportInput(db, "csv"))
	w := call("POST", base, string(b))
	if w.Code != 202 {
		t.Fatalf("async %d %s", w.Code, w.Body)
	}
	var out struct {
		Code int    `json:"code"`
		Data Result `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Code != 1001 || out.Data.ID == "" {
		t.Fatalf("response %s", w.Body)
	}
	w = call("GET", base+"/"+out.Data.ID, "")
	if w.Code != 200 {
		t.Fatalf("poll %d %s", w.Code, w.Body)
	}
	if err := s.Handle(context.Background(), platform.Event{Event: "report.export", Payload: json.RawMessage(`{"exportId":"` + out.Data.ID + `"}`)}); err != nil {
		t.Fatal(err)
	}
	w = call("GET", base+"/"+out.Data.ID, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "downloadUrl") {
		t.Fatalf("ready %d %s", w.Code, w.Body)
	}
	for _, body := range []string{`{`, `{"unknown":true}`, `{"reportType":"spending","from":"2026-11-12","to":"2026-11-11","format":"csv"}`, `{"reportType":"spending","from":"2026-01-01","to":"2027-01-02","format":"csv"}`, `{"reportType":"spending","from":"2026-01-01","to":"2026-01-02","format":"exe"}`, `{"reportType":"invalid","from":"2026-01-01","to":"2026-01-02","format":"csv"}`, `{"reportType":"spending","from":"2026-01-01","to":"2026-01-02","format":"csv","groupBy":"hour"}`} {
		w = call("POST", base, body)
		if w.Code != 400 {
			t.Fatalf("invalid %s: %d %s", body, w.Code, w.Body)
		}
	}
	if exportJobs(t, db, "report.export") != 1 {
		t.Fatal("invalid requests created jobs")
	}
}
