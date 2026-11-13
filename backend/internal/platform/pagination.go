package platform

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strconv"
	"time"
)

type PageKey byte

const (
	PageText PageKey = iota
	PageUUID
	PageDate
	PageTime
)

type Page struct {
	Limit int
	After []string
	scope string
}
type pageCursor struct {
	Version int      `json:"v"`
	Scope   string   `json:"scope"`
	After   []string `json:"after"`
}

func ReadPage(r *http.Request, kinds ...PageKey) (Page, error) {
	q := r.URL.Query()
	p := Page{Limit: 20, After: make([]string, len(kinds))}
	if len(q["limit"]) > 1 || len(q["cursor"]) > 1 {
		return p, Invalid("Tham số phân trang bị lặp")
	}
	if _, present := q["limit"]; present {
		n, err := strconv.Atoi(q.Get("limit"))
		if err != nil || n < 1 || n > 100 {
			return p, Invalid("limit phải từ1đến100")
		}
		p.Limit = n
	}
	encoded := q.Get("cursor")
	q.Del("limit")
	q.Del("cursor")
	for _, values := range q {
		sort.Strings(values)
	}
	sum := sha256.Sum256([]byte(r.Method + "\n" + r.URL.Path + "\n" + Current(r).UserID + "\n" + q.Encode()))
	p.scope = hex.EncodeToString(sum[:])
	if encoded == "" {
		return p, nil
	}
	if len(encoded) > 4096 {
		return p, Invalid("Con trỏ không hợp lệ")
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return p, Invalid("Con trỏ không hợp lệ")
	}
	var c pageCursor
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&c) != nil || dec.Decode(&struct{}{}) != io.EOF || c.Version != 1 || c.Scope != p.scope || len(c.After) != len(kinds) {
		return p, Invalid("Con trỏ không hợp lệ")
	}
	for i, kind := range kinds {
		value := c.After[i]
		valid := len(value) <= 1024
		switch kind {
		case PageUUID:
			valid = valid && uuidKey(value)
		case PageDate:
			_, err := time.Parse("2006-01-02", value)
			valid = valid && err == nil
		case PageTime:
			at, err := time.Parse(time.RFC3339Nano, value)
			valid = valid && err == nil && !at.IsZero()
		case PageText:
		default:
			valid = false
		}
		if !valid {
			return p, Invalid("Khóa sắp xếp trong con trỏ không hợp lệ")
		}
	}
	p.After = c.After
	return p, nil
}
func (p Page) Next(keys ...string) string {
	raw, _ := json.Marshal(pageCursor{Version: 1, Scope: p.scope, After: keys})
	return base64.RawURLEncoding.EncodeToString(raw)
}
func WritePage(w http.ResponseWriter, data any, next string, now time.Time) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": 1000, "data": data, "meta": map[string]any{"nextCursor": next, "serverTime": now.UTC()}})
}
