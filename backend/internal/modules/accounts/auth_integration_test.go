//go:build integration

package accounts

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"dicho/internal/platform"
	"dicho/internal/testutil"
)

type fixedClock struct{ at time.Time }

func (c *fixedClock) Now() time.Time { return c.at }

type authFixture struct {
	db     *platform.DB
	s      *Service
	clock  *fixedClock
	router http.Handler
}

func setupAuth(t *testing.T) *authFixture {
	t.Helper()
	db := testutil.NewDB(t)
	clock := &fixedClock{time.Date(2026, 11, 9, 6, 0, 0, 0, time.UTC)}
	db.Clock = clock
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	s := New(db, key)
	mux := http.NewServeMux()
	s.Register(mux)
	if feature, ok := any(s).(interface{ RegisterCatalog(*http.ServeMux) }); ok {
		feature.RegisterCatalog(mux)
	}
	if feature, ok := any(s).(interface{ RegisterAdmin(*http.ServeMux) }); ok {
		feature.RegisterAdmin(mux)
	}
	if feature, ok := any(s).(interface{ RegisterProfile(*http.ServeMux) }); ok {
		feature.RegisterProfile(mux)
	}
	if feature, ok := any(s).(interface{ RegisterGroups(*http.ServeMux) }); ok {
		feature.RegisterGroups(mux)
	}
	return &authFixture{db, s, clock, platform.Auth(s.Authenticate, mux)}
}
func (f *authFixture) call(t *testing.T, method, url string, payload any, token string) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(payload)
	r := httptest.NewRequest(method, url, bytes.NewReader(b))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("%s %s: %s", method, url, w.Body.String())
	}
	return w.Code, result
}
func (f *authFixture) otp(t *testing.T, email string) string {
	t.Helper()
	var code string
	if err := f.db.Pool.QueryRow(context.Background(), "SELECT args->'payload'->>'code' FROM river_job WHERE args->'payload'->>'to'=$1 ORDER BY id DESC LIMIT 1", email).Scan(&code); err != nil {
		t.Fatal(err)
	}
	return code
}
func (f *authFixture) activate(t *testing.T, email string) map[string]any {
	t.Helper()
	status, body := f.call(t, "POST", "/it4788/v1/auth/register", map[string]any{"fullName": "Nguyễn An", "email": email, "password": "ValidPass1"}, "")
	if status != 201 {
		t.Fatalf("register %d %+v", status, body)
	}
	status, body = f.call(t, "POST", "/it4788/v1/auth/otp/verify", map[string]any{"email": email, "purpose": "register", "code": f.otp(t, email), "deviceId": platform.ID(), "platform": "android"}, "")
	if status != 200 {
		t.Fatalf("verify %d %+v", status, body)
	}
	return body["data"].(map[string]any)
}
func TestRegistrationCreatesPersonalSpaceOnlyAfterOTP(t *testing.T) {
	f := setupAuth(t)
	status, body := f.call(t, "POST", "/it4788/v1/auth/register", map[string]any{"fullName": "Nguyễn An", "email": "an@example.com", "password": "ValidPass1"}, "")
	if status != 201 {
		t.Fatalf("%d %+v", status, body)
	}
	data := body["data"].(map[string]any)
	if _, ok := data["code"]; ok {
		t.Fatal("exposed OTP")
	}
	var count int
	if err := f.db.Pool.QueryRow(context.Background(), "SELECT count(*) FROM household.spaces").Scan(&count); err != nil || count != 0 {
		t.Fatalf("activated before OTP: %d %v", count, err)
	}
	status, body = f.call(t, "POST", "/it4788/v1/auth/otp/verify", map[string]any{"email": "an@example.com", "purpose": "register", "code": f.otp(t, "an@example.com"), "deviceId": platform.ID(), "platform": "android"}, "")
	if status != 200 {
		t.Fatalf("%d %+v", status, body)
	}
	if err := f.db.Pool.QueryRow(context.Background(), "SELECT count(*) FROM household.spaces WHERE kind='personal'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("personal spaces %d %v", count, err)
	}
	token := body["data"].(map[string]any)["accessToken"].(string)
	if _, err := f.s.Authenticate(context.Background(), token); err != nil {
		t.Fatal(err)
	}
	status, _ = f.call(t, "POST", "/it4788/v1/auth/otp/verify", map[string]any{"email": "an@example.com", "purpose": "register", "code": f.otp(t, "an@example.com")}, "")
	if status != 400 {
		t.Fatalf("OTP reused status %d", status)
	}
}
func TestOTPFailuresPersistAndResendIsThrottled(t *testing.T) {
	f := setupAuth(t)
	f.call(t, "POST", "/it4788/v1/auth/register", map[string]any{"fullName": "Nguyễn An", "email": "an@example.com", "password": "ValidPass1"}, "")
	status, _ := f.call(t, "POST", "/it4788/v1/auth/otp/resend", map[string]any{"email": "an@example.com", "purpose": "register"}, "")
	if status != 429 {
		t.Fatalf("resend status %d", status)
	}
	actual := f.otp(t, "an@example.com")
	bad := "000000"
	if actual == bad {
		bad = "111111"
	}
	for i := 0; i < 5; i++ {
		status, _ = f.call(t, "POST", "/it4788/v1/auth/otp/verify", map[string]any{"email": "an@example.com", "purpose": "register", "code": bad}, "")
		if status != 400 {
			t.Fatalf("OTP fail %d status %d", i, status)
		}
	}
	status, _ = f.call(t, "POST", "/it4788/v1/auth/otp/verify", map[string]any{"email": "an@example.com", "purpose": "register", "code": actual}, "")
	if status != 400 {
		t.Fatal("OTP still usable after fifth wrong attempt")
	}
	var failures int
	f.db.Pool.QueryRow(context.Background(), "SELECT attempts FROM identity.otp_challenges").Scan(&failures)
	if failures != 5 {
		t.Fatalf("lost counters %d", failures)
	}
}
func TestLoginLockoutPersistsAndLogoutRevokesOnlyCurrentFamily(t *testing.T) {
	f := setupAuth(t)
	first := f.activate(t, "an@example.com")
	device := platform.ID()
	login := map[string]any{"email": "an@example.com", "password": "ValidPass1", "deviceId": device, "platform": "android"}
	status, body := f.call(t, "POST", "/it4788/v1/auth/login", login, "")
	if status != 200 {
		t.Fatalf("login %d %+v", status, body)
	}
	second := body["data"].(map[string]any)
	firstToken := first["accessToken"].(string)
	status, body = f.call(t, "POST", "/it4788/v1/auth/logout", map[string]any{}, firstToken)
	if status != 200 {
		t.Fatalf("logout %d %+v", status, body)
	}
	if _, err := f.s.Authenticate(context.Background(), firstToken); err == nil {
		t.Fatal("logout token remains usable")
	}
	if _, err := f.s.Authenticate(context.Background(), second["accessToken"].(string)); err != nil {
		t.Fatal("logout revoked other device", err)
	}
	login["password"] = "WrongPassword1"
	for i := 0; i < 5; i++ {
		status, body = f.call(t, "POST", "/it4788/v1/auth/login", login, "")
		if status != 401 && status != 423 {
			t.Fatalf("failure %d: %d %+v", i, status, body)
		}
	}
	login["password"] = "ValidPass1"
	status, _ = f.call(t, "POST", "/it4788/v1/auth/login", login, "")
	if status != 423 {
		t.Fatalf("not locked: %d", status)
	}
	f.clock.at = f.clock.at.Add(15 * time.Minute)
	status, body = f.call(t, "POST", "/it4788/v1/auth/login", login, "")
	if status != 200 {
		t.Fatalf("not unlocked %d %+v", status, body)
	}
}
func TestRefreshReuseGraceThenRevokesAllExistingAccess(t *testing.T) {
	f := setupAuth(t)
	first := f.activate(t, "an@example.com")
	device := first["deviceId"].(string)
	request := map[string]any{"refreshToken": first["refreshToken"], "deviceId": device}
	status, body := f.call(t, "POST", "/it4788/v1/auth/refresh", request, "")
	if status != 200 {
		t.Fatalf("rotate %d %+v", status, body)
	}
	second := body["data"].(map[string]any)
	status, body = f.call(t, "POST", "/it4788/v1/auth/refresh", request, "")
	if status != 200 {
		t.Fatalf("grace %d %+v", status, body)
	}
	f.clock.at = f.clock.at.Add(31 * time.Second)
	status, body = f.call(t, "POST", "/it4788/v1/auth/refresh", request, "")
	if status != 401 || fmt.Sprint(body["code"]) != "3008" {
		t.Fatalf("reuse %d %+v", status, body)
	}
	if _, err := f.s.Authenticate(context.Background(), second["accessToken"].(string)); err == nil {
		t.Fatal("reuse failed to revoke JWT")
	}
}
