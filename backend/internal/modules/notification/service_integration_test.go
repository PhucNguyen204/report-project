//go:build integration

package notification

import (
	"context"
	"dicho/internal/platform"
	"dicho/internal/testutil"
	"encoding/json"
	"errors"
	"testing"
)

type testPush struct {
	calls map[string]int
	fail  bool
}

func (p *testPush) Send(_ context.Context, token, title, body string) error {
	p.calls[token]++
	if token == "two" && p.fail {
		return errors.New("temporary")
	}
	return nil
}

type testMail struct{}

func (testMail) Send(context.Context, string, string, string) error { return nil }
func TestRetryDoesNotResendSuccessfulDevice(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	id := platform.ID()
	_, err := db.Pool.Exec(ctx, "INSERT INTO identity.users(id,email,full_name,password_hash,status) VALUES($1,'notify@example.test','Notify User','unused','active')", id)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"one", "two"} {
		if _, err = db.Pool.Exec(ctx, "INSERT INTO identity.devices(id,user_id,fcm_token,platform) VALUES($1,$2,$3,'android')", platform.ID(), id, token); err != nil {
			t.Fatal(err)
		}
	}
	push := &testPush{calls: map[string]int{}, fail: true}
	s := New(db, testMail{}, push)
	b, _ := json.Marshal(map[string]any{"userId": id, "title": "Title", "body": "Message", "type": "activity"})
	e := platform.Event{Event: "notification", Key: "one-notice", Payload: b}
	if err = s.Handle(ctx, e); err == nil {
		t.Fatal("provider failure swallowed")
	}
	push.fail = false
	if err = s.Handle(ctx, e); err != nil {
		t.Fatal(err)
	}
	if err = s.Handle(ctx, e); err != nil {
		t.Fatal(err)
	}
	if push.calls["one"] != 1 || push.calls["two"] != 2 {
		t.Fatalf("calls %v", push.calls)
	}
	var n int
	_ = db.Pool.QueryRow(ctx, "SELECT count(*) FROM notification.notifications").Scan(&n)
	if n != 1 {
		t.Fatalf("duplicate inbox %d", n)
	}
}
