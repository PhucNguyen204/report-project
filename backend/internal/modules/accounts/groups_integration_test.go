//go:build integration

package accounts

import (
	"context"
	"github.com/jackc/pgx/v5"
	"sync"
	"testing"
)

func TestGroupInviteJoinAndLeaderLifecycle(t *testing.T) {
	f := setupAuth(t)
	leader := f.activate(t, "leader@example.com")
	member := f.activate(t, "member@example.com")
	lt := leader["accessToken"].(string)
	mt := member["accessToken"].(string)
	status, body := f.call(t, "POST", "/it4788/v1/spaces", map[string]any{"name": "Gia đình An"}, lt)
	if status != 201 {
		t.Fatalf("create %d %+v", status, body)
	}
	sid := body["data"].(map[string]any)["id"].(string)
	base := "/it4788/v1/spaces/" + sid
	status, body = f.call(t, "POST", base+"/invites", map[string]any{}, lt)
	if status != 201 {
		t.Fatalf("invite %d %+v", status, body)
	}
	code := body["data"].(map[string]any)["code"].(string)
	status, body = f.call(t, "POST", "/it4788/v1/spaces/join", map[string]any{"code": code, "dryRun": true}, mt)
	if status != 200 {
		t.Fatalf("preview %d %+v", status, body)
	}
	var n int
	_ = f.db.Pool.QueryRow(context.Background(), "SELECT count(*) FROM household.space_members WHERE space_id=$1", sid).Scan(&n)
	if n != 1 {
		t.Fatal("preview mutated membership")
	}
	status, body = f.call(t, "POST", "/it4788/v1/spaces/join", map[string]any{"code": code}, mt)
	if status != 200 {
		t.Fatalf("join %d %+v", status, body)
	}
	status, _ = f.call(t, "POST", base+"/leave", map[string]any{}, lt)
	if status != 422 {
		t.Fatalf("leader left group with member: %d", status)
	}
	status, _ = f.call(t, "POST", base+"/invites", map[string]any{}, mt)
	if status != 403 {
		t.Fatalf("member invited: %d", status)
	}
	status, body = f.call(t, "POST", base+"/leader-transfer", map[string]any{"userId": member["userId"]}, lt)
	if status != 200 {
		t.Fatalf("transfer %d %+v", status, body)
	}
	_, err := f.db.Pool.Exec(context.Background(), "CREATE TABLE test_leave(user_id uuid)")
	if err != nil {
		t.Fatal(err)
	}
	f.db.MemberLeave = func(ctx context.Context, tx pgx.Tx, s, u string) error {
		_, err := tx.Exec(ctx, "INSERT INTO test_leave VALUES($1)", u)
		return err
	}
	status, body = f.call(t, "POST", base+"/leave", map[string]any{}, lt)
	if status != 200 {
		t.Fatalf("member leave %d %+v", status, body)
	}
	_ = f.db.Pool.QueryRow(context.Background(), "SELECT count(*) FROM test_leave").Scan(&n)
	if n != 1 {
		t.Fatal("leave hook missing")
	}
	status, body = f.call(t, "POST", base+"/leave", map[string]any{}, mt)
	if status != 200 {
		t.Fatalf("dissolve %d %+v", status, body)
	}
	var dissolved bool
	_ = f.db.Pool.QueryRow(context.Background(), "SELECT dissolved_at IS NOT NULL FROM household.spaces WHERE id=$1", sid).Scan(&dissolved)
	if !dissolved {
		t.Fatal("last leader did not dissolve")
	}

}

func TestConcurrentJoinLastSlotAndInvalidatedInvite(t *testing.T) {
	f := setupAuth(t)
	leader := f.activate(t, "capacity@example.com")
	a := f.activate(t, "slot-a@example.com")
	b := f.activate(t, "slot-b@example.com")
	lt := leader["accessToken"].(string)
	_, err := f.db.Pool.Exec(context.Background(), "UPDATE admin.system_configs SET value='2' WHERE key='group.maxMembers'")
	if err != nil {
		t.Fatal(err)
	}
	status, body := f.call(t, "POST", "/it4788/v1/spaces", map[string]any{"name": "Capacity group"}, lt)
	if status != 201 {
		t.Fatal(body)
	}
	sid := body["data"].(map[string]any)["id"].(string)
	base := "/it4788/v1/spaces/" + sid
	_, body = f.call(t, "POST", base+"/invites", map[string]any{}, lt)
	old := body["data"].(map[string]any)["code"].(string)
	_, body = f.call(t, "POST", base+"/invites", map[string]any{}, lt)
	code := body["data"].(map[string]any)["code"].(string)
	status, _ = f.call(t, "POST", "/it4788/v1/spaces/join", map[string]any{"code": old}, a["accessToken"].(string))
	if status != 422 {
		t.Fatalf("old invite accepted %d", status)
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make(chan int, 2)
	for _, u := range []map[string]any{a, b} {
		wg.Add(1)
		go func(token string) {
			defer wg.Done()
			<-start
			status, _ := f.call(t, "POST", "/it4788/v1/spaces/join", map[string]any{"code": code}, token)
			results <- status
		}(u["accessToken"].(string))
	}
	close(start)
	wg.Wait()
	close(results)
	wins, full := 0, 0
	for status := range results {
		if status == 200 {
			wins++
		}
		if status == 422 {
			full++
		}
	}
	if wins != 1 || full != 1 {
		t.Fatalf("wins=%d full=%d", wins, full)
	}
	var n int
	_ = f.db.Pool.QueryRow(context.Background(), "SELECT count(*) FROM household.space_members WHERE space_id=$1", sid).Scan(&n)
	if n != 2 {
		t.Fatalf("capacity exceeded: %d", n)
	}
}
