//go:build integration

package media

import (
	"context"
	"dicho/internal/platform"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Omitting target authorization in the public upload handler exposes foreign avatars.
func TestUploadHTTPValidatesTargetAndCompletes(t *testing.T) {
	s, _, _, user, _ := setupMedia(t)
	mux := http.NewServeMux()
	s.Register(mux)
	call := func(path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest("POST", path, strings.NewReader(body))
		req = req.WithContext(platform.WithPrincipal(context.Background(), platform.Principal{UserID: user}))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}
	w := call("/it4788/v1/media/upload-urls", `{"purpose":"avatar","targetId":"`+platform.ID()+`","contentType":"image/png","size":20}`)
	if w.Code != 404 {
		t.Fatalf("foreign target %d %s", w.Code, w.Body)
	}
	w = call("/it4788/v1/media/upload-urls", `{"purpose":"avatar","targetId":"`+user+`","contentType":"image/png","size":`+jsonNumber(len(picture(t)))+`}`)
	if w.Code != 201 {
		t.Fatalf("grant %d %s", w.Code, w.Body)
	}
	var out struct {
		Data Grant `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Data.URL == "" {
		t.Fatal("missing signed URL")
	}
	putImage(t, out.Data, picture(t))
	w = call("/it4788/v1/media/"+out.Data.ObjectID+"/complete", `{}`)
	if w.Code != 200 {
		t.Fatalf("complete %d %s", w.Code, w.Body)
	}
	read := httptest.NewRequest("GET", "/it4788/v1/media/"+out.Data.ObjectID, nil)
	read = read.WithContext(platform.WithPrincipal(context.Background(), platform.Principal{UserID: user}))
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, read)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "downloadUrl") {
		t.Fatalf("read %d %s", w.Code, w.Body)
	}
	w = call("/it4788/v1/media/upload-urls", `{"unknown":true}`)
	if w.Code != 400 {
		t.Fatalf("malformed %d", w.Code)
	}
}

func jsonNumber(n int) string { b, _ := json.Marshal(n); return string(b) }
