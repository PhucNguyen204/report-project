//go:build integration

package notification

import (
	"context"
	"dicho/internal/platform"
	"dicho/internal/testutil"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// Once delivery history expires, replaying the surviving inbox entry must not recreate delivery rows or notify a newly added device.
func TestOldNoticeReplayDoesNotRefanoutAfterDeliveryRetention(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	clock := &fixedClock{time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)}
	db.Clock = clock
	user := notifyUser(t, db)
	if _, err := db.Pool.Exec(ctx, "INSERT INTO identity.devices(id,user_id,fcm_token,platform) VALUES($1,$2,'original-device','android')", platform.ID(), user); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`{"name":"projects/project/messages/sent"}`))
	}))
	defer server.Close()
	s := New(db, nil, FCM{Project: "project", Client: server.Client(), BaseURL: server.URL})
	payload, _ := json.Marshal(map[string]string{"userId": user, "title": "Title", "body": "Body"})
	event := platform.Event{Event: "notification", Key: "old-notice", Payload: payload}
	if err := s.Handle(ctx, event); err != nil {
		t.Fatal(err)
	}
	clock.at = clock.at.Add(31 * 24 * time.Hour)
	if err := s.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, "INSERT INTO identity.devices(id,user_id,fcm_token,platform) VALUES($1,$2,'new-device','ios')", platform.ID(), user); err != nil {
		t.Fatal(err)
	}
	if err := s.Handle(ctx, event); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM notification.deliveries").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 || requests.Load() != 1 {
		t.Fatalf("old notification refanned out: rows%d requests%d", count, requests.Load())
	}
}
