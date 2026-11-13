//go:build integration

package accounts

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"dicho/internal/platform"
)

func privacyFixture(t *testing.T) *authFixture {
	f := setupAuth(t)
	mux := http.NewServeMux()
	f.s.Register(mux)
	f.s.RegisterProfile(mux)
	f.s.RegisterGroups(mux)
	f.s.RegisterPrivacy(mux)
	f.router = platform.Auth(f.s.Authenticate, mux)
	return f
}
func TestDeleteAccountRevokesImmediatelyAndAnonymizesAfterThirtyDays(t *testing.T) {
	f := privacyFixture(t)
	u := f.activate(t, "delete@example.com")
	token := u["accessToken"].(string)
	status, body := f.call(t, "DELETE", "/it4788/v1/me", map[string]any{"password": "WrongPass1"}, token)
	if status != 400 {
		t.Fatal(body)
	}
	status, body = f.call(t, "DELETE", "/it4788/v1/me", map[string]any{"password": "ValidPass1"}, token)
	if status != 200 {
		t.Fatalf("delete %d %+v", status, body)
	}
	if _, err := f.s.Authenticate(context.Background(), token); err == nil {
		t.Fatal("deleted token still valid")
	}
	payload, _ := json.Marshal(map[string]string{"userId": u["userId"].(string)})
	event := platform.Event{Event: "account.purge", Payload: payload}
	if err := f.s.HandleAccountPurge(context.Background(), event); err == nil {
		t.Fatal("anonymized before30days")
	}
	f.clock.at = f.clock.at.Add(30 * 24 * time.Hour)
	for range 2 {
		if err := f.s.HandleAccountPurge(context.Background(), event); err != nil {
			t.Fatal(err)
		}
	}
	var email, name string
	var n int
	err := f.db.Pool.QueryRow(context.Background(), "SELECT email,full_name FROM identity.users WHERE id=$1", u["userId"]).Scan(&email, &name)
	if err != nil || name != "Người dùng đã xóa" || email == "delete@example.com" {
		t.Fatalf("%s %s %v", email, name, err)
	}
	_ = f.db.Pool.QueryRow(context.Background(), "SELECT count(*) FROM identity.refresh_sessions WHERE user_id=$1", u["userId"]).Scan(&n)
	if n != 0 {
		t.Fatal("sessions retained")
	}
}
func TestDeleteAccountLeaderMustTransferAndDoesNotLoseMembership(t *testing.T) {
	f := privacyFixture(t)
	a := f.activate(t, "delete-leader@example.com")
	b := f.activate(t, "delete-member@example.com")
	token := a["accessToken"].(string)
	_, body := f.call(t, "POST", "/it4788/v1/spaces", map[string]any{"name": "Privacy group"}, token)
	sid := body["data"].(map[string]any)["id"].(string)
	_, body = f.call(t, "POST", "/it4788/v1/spaces/"+sid+"/invites", map[string]any{}, token)
	code := body["data"].(map[string]any)["code"]
	status, body := f.call(t, "POST", "/it4788/v1/spaces/join", map[string]any{"code": code}, b["accessToken"].(string))
	if status != 200 {
		t.Fatal(body)
	}
	status, body = f.call(t, "DELETE", "/it4788/v1/me", map[string]any{"password": "ValidPass1"}, token)
	if status != 422 {
		t.Fatalf("leader deleted %d %+v", status, body)
	}
	if _, err := f.s.Authenticate(context.Background(), token); err != nil {
		t.Fatalf("failed delete revoked session: %v", err)
	}
	var n int
	_ = f.db.Pool.QueryRow(context.Background(), "SELECT count(*) FROM household.space_members WHERE space_id=$1", sid).Scan(&n)
	if n != 2 {
		t.Fatal("failed delete lost members")
	}
}
