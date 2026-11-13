//go:build integration

package notification

import (
	"context"
	"dicho/internal/platform"
	"dicho/internal/testutil"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// Per-device persistence must retry only failed deliveries, remove only UNREGISTERED tokens, and send navigation metadata on the real HTTP wire.
func TestFCMDeliveryPersistsRetryInvalidTokenAndMetadata(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	user := notifyUser(t, db)
	devices := map[string]string{}
	for _, token := range []string{"good", "retry", "invalid"} {
		id := platform.ID()
		devices[token] = id
		if _, err := db.Pool.Exec(ctx, "INSERT INTO identity.devices(id,user_id,fcm_token,platform) VALUES($1,$2,$3,'android')", id, user, token); err != nil {
			t.Fatal(err)
		}
	}
	space, entity := platform.ID(), platform.ID()
	var fail atomic.Bool
	fail.Store(true)
	received := make(chan map[string]any, 10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/projects/project/messages:send" || r.Header.Get("Content-Type") != "application/json" {
			http.Error(w, "bad wire contract", 400)
			return
		}
		var wire struct {
			Message map[string]any `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
			http.Error(w, "bad payload", 400)
			return
		}
		received <- wire.Message
		data, ok := wire.Message["data"].(map[string]any)
		if !ok || data["spaceId"] != space || data["entityId"] != entity || data["notificationId"] == "" || data["type"] != "assignment" || data["route"] != "shopping" {
			http.Error(w, "missing navigation metadata", 400)
			return
		}
		switch wire.Message["token"] {
		case "invalid":
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"error":{"details":[{"errorCode":"UNREGISTERED"}]}}`))
		case "retry":
			if fail.Load() {
				w.WriteHeader(503)
				_, _ = w.Write([]byte(`{"error":{"message":"secret-provider-token"}}`))
				return
			}
			fallthrough
		default:
			_, _ = w.Write([]byte(`{"name":"projects/project/messages/accepted"}`))
		}
	}))
	defer server.Close()
	s := New(db, nil, FCM{Project: "project", Client: server.Client(), BaseURL: server.URL})
	payload, _ := json.Marshal(map[string]any{"userId": user, "spaceId": space, "entityId": entity, "title": "Phân công mua sắm", "body": "Bạn được giao mua rau củ", "type": "assignment", "data": map[string]string{"route": "shopping"}})
	event := platform.Event{Event: "notification", Key: "assignment:one", Payload: payload}
	if err := s.Handle(ctx, event); err == nil || strings.Contains(err.Error(), "secret-provider-token") {
		t.Fatalf("transient error %v", err)
	}
	var noticeID string
	if err := db.Pool.QueryRow(ctx, "SELECT id::text FROM notification.notifications WHERE user_id=$1", user).Scan(&noticeID); err != nil {
		t.Fatal(err)
	}
	check := func(token, want string, attempts int) {
		t.Helper()
		var status string
		var tries int
		var last *string
		if err := db.Pool.QueryRow(ctx, "SELECT status,attempts,last_error FROM notification.deliveries WHERE notification_id=$1 AND device_id=$2", noticeID, devices[token]).Scan(&status, &tries, &last); err != nil {
			t.Fatal(err)
		}
		if status != want || tries != attempts {
			t.Fatalf("%s status %s tries%d", token, status, tries)
		}
		if want == "sent" && last != nil {
			t.Fatal("successful retry retained error")
		}
		if last != nil && strings.Contains(*last, "secret") {
			t.Fatal("provider error persisted secrets")
		}
	}
	check("good", "sent", 1)
	check("retry", "pending", 1)
	check("invalid", "invalid", 1)
	var present bool
	if err := db.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM identity.devices WHERE id=$1)", devices["invalid"]).Scan(&present); err != nil {
		t.Fatal(err)
	}
	if present {
		t.Fatal("UNREGISTERED device retained")
	}
	fail.Store(false)
	for i := 0; i < 2; i++ {
		if err := s.Handle(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	check("good", "sent", 1)
	check("retry", "sent", 2)
	check("invalid", "invalid", 1)
	var n int
	if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM notification.notifications").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("worker replay duplicated inbox")
	}
	if len(received) != 4 {
		t.Fatalf("unexpected actual HTTP deliveries %d", len(received))
	}
}

// Missing providers remain retryable, while disabled accounts/preferences suppress new delivery and retries.
func TestNotificationEligibilityAndMissingProviderPersistOutcomes(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	s := New(db, nil, nil)
	emit := func(user, kind, key string) error {
		p, _ := json.Marshal(map[string]string{"userId": user, "title": "Title", "body": "Body", "type": kind})
		return s.Handle(ctx, platform.Event{Event: "notification", Key: key, Payload: p})
	}
	if err := emit(platform.ID(), "activity", "missing"); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"disabled", "deleted"} {
		user := notifyUser(t, db)
		var err error
		if state == "disabled" {
			_, err = db.Pool.Exec(ctx, "UPDATE identity.users SET status='disabled' WHERE id=$1", user)
		} else {
			_, err = db.Pool.Exec(ctx, "UPDATE identity.users SET deleted_at=now() WHERE id=$1", user)
		}
		if err != nil {
			t.Fatal(err)
		}
		if err = emit(user, "activity", state); err != nil {
			t.Fatal(err)
		}
	}
	for _, kind := range []string{"activity", "assignment", "expiry"} {
		user := notifyUser(t, db)
		if _, err := db.Pool.Exec(ctx, "INSERT INTO identity.user_settings(user_id,activity_notify_on,assignment_notify_on,expiry_notify_on) VALUES($1,false,false,false)", user); err != nil {
			t.Fatal(err)
		}
		if err := emit(user, kind, kind); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM notification.notifications").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("ineligible notification created")
	}
	user := notifyUser(t, db)
	device := platform.ID()
	if _, err := db.Pool.Exec(ctx, "INSERT INTO identity.devices(id,user_id,fcm_token,platform) VALUES($1,$2,'unconfigured','ios')", device, user); err != nil {
		t.Fatal(err)
	}
	if err := emit(user, "", "retry-default"); err == nil {
		t.Fatal("missing provider reported success")
	}
	var attempts int
	var kind string
	if err := db.Pool.QueryRow(ctx, "SELECT d.attempts,n.type FROM notification.deliveries d JOIN notification.notifications n ON n.id=d.notification_id WHERE d.device_id=$1", device).Scan(&attempts, &kind); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 || kind != "activity" {
		t.Fatalf("missing provider outcome %d %s", attempts, kind)
	}
	if _, err := db.Pool.Exec(ctx, "INSERT INTO identity.user_settings(user_id,activity_notify_on) VALUES($1,false)", user); err != nil {
		t.Fatal(err)
	}
	if err := emit(user, "activity", "retry-default"); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool.QueryRow(ctx, "SELECT attempts FROM notification.deliveries WHERE device_id=$1", device).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 {
		t.Fatal("disabled preference still retried")
	}
}
