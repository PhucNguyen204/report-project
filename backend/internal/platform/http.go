package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

type principalKey struct{}

func Current(r *http.Request) Principal {
	p, _ := r.Context().Value(principalKey{}).(Principal)
	return p
}
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}
func Decode(r *http.Request, dst any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1_048_577))
	var body json.RawMessage
	if err := dec.Decode(&body); err != nil || len(body) == 0 || body[0] != '{' {
		return Invalid("JSON không hợp lệ")
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return Invalid("JSON không hợp lệ")
	}
	dec = json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	if err := dec.Decode(dst); err != nil {
		return Invalid("JSON không hợp lệ")
	}
	return nil
}
func Auth(auth AuthenticateFunc, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" || publicAuth(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			Fail(w, Err(401, 3001, "Cần đăng nhập"))
			return
		}
		p, err := auth(r.Context(), strings.TrimPrefix(header, "Bearer "))
		if err != nil {
			Fail(w, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), p)))
	})
}

func publicAuth(path string) bool {
	switch path {
	case "/it4788/v1/auth/register", "/it4788/v1/auth/login", "/it4788/v1/auth/refresh", "/it4788/v1/auth/otp/verify", "/it4788/v1/auth/otp/resend", "/it4788/v1/auth/password/forgot", "/it4788/v1/auth/password/reset":
		return true
	}
	return false
}

type Handler func(http.ResponseWriter, *http.Request) error

func Handle(fn Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			Fail(w, err)
		}
	}
}
func Admin(r *http.Request) error {
	if Current(r).SystemRole != "admin" {
		return Forbidden()
	}
	return nil
}
