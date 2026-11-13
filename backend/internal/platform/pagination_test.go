package platform

import (
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestPaginationDefaultsAndValidatesLimits(t *testing.T) {
	for _, tc := range []struct {
		query   string
		want    int
		invalid bool
	}{
		{"", 20, false}, {"limit=100", 100, false}, {"limit=1", 1, false}, {"limit=", 0, true}, {"limit=0", 0, true}, {"limit=101", 0, true}, {"limit=x", 0, true}, {"limit=1&limit=2", 0, true},
	} {
		p, err := ReadPage(httptest.NewRequest("GET", "/items?"+tc.query, nil), PageUUID)
		if (err != nil) != tc.invalid || (!tc.invalid && p.Limit != tc.want) {
			t.Errorf("query%s limit%d err%v", tc.query, p.Limit, err)
		}
	}
}

func TestPaginationRejectsUnknownVersionsAndInvalidCalendarKeys(t *testing.T) {
	r := httptest.NewRequest("GET", "/items", nil)
	p, err := ReadPage(r, PageDate, PageUUID)
	if err != nil {
		t.Fatal(err)
	}
	valid := p.Next("2026-11-13", ID())
	raw, err := base64.RawURLEncoding.DecodeString(valid)
	if err != nil {
		t.Fatal(err)
	}
	var cursor map[string]any
	if err = json.Unmarshal(raw, &cursor); err != nil {
		t.Fatal(err)
	}
	cursor["v"] = 2
	changed, err := json.Marshal(cursor)
	if err != nil {
		t.Fatal(err)
	}
	for _, encoded := range []string{p.Next("2026-02-30", ID()), base64.RawURLEncoding.EncodeToString(changed), base64.RawURLEncoding.EncodeToString(append(raw, []byte("{}")...))} {
		req := httptest.NewRequest("GET", "/items?cursor="+url.QueryEscape(encoded), nil)
		if _, err := ReadPage(req, PageDate, PageUUID); err == nil {
			t.Fatal("incompatible cursor accepted")
		}
	}
}
func TestPaginationMetadataPreservesDataAndUsesUTC(t *testing.T) {
	w := httptest.NewRecorder()
	WritePage(w, map[string]any{"items": []string{"Canh"}}, "next", time.Date(2026, 11, 13, 7, 0, 0, 0, time.FixedZone("VN", 7*3600)))
	var body struct {
		Code int
		Data struct{ Items []string }
		Meta struct {
			NextCursor string
			ServerTime string
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != 200 || body.Code != 1000 || len(body.Data.Items) != 1 || body.Meta.NextCursor != "next" || body.Meta.ServerTime != "2026-11-13T00:00:00Z" {
		t.Fatalf("metadata%d %s %v", w.Code, w.Body, err)
	}
}
func TestPaginationBindsCursorToScopeAndFiltersButAllowsPageResize(t *testing.T) {
	r := httptest.NewRequest("GET", "/spaces/one/recipes?q=canh&limit=2", nil)
	r = r.WithContext(WithPrincipal(r.Context(), Principal{UserID: "owner"}))
	p, err := ReadPage(r, PageText, PageUUID)
	if err != nil {
		t.Fatal(err)
	}
	id := ID()
	next := p.Next("Canh cà rốt", id)
	r2 := httptest.NewRequest("GET", "/spaces/one/recipes?q=canh&limit=1&cursor="+url.QueryEscape(next), nil)
	r2 = r2.WithContext(r.Context())
	resumed, err := ReadPage(r2, PageText, PageUUID)
	if err != nil || resumed.Limit != 1 || len(resumed.After) != 2 || resumed.After[0] != "Canh cà rốt" || resumed.After[1] != id {
		t.Fatalf("resumed%+v err%v", resumed, err)
	}
	for _, path := range []string{"/spaces/two/recipes?q=canh", "/spaces/one/recipes?q=soup"} {
		changed := httptest.NewRequest("GET", path+"&cursor="+url.QueryEscape(next), nil).WithContext(r.Context())
		if _, err := ReadPage(changed, PageText, PageUUID); err == nil {
			t.Fatal("changed scope/filter accepted")
		}
	}
	changed := r2.WithContext(WithPrincipal(r.Context(), Principal{UserID: "other"}))
	if _, err := ReadPage(changed, PageText, PageUUID); err == nil {
		t.Fatal("another user's cursor accepted")
	}
}
func TestPaginationRejectsMalformedOrWronglyTypedCursors(t *testing.T) {
	r := httptest.NewRequest("GET", "/items", nil)
	p, err := ReadPage(r, PageTime, PageUUID)
	if err != nil {
		t.Fatal(err)
	}
	for _, encoded := range []string{"%%%", strings.Repeat("x", 4097), p.Next("not-a-time", ID()), p.Next("2026-11-13T00:00:00Z", "bad-id"), p.Next(ID())} {
		req := httptest.NewRequest("GET", "/items?cursor="+url.QueryEscape(encoded), nil)
		if _, err := ReadPage(req, PageTime, PageUUID); err == nil {
			t.Fatal("invalid cursor accepted")
		}
	}
}
