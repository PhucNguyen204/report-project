//go:build integration

package accounts

import (
	"context"
	"dicho/internal/platform"
	"testing"
	"time"
)

func TestPendingRegistrationResendExpiryAndLoginBoundaries(t *testing.T) {
	f := setupAuth(t)
	email := "pending-boundary@example.com"
	in := map[string]any{"fullName": "Nguyễn An", "email": email, "password": "ValidPass1"}
	status, body := f.call(t, "POST", "/it4788/v1/auth/register", in, "")
	if status != 201 {
		t.Fatal(body)
	}
	status, _ = f.call(t, "POST", "/it4788/v1/auth/login", map[string]any{"email": email, "password": "ValidPass1"}, "")
	if status != 403 {
		t.Fatal(status)
	}
	f.clock.at = f.clock.at.Add(time.Minute)
	status, body = f.call(t, "POST", "/it4788/v1/auth/register", in, "")
	if status != 200 {
		t.Fatal(body)
	}
	var n int
	if err := f.db.Pool.QueryRow(context.Background(), `SELECT count(*) FROM identity.users WHERE email=$1`, email).Scan(&n); err != nil || n != 1 {
		t.Fatalf("duplicate pending %d %v", n, err)
	}
	f.clock.at = f.clock.at.Add(time.Minute)
	status, body = f.call(t, "POST", "/it4788/v1/auth/otp/resend", map[string]any{"email": email}, "")
	if status != 200 {
		t.Fatal(body)
	}
	code := f.otp(t, email)
	f.clock.at = f.clock.at.Add(5 * time.Minute)
	status, _ = f.call(t, "POST", "/it4788/v1/auth/otp/verify", map[string]any{"email": email, "code": code}, "")
	if status != 400 {
		t.Fatal(status)
	}
	status, body = f.call(t, "POST", "/it4788/v1/auth/otp/resend", map[string]any{"email": email}, "")
	if status != 200 {
		t.Fatal(body)
	}
	status, body = f.call(t, "POST", "/it4788/v1/auth/otp/verify", map[string]any{"email": email, "code": f.otp(t, email)}, "")
	if status != 200 {
		t.Fatal(body)
	}
	status, _ = f.call(t, "POST", "/it4788/v1/auth/register", in, "")
	if status != 409 {
		t.Fatal(status)
	}
	status, _ = f.call(t, "POST", "/it4788/v1/auth/otp/resend", map[string]any{"email": email}, "")
	if status != 400 {
		t.Fatal(status)
	}
	status, _ = f.call(t, "POST", "/it4788/v1/auth/otp/resend", map[string]any{"email": "unknown@example.com"}, "")
	if status != 200 {
		t.Fatal(status)
	}
	for _, path := range []string{"verify", "resend"} {
		status, _ = f.call(t, "POST", "/it4788/v1/auth/otp/"+path, map[string]any{"email": email, "purpose": "bogus"}, "")
		if status != 400 {
			t.Fatal(status)
		}
	}
	status, _ = f.call(t, "POST", "/it4788/v1/auth/otp/verify", map[string]any{"email": "unknown@example.com", "code": "123456"}, "")
	if status != 400 {
		t.Fatal(status)
	}
	status, _ = f.call(t, "POST", "/it4788/v1/auth/login", map[string]any{"email": "unknown@example.com", "password": "ValidPass1"}, "")
	if status != 401 {
		t.Fatal(status)
	}
	status, _ = f.call(t, "POST", "/it4788/v1/auth/login", map[string]any{"email": email, "password": "ValidPass1", "platform": "web"}, "")
	if status != 400 {
		t.Fatal(status)
	}
}

func TestRefreshRejectsWrongDeviceUnknownAndExpiredCredentials(t *testing.T) {
	f := setupAuth(t)
	u := f.activate(t, "refresh-bounds@example.com")
	token := u["refreshToken"].(string)
	device := u["deviceId"].(string)
	for _, in := range []map[string]any{{"refreshToken": "unknown", "deviceId": device}, {"refreshToken": token, "deviceId": platform.ID()}} {
		status, _ := f.call(t, "POST", "/it4788/v1/auth/refresh", in, "")
		if status != 401 {
			t.Fatal(status)
		}
	}
	status, body := f.call(t, "POST", "/it4788/v1/auth/refresh", map[string]any{"refreshToken": token, "deviceId": device}, "")
	if status != 200 {
		t.Fatal(body)
	}
	next := body["data"].(map[string]any)
	status, body = f.call(t, "POST", "/it4788/v1/auth/refresh", map[string]any{"refreshToken": next["refreshToken"], "deviceId": device}, "")
	if status != 200 {
		t.Fatal(body)
	}
	status, _ = f.call(t, "POST", "/it4788/v1/auth/refresh", map[string]any{"refreshToken": token, "deviceId": device}, "")
	if status != 401 {
		t.Fatal(status)
	}
	f.clock.at = f.clock.at.Add(31 * 24 * time.Hour)
	status, _ = f.call(t, "POST", "/it4788/v1/auth/refresh", map[string]any{"refreshToken": next["refreshToken"], "deviceId": device}, "")
	if status != 401 {
		t.Fatal(status)
	}
}

func TestMalformedBodiesAreRejectedBeforeMutatingAuthenticatedState(t *testing.T) {
	f := privacyFixture(t)
	u := f.activate(t, "malformed-account@example.com")
	token := u["accessToken"].(string)
	sid := u["spaceId"].(string)
	for _, route := range []struct{ method, path string }{
		{"POST", "/auth/register"}, {"POST", "/auth/login"}, {"POST", "/auth/otp/verify"}, {"POST", "/auth/otp/resend"}, {"POST", "/auth/refresh"},
		{"PATCH", "/me"}, {"PUT", "/me/settings"}, {"PUT", "/me/password"}, {"DELETE", "/me"}, {"POST", "/me/devices"},
		{"POST", "/auth/password/forgot"}, {"POST", "/auth/password/reset"}, {"POST", "/spaces"}, {"PATCH", "/spaces/" + sid}, {"POST", "/spaces/join"}, {"POST", "/spaces/" + sid + "/leader-transfer"},
	} {
		status, _ := f.call(t, route.method, "/it4788/v1"+route.path, []any{1}, token)
		if status != 400 {
			t.Fatalf("%s %s got%d", route.method, route.path, status)
		}
	}
	if _, err := f.s.Authenticate(context.Background(), token); err != nil {
		t.Fatal("malformed account request revoked token", err)
	}
	var name string
	if err := f.db.Pool.QueryRow(context.Background(), `SELECT full_name FROM identity.users WHERE id=$1`, u["userId"]).Scan(&name); err != nil || name != "Nguyễn An" {
		t.Fatalf("malformed profile changed %s %v", name, err)
	}
}
