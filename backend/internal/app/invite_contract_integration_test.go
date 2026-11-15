//go:build integration

package app

import (
	"context"
	"strings"
	"testing"
)

func TestOpenAPIInviteLifecycleAndJoinPreviewMatchSchemas(t *testing.T) {
	h := newAPIHarness(t)
	owner, _, _ := h.user(t, "invite-contract-owner@example.test")
	guest, _, _ := h.user(t, "invite-contract-guest@example.test")
	group := h.call(t, "POST", "/it4788/v1/spaces", map[string]any{"name": "Gia đình kiểm thử"}, owner, "", 201)
	sid := field(group, "id")
	path := "/it4788/v1/spaces/" + sid + "/invites/current"
	none := h.call(t, "GET", path, nil, owner, "", 200)["data"].(map[string]any)
	if none["status"] != "none" || none["code"] != nil || none["expiresAt"] != nil || none["link"] != nil {
		t.Fatal("reading an absent invite created or exposed a code")
	}
	created := h.call(t, "POST", "/it4788/v1/spaces/"+sid+"/invites", map[string]any{}, owner, "", 201)
	code := field(created, "code")
	var firstExpiry any
	for attempt := range 2 {
		active := h.call(t, "GET", path, nil, owner, "", 200)["data"].(map[string]any)
		if attempt == 0 {
			firstExpiry = active["expiresAt"]
		}
		if active["status"] != "active" || active["code"] != code || active["expiresAt"] != firstExpiry {
			t.Fatal("invite reload rotated or extended the current code")
		}
	}
	join := "/it4788/v1/spaces/join"
	preview := h.call(t, "POST", join, map[string]any{"code": " " + strings.ToLower(code[:4]) + " " + strings.ToLower(code[4:]) + " ", "dryRun": true}, guest, "", 200)["data"].(map[string]any)
	if preview["leaderName"] != "Nguyễn An" || preview["memberCount"] != float64(1) || preview["maxMembers"] != float64(10) || preview["joinedAt"] != nil {
		t.Fatalf("join preview lacks confirmation metadata: %v", preview)
	}
	for range 2 {
		joined := h.call(t, "POST", join, map[string]any{"code": code}, guest, "", 200)["data"].(map[string]any)
		if joined["role"] != "member" || joined["joinedAt"] == nil {
			t.Fatal("confirmed membership has no role or join time")
		}
	}
	var joins, members int
	if err := h.system.DB.Pool.QueryRow(context.Background(), "SELECT (SELECT count(*) FROM platform.activities WHERE space_id=$1 AND kind='member.joined'),(SELECT count(*) FROM household.space_members WHERE space_id=$1)", sid).Scan(&joins, &members); err != nil || joins != 1 || members != 2 {
		t.Fatalf("repeated confirmation duplicated membership effects: joins%d members%d %v", joins, members, err)
	}
	if _, err := h.system.DB.Pool.Exec(context.Background(), "UPDATE household.invite_codes SET expires_at=$2 WHERE space_id=$1 AND revoked_at IS NULL", sid, h.system.DB.Clock.Now().AddDate(0, 0, -1)); err != nil {
		t.Fatal(err)
	}
	expired := h.call(t, "GET", path, nil, owner, "", 200)["data"].(map[string]any)
	if expired["status"] != "expired" || expired["code"] != code {
		t.Fatal("expired invite was replaced or hidden without an explicit action")
	}
	h.call(t, "DELETE", path, nil, owner, "", 200)
	revoked := h.call(t, "GET", path, nil, owner, "", 200)["data"].(map[string]any)
	if revoked["status"] != "none" || revoked["code"] != nil {
		t.Fatal("revoked code remained visible")
	}
}
