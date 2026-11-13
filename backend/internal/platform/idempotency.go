package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
)

type transactionKey struct{}

func Idempotency(db *DB, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := Current(r)
		if r.Method == "GET" || r.Method == "HEAD" || r.Method == "OPTIONS" || p.UserID == "" || publicAuth(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method == "PUT" && r.URL.Path == "/it4788/v1/me/password" {
			next.ServeHTTP(w, r)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 1_048_577))
		if err != nil || len(body) > 1_048_576 {
			Fail(w, Err(413, 2002, "Dữ liệu quá lớn"))
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		if previewRequest(r, body) {
			next.ServeHTTP(w, r)
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if !uuidKey(key) {
			Fail(w, Invalid("Idempotency-Key phải là UUID"))
			return
		}
		key = strings.ToLower(key)
		sum := sha256.Sum256(append([]byte(r.Method+" "+r.URL.RequestURI()+"\n"), body...))
		hash := hex.EncodeToString(sum[:])
		if r.Method == "POST" && strings.HasPrefix(r.URL.Path, "/it4788/v1/media/") && strings.HasSuffix(r.URL.Path, "/complete") {
			externalCommand(db, next, w, r, key, hash)
			return
		}
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
				var pe *pgconn.PgError
				if errors.As(e, &pe) && pe.Code == "55P03" {
					return Err(409, 4002, "Yêu cầu cùng khóa đang được xử lý")
				}
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
				var cachedHeaders http.Header
				_ = json.Unmarshal(headers, &cachedHeaders)
				for name, values := range cachedHeaders {
					result.Header()[name] = values
				}
				result.Header().Set("Idempotent-Replayed", "true")
				result.WriteHeader(status)
				_, _ = result.Write(cached)
				return nil
			}
			result = commandRecorder(w)
			r.Body = io.NopCloser(bytes.NewReader(body))
			ctx := context.WithValue(r.Context(), transactionKey{}, tx)
			next.ServeHTTP(result, r.WithContext(ctx))
			if result.Code >= 400 {
				return &responseError{}
			}
			headers := cacheHeaders(result.Header())
			_, e = tx.Exec(r.Context(), "UPDATE platform.idempotency_keys SET status=$3,response=$4,headers=$5 WHERE user_id=$1 AND key=$2", p.UserID, key, result.Code, result.Body.Bytes(), headers)
			return e
		})
		if _, ok := err.(*responseError); err != nil && !ok {
			var typed *Error
			if errors.As(err, &typed) && typed.Status == 409 {
				w.Header().Set("Retry-After", "1")
			}
			Fail(w, err)
			return
		}
		if result == nil {
			Fail(w, Err(500, 5000, "Không có phản hồi"))
			return
		}
		for k, values := range result.Header() {
			if strings.EqualFold(k, "X-Trace-ID") || strings.EqualFold(k, "X-Request-ID") {
				continue
			}
			for _, v := range values {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(result.Code)
		_, _ = w.Write(result.Body.Bytes())
	})
}

func uuidKey(key string) bool {
	if len(key) != 36 || key[8] != '-' || key[13] != '-' || key[18] != '-' || key[23] != '-' {
		return false
	}
	_, err := hex.DecodeString(strings.ReplaceAll(key, "-", ""))
	return err == nil
}

func externalCommand(db *DB, next http.Handler, w http.ResponseWriter, r *http.Request, key, hash string) {
	replay := false
	err := db.Tx(r.Context(), func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), "INSERT INTO platform.idempotency_keys(user_id,key,request_hash,status) VALUES($1,$2,$3,102) ON CONFLICT DO NOTHING", Current(r).UserID, key, hash); err != nil {
			return err
		}
		var old string
		var status int
		if err := tx.QueryRow(r.Context(), "SELECT request_hash,status FROM platform.idempotency_keys WHERE user_id=$1 AND key=$2", Current(r).UserID, key).Scan(&old, &status); err != nil {
			return err
		}
		if old != hash {
			return Invalid("Idempotency-Key đã dùng cho yêu cầu khác")
		}
		replay = status >= 200 && status < 300
		return nil
	})
	if err != nil {
		Fail(w, err)
		return
	}
	result := commandRecorder(w)
	next.ServeHTTP(result, r)
	err = db.Tx(r.Context(), func(tx pgx.Tx) error {
		if result.Code >= 400 {
			_, err := tx.Exec(r.Context(), "DELETE FROM platform.idempotency_keys WHERE user_id=$1 AND key=$2 AND status=102", Current(r).UserID, key)
			return err
		}
		headers := cacheHeaders(result.Header())
		_, err := tx.Exec(r.Context(), "UPDATE platform.idempotency_keys SET status=$3,response=$4,headers=$5 WHERE user_id=$1 AND key=$2", Current(r).UserID, key, result.Code, result.Body.Bytes(), headers)
		return err
	})
	if err != nil {
		Fail(w, err)
		return
	}
	if replay && result.Code < 400 {
		result.Header().Set("Idempotent-Replayed", "true")
	}
	for k, values := range result.Header() {
		if strings.EqualFold(k, "X-Trace-ID") || strings.EqualFold(k, "X-Request-ID") {
			continue
		}
		for _, v := range values {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(result.Code)
	_, _ = w.Write(result.Body.Bytes())
}

func commandRecorder(w http.ResponseWriter) *httptest.ResponseRecorder {
	result := httptest.NewRecorder()
	if id := w.Header().Get("X-Trace-ID"); id != "" {
		result.Header().Set("X-Trace-ID", id)
	}
	return result
}

func cacheHeaders(headers http.Header) []byte {
	copy := headers.Clone()
	copy.Del("X-Trace-ID")
	copy.Del("X-Request-ID")
	raw, _ := json.Marshal(copy)
	return raw
}

type responseError struct{}

func (*responseError) Error() string { return "business request failed" }

func previewRequest(r *http.Request, body []byte) bool {
	if r.Method != "POST" {
		return false
	}
	path := r.URL.Path
	if path != "/it4788/v1/spaces/join" && !strings.HasSuffix(path, "/shopping-lists/generate") && !strings.HasSuffix(path, "/missing-to-list") {
		return false
	}
	var in struct {
		DryRun bool `json:"dryRun"`
	}
	return json.Unmarshal(body, &in) == nil && in.DryRun
}
