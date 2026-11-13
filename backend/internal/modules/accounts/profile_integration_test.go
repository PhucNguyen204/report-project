//go:build integration

package accounts

import (
	"context"
	"testing"
	"time"
)

func TestProfileAndReminderSettings(t *testing.T) {
	f := setupAuth(t)
	user := f.activate(t, "profile@example.com")
	token := user["accessToken"].(string)
	status, body := f.call(t, "GET", "/it4788/v1/me", nil, token)
	if status != 200 {
		t.Fatalf("profile %d %+v", status, body)
	}
	status, body = f.call(t, "PATCH", "/it4788/v1/me", map[string]any{"fullName": "Trần Bình", "locale": "en"}, token)
	if status != 200 {
		t.Fatalf("update %d %+v", status, body)
	}
	status, body = f.call(t, "GET", "/it4788/v1/me", nil, token)
	if body["data"].(map[string]any)["fullName"] != "Trần Bình" {
		t.Fatal(body)
	}
	status, _ = f.call(t, "PATCH", "/it4788/v1/me", map[string]any{"fullName": "X"}, token)
	if status != 400 {
		t.Fatal(status)
	}
	status, body = f.call(t, "PUT", "/it4788/v1/me/settings", map[string]any{"reminderDays": 7, "expiryNotifyOn": false}, token)
	if status != 200 {
		t.Fatalf("settings %d %+v", status, body)
	}
	status, body = f.call(t, "GET", "/it4788/v1/me/settings", nil, token)
	data := body["data"].(map[string]any)
	if data["reminderDays"] != float64(7) || data["expiryNotifyOn"] != false || data["reminderRunAt"] != "08:00" {
		t.Fatal(body)
	}
	status, _ = f.call(t, "PUT", "/it4788/v1/me/settings", map[string]any{"reminderDays": 8}, token)
	if status != 400 {
		t.Fatal(status)
	}
	status, body = f.call(t, "PUT", "/it4788/v1/me/settings", map[string]any{"reminderDays": nil}, token)
	if status != 200 {
		t.Fatalf("default %d %+v", status, body)
	}
}

func TestPasswordResetSingleUseAndChangeRevokesOtherDevices(t *testing.T) {
	f := setupAuth(t)
	user := f.activate(t, "password@example.com")
	token := user["accessToken"].(string)
	status, body := f.call(t, "POST", "/it4788/v1/auth/login", map[string]any{"email": "password@example.com", "password": "ValidPass1"}, "")
	if status != 200 {
		t.Fatal(body)
	}
	other := body["data"].(map[string]any)["accessToken"].(string)
	status, body = f.call(t, "PUT", "/it4788/v1/me/password", map[string]any{"currentPassword": "ValidPass1", "newPassword": "NewValidPass2"}, token)
	if status != 200 {
		t.Fatalf("change %d %+v", status, body)
	}
	if _, err := f.s.Authenticate(context.Background(), other); err == nil {
		t.Fatal("other device kept access")
	}
	newToken := body["data"].(map[string]any)["accessToken"].(string)
	if _, err := f.s.Authenticate(context.Background(), newToken); err != nil {
		t.Fatal(err)
	}
	f.clock.at = f.clock.at.Add(time.Minute)
	status, body = f.call(t, "POST", "/it4788/v1/auth/password/forgot", map[string]any{"email": "password@example.com"}, "")
	if status != 200 {
		t.Fatal(body)
	}
	status, body = f.call(t, "POST", "/it4788/v1/auth/otp/verify", map[string]any{"email": "password@example.com", "purpose": "reset_password", "code": f.otp(t, "password@example.com")}, "")
	if status != 200 {
		t.Fatal(body)
	}
	reset := body["data"].(map[string]any)["resetToken"].(string)
	payload := map[string]any{"email": "password@example.com", "resetToken": reset, "newPassword": "ResetValid3"}
	status, body = f.call(t, "POST", "/it4788/v1/auth/password/reset", payload, "")
	if status != 200 {
		t.Fatalf("reset %d %+v", status, body)
	}
	status, _ = f.call(t, "POST", "/it4788/v1/auth/password/reset", payload, "")
	if status != 400 {
		t.Fatalf("reset reused: %d", status)
	}
	if _, err := f.s.Authenticate(context.Background(), newToken); err == nil {
		t.Fatal("reset kept old access")
	}
	status, body = f.call(t, "POST", "/it4788/v1/auth/password/forgot", map[string]any{"email": "missing@example.com"}, "")
	if status != 200 {
		t.Fatal(body)
	}
}
