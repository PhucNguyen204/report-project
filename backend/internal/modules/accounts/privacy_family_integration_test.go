//go:build integration

package accounts

import (
	"context"
	"dicho/internal/platform"
	"encoding/json"
	"testing"
	"time"
)

func TestDeletedSoleLeaderDissolvesFamilyImmediatelyAndSchedulesDelayedPersonalPurge(t *testing.T) {
	f := privacyFixture(t)
	ctx := context.Background()
	u := f.activate(t, "delete-sole@example.com")
	token := u["accessToken"].(string)
	status, body := f.call(t, "POST", "/it4788/v1/spaces", map[string]any{"name": "Sole family"}, token)
	if status != 201 {
		t.Fatal(body)
	}
	sid := body["data"].(map[string]any)["id"].(string)
	if status, body = f.call(t, "POST", "/it4788/v1/spaces/"+sid+"/invites", map[string]any{}, token); status != 201 {
		t.Fatal(body)
	}
	if status, body = f.call(t, "POST", "/it4788/v1/me/devices", map[string]any{"fcmToken": "delete-device-token", "platform": "android"}, token); status != 200 {
		t.Fatal(body)
	}
	status, body = f.call(t, "DELETE", "/it4788/v1/me", map[string]any{"password": "ValidPass1"}, token)
	if status != 200 {
		t.Fatal(body)
	}
	var dissolved, revoked bool
	var members, devices, purges int
	if err := f.db.Pool.QueryRow(ctx, `SELECT dissolved_at IS NOT NULL FROM household.spaces WHERE id=$1`, sid).Scan(&dissolved); err != nil || !dissolved {
		t.Fatalf("dissolved %v %v", dissolved, err)
	}
	if err := f.db.Pool.QueryRow(ctx, `SELECT revoked_at IS NOT NULL FROM household.invite_codes WHERE space_id=$1`, sid).Scan(&revoked); err != nil || !revoked {
		t.Fatalf("invite %v %v", revoked, err)
	}
	if err := f.db.Pool.QueryRow(ctx, `SELECT count(*) FROM household.space_members WHERE user_id=$1 AND space_kind='family'`, u["userId"]).Scan(&members); err != nil || members != 0 {
		t.Fatalf("family membership %d %v", members, err)
	}
	if err := f.db.Pool.QueryRow(ctx, `SELECT count(*) FROM identity.devices WHERE user_id=$1`, u["userId"]).Scan(&devices); err != nil || devices != 0 {
		t.Fatalf("devices %d %v", devices, err)
	}
	if err := f.db.Pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE args->>'event'='space.purge'`).Scan(&purges); err != nil || purges != 1 {
		t.Fatalf("immediate purge %d %v", purges, err)
	}
	payload, _ := json.Marshal(map[string]any{"userId": u["userId"]})
	f.clock.at = f.clock.at.Add(30 * 24 * time.Hour)
	if err := f.s.HandleAccountPurge(ctx, platform.Event{Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE args->>'event'='space.purge'`).Scan(&purges); err != nil || purges != 2 {
		t.Fatalf("personal purge %d %v", purges, err)
	}
}

func TestAccountPurgeRejectsMalformedAndNondeletedUsers(t *testing.T) {
	f := privacyFixture(t)
	u := f.activate(t, "purge-live@example.com")
	for _, raw := range [][]byte{[]byte("{"), []byte("{}")} {
		if err := f.s.HandleAccountPurge(context.Background(), platform.Event{Payload: raw}); err == nil {
			t.Fatal("malformed purge accepted")
		}
	}
	raw, _ := json.Marshal(map[string]any{"userId": u["userId"]})
	if err := f.s.HandleAccountPurge(context.Background(), platform.Event{Payload: raw}); err == nil {
		t.Fatal("active user anonymized")
	}
	if _, err := f.s.Authenticate(context.Background(), u["accessToken"].(string)); err != nil {
		t.Fatal("rejected purge revoked user", err)
	}
}
