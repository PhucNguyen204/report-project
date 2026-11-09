package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
)

type transactionKey struct{}

func Idempotency(db *DB, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := Current(r)
		if r.Method == "GET" || r.Method == "HEAD" || r.Method == "OPTIONS" || p.UserID == "" || strings.HasPrefix(r.URL.Path, "/it4788/v1/auth/") {
			next.ServeHTTP(w, r)
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if key == "" || len(key) > 128 {
			Fail(w, Invalid("Idempotency-Key bắt buộc, tối đa128 ký tự"))
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1_048_577))
		if err != nil || len(body) > 1_048_576 {
			Fail(w, Err(413, 2002, "Dữ liệu quá lớn"))
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		sum := sha256.Sum256(append([]byte(r.Method+" "+r.URL.RequestURI()+"\n"), body...))
		hash := hex.EncodeToString(sum[:])
		var result *httptest.ResponseRecorder
		err = db.Tx(r.Context(), func(tx pgx.Tx) error {
			segments := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			if len(segments) >= 4 && segments[3] != "join" && segments[2] == "spaces" {
				if p.SystemRole == "admin" {
					return NotFound()
				}
				if _, e := db.RequireMember(r.Context(), tx, segments[3], p.UserID); e != nil {
					return e
				}
			}
			tag, e := tx.Exec(r.Context(), "INSERT INTO platform.idempotency_keys(user_id,key,request_hash) VALUES($1,$2,$3) ON CONFLICT DO NOTHING", p.UserID, key, hash)
			if e != nil {
				return e
			}
			if tag.RowsAffected() == 0 {
				var old string
				var status int
				var cached []byte
				var headers []byte
				if e = tx.QueryRow(r.Context(), "SELECT request_hash,status,response,headers FROM platform.idempotency_keys WHERE user_id=$1 AND key=$2", p.UserID, key).Scan(&old, &status, &cached, &headers); e != nil {
					return e
				}
				if old != hash {
					return Invalid("Idempotency-Key đã dùng cho yêu cầu khác")
				}
				result = httptest.NewRecorder()
				r.Body = io.NopCloser(bytes.NewReader(body))
				_ = json.Unmarshal(headers, &result.HeaderMap)
				result.Header().Set("Idempotent-Replayed", "true")
				result.WriteHeader(status)
				_, _ = result.Write(cached)
				return nil
			}
			result = httptest.NewRecorder()
			ctx := context.WithValue(r.Context(), transactionKey{}, tx)
			next.ServeHTTP(result, r.WithContext(ctx))
			if result.Code >= 400 {
				return &responseError{}
			}
			headers, _ := json.Marshal(result.Header())
			_, e = tx.Exec(r.Context(), "UPDATE platform.idempotency_keys SET status=$3,response=$4,headers=$5 WHERE user_id=$1 AND key=$2", p.UserID, key, result.Code, result.Body.Bytes(), headers)
			return e
		})
		if _, ok := err.(*responseError); err != nil && !ok {
			Fail(w, err)
			return
		}
		if result == nil {
			Fail(w, Err(500, 5000, "Không có phản hồi"))
			return
		}
		for k, values := range result.Header() {
			for _, v := range values {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(result.Code)
		_, _ = w.Write(result.Body.Bytes())
	})
}

type responseError struct{}

func (*responseError) Error() string { return "business request failed" }
